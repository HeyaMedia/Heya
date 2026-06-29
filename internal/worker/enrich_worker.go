package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// EnrichMediaItemWorker fills in everything the match step left out. Match
// writes a search-only stub (title / year / poster_url / external_ids); this
// worker resolves the full provider detail and walks each component (base
// → people → extras → images → structure for TV), stamping the
// corresponding *_enriched_at column as it goes so a failed run can resume
// cleanly on the next attempt.
//
// Replaces MetadataFetchWorker, RefreshMusicArtistWorker, and consolidates
// the dispatch that used to be split across two queues. Single `metadata`
// queue, MaxWorkers=1 — heya.media artist lookups are still ~27s cold and
// shouldn't pile up on the upstream.
type EnrichMediaItemWorker struct {
	river.WorkerDefaults[EnrichMediaItemArgs]
	DB      *pgxpool.Pool
	Matcher MatchService
	Heya    *heyamedia.HeyaProvider
	Hub     EventPublisher
	// DataDir is the root for cached artwork copies (data/images/...). The
	// music enrich path reads local poster/backdrop/logo from the artist
	// folder and copies them here before falling back to heya.media URLs.
	DataDir  string
	Progress *TaskProgressBroadcaster
}

func (w *EnrichMediaItemWorker) Work(ctx context.Context, job *river.Job[EnrichMediaItemArgs]) error {
	q := sqlc.New(w.DB)

	item, err := q.GetMediaItemByID(ctx, job.Args.ItemID)
	if err != nil {
		return fmt.Errorf("get media item %d: %w", job.Args.ItemID, err)
	}

	// Idempotency gate: if the item is already complete and this isn't a
	// forced refresh, skip. Lets us tolerate redundant enqueues (e.g. scan
	// + scheduled tick both queuing the same item) without redoing work.
	if !job.Args.Force && item.EnrichmentStatus == enrichStatusComplete {
		log.Debug().Int64("item_id", item.ID).Str("title", item.Title).Msg("enrich: already complete, skipping")
		return nil
	}

	w.Progress.SetCurrent(EnrichMediaItemArgs{}.Kind(), job.Args.ScheduledTaskID, item.Title)
	_ = q.MarkEnrichAttempted(ctx, item.ID)

	switch item.MediaType {
	case sqlc.MediaTypeMusic:
		return w.enrichMusic(ctx, q, item, job)
	default:
		return w.enrichGeneric(ctx, q, item, job)
	}
}

// enrichGeneric handles movies / TV / books — call GetDetail, populate
// type-specific rows + rich metadata, kick off image + person + ratings +
// NFO downstream jobs.
func (w *EnrichMediaItemWorker) enrichGeneric(ctx context.Context, q *sqlc.Queries, item sqlc.MediaItem, job *river.Job[EnrichMediaItemArgs]) error {
	kind := matcher.MediaTypeToKind(item.MediaType)

	var externalIDs map[string]string
	if err := json.Unmarshal(item.ExternalIds, &externalIDs); err != nil {
		externalIDs = map[string]string{}
	}

	providerIDs := heyamedia.BuildLookupIDs(kind, externalIDs, item.HeyaSlug)
	if len(providerIDs) == 0 {
		return w.markFailed(ctx, q, item.ID, "no provider lookup id in external_ids")
	}

	lib, err := q.GetLibraryByID(ctx, item.LibraryID)
	if err != nil {
		return fmt.Errorf("library %d: %w", item.LibraryID, err)
	}
	settings := metadata.ParseSettings(lib.Settings)

	var fetchOpts *metadata.FetchOptions
	if settings.PreferredLanguage != "" || settings.PreferredCountry != "" {
		fetchOpts = &metadata.FetchOptions{Language: settings.PreferredLanguage, Country: settings.PreferredCountry}
	}

	detail, usedID, err := w.Heya.GetDetailFallback(ctx, providerIDs, fetchOpts)
	if err != nil {
		return w.markFailed(ctx, q, item.ID, fmt.Sprintf("get detail (tried %d ids): %v", len(providerIDs), err))
	}
	if usedID != providerIDs[0] {
		log.Info().Int64("item_id", item.ID).Str("used", usedID).Str("preferred", providerIDs[0]).Msg("enrich: fell back to non-preferred lookup id")
	}

	// Base: type-specific row (movies / tv_series / books) + seasons for TV.
	// StoreEntityMetadata already handles the kind branch.
	w.Matcher.StoreEntityMetadata(ctx, item.ID, kind, detail)
	_ = q.MarkEnrichBaseDone(ctx, item.ID)
	if kind == metadata.KindTV {
		_ = q.MarkEnrichStructureDone(ctx, item.ID)
	}

	// Persist heya.media's canonical slug. It's a stable lookup key
	// (heya.media accepts slug:<slug> alongside mbid:<id>) and lets
	// future refreshes / cross-service joins skip the search step.
	if detail.HeyaSlug != "" && detail.HeyaSlug != item.HeyaSlug {
		if err := q.UpdateMediaItemHeyaSlug(ctx, sqlc.UpdateMediaItemHeyaSlugParams{
			ID:       item.ID,
			HeyaSlug: detail.HeyaSlug,
		}); err != nil {
			log.Warn().Err(err).Int64("item_id", item.ID).Msg("update heya_slug failed")
		}
	}

	// People + extras come from the same StoreRichMetadata call. We stamp
	// both timestamps even though one call does the work, so the UI can
	// surface "people enriched" / "extras enriched" independently if we
	// ever split the call.
	w.Matcher.StoreRichMetadata(ctx, item.ID, detail)
	_ = q.MarkEnrichPeopleDone(ctx, item.ID)
	_ = q.MarkEnrichExtrasDone(ctx, item.ID)

	// Image pipeline: enqueue DetectLocalAssets (which fans out poster +
	// backdrop downloads + secondary artwork enrichment). We stamp
	// images_enriched_at here meaning "URLs known, downloads queued" —
	// the actual local files arrive asynchronously, tracked separately.
	client := river.ClientFromContext[pgx.Tx](ctx)
	pending := buildPendingImages(detail)
	if _, err := client.Insert(ctx, DetectLocalAssetsArgs{
		MediaItemID:     item.ID,
		LibraryFileID:   0, // looked up by DetectLocalAssetsWorker if needed
		MediaType:       string(item.MediaType),
		PendingImages:   pending,
		QueueEnrich:     true,
		LibraryID:       item.LibraryID,
		ScheduledTaskID: job.Args.ScheduledTaskID,
	}, nil); err != nil {
		log.Warn().Err(err).Int64("item_id", item.ID).Msg("enqueue DetectLocalAssets failed")
	}
	_ = q.MarkEnrichImagesDone(ctx, item.ID)

	enqueuePersonFetches(ctx, client, q, detail, settings.PreferredLanguage)

	if settings.FetchRatings {
		_, _ = client.Insert(ctx, RatingsFetchArgs{MediaItemID: item.ID, LibraryID: item.LibraryID}, nil)
	}

	if settings.SaveNFO {
		_, _ = client.Insert(ctx, SaveNFOArgs{
			MediaItemID: item.ID,
			MediaType:   string(item.MediaType),
		}, nil)
	}

	_ = q.MarkEnrichComplete(ctx, item.ID)

	if w.Hub != nil {
		w.Hub.Emit(eventhub.EventMediaUpdated, eventhub.MediaPayload{
			MediaItemID: item.ID,
			LibraryID:   item.LibraryID,
			Title:       detail.Title,
			MediaType:   string(item.MediaType),
		})
	}

	log.Info().
		Int64("item_id", item.ID).
		Str("source", job.Args.Source).
		Int("cast", len(detail.Cast)).
		Int("crew", len(detail.Crew)).
		Int("keywords", len(detail.Keywords)).
		Msg("enrich complete")

	return nil
}

// enrichMusic delegates to matcher.RefreshMusicArtist (which already does the
// heya.media artist fetch + canonical upsert) and layers the new status
// stamps on top.
func (w *EnrichMediaItemWorker) enrichMusic(ctx context.Context, q *sqlc.Queries, item sqlc.MediaItem, job *river.Job[EnrichMediaItemArgs]) error {
	artist, err := q.GetArtistByMediaItemID(ctx, item.ID)
	if err != nil {
		return w.markFailed(ctx, q, item.ID, fmt.Sprintf("get artist for media item: %v", err))
	}

	res, err := w.Matcher.RefreshMusicArtist(ctx, artist.ID)
	if err != nil {
		return w.markFailed(ctx, q, item.ID, fmt.Sprintf("refresh music artist: %v", err))
	}

	// RefreshMusicArtist already stamps artists.discography_enriched_at
	// inside the matcher. Mirror that onto media_items' base/structure
	// stamps so the UI's component view stays accurate.
	_ = q.MarkEnrichBaseDone(ctx, item.ID)
	_ = q.MarkEnrichStructureDone(ctx, item.ID) // artist → albums → tracks tree

	// Artist artwork: local-first, heya.media fills any gaps.
	//
	// 1. Scan the artist folder for Kodi-convention art (folder.jpg,
	//    backdrop*.jpg, logo.png, banner.jpg, fanart, clearart, thumb).
	//    Detected files are copied into the cache dir, recorded as
	//    media_assets rows with source='local', and the primary poster
	//    + backdrop are also written to media_items.poster_path /
	//    backdrop_path so the /api/media/{id}/image endpoints serve them
	//    directly. Movies/TV already get this via DetectLocalAssetsWorker
	//    — this restores parity for music.
	// 2. For slots that didn't land locally, queueArtistArtworkGaps walks
	//    the per-slot caps (1 of poster/logo/banner/clearart/thumb, up to
	//    5 unique backdrops) and queues DownloadImageArgs for the missing
	//    slots from heya.media's pool. Already-used remote URLs are
	//    skipped so repeated refreshes don't re-download the same file.
	local := detectLocalMusicAssets(ctx, q, w.DataDir, item.ID)
	remote := rankRemoteArtistImages(res.ArtistImages, res.PosterURL, res.BackdropURL)
	client := river.ClientFromContext[pgx.Tx](ctx)
	queueArtistArtworkGaps(ctx, client, item, string(item.MediaType), local, remote)
	_ = q.MarkEnrichImagesDone(ctx, item.ID)

	// SaveMusicNFO is the music-specific analogue of SaveNFO. Mirror the
	// behaviour from the old RefreshMusicArtistWorker.
	if lib, err := q.GetLibraryByID(ctx, item.LibraryID); err == nil {
		settings := metadata.ParseSettings(lib.Settings)
		if settings.SaveNFO {
			if _, err := client.Insert(ctx, SaveMusicNFOArgs{ArtistID: artist.ID}, nil); err != nil {
				log.Warn().Err(err).Int64("artist_id", artist.ID).Msg("enqueue SaveMusicNFO failed")
			}
		}
	}

	_ = q.MarkEnrichComplete(ctx, item.ID)

	if w.Hub != nil {
		w.Hub.Emit(eventhub.EventMediaUpdated, eventhub.MediaPayload{
			MediaItemID: item.ID,
			LibraryID:   item.LibraryID,
			Title:       artist.Name,
			MediaType:   string(sqlc.MediaTypeMusic),
		})

		if job.Args.BatchTotal > 0 {
			w.Hub.Emit(eventhub.EventScanProgress, eventhub.ScanPayload{
				LibraryID:  job.Args.BatchLibraryID,
				Phase:      "refresh",
				Total:      job.Args.BatchTotal,
				Done:       job.Args.BatchPosition,
				CurrentRef: artist.Name,
			})
		}
	}

	if res.Skipped {
		log.Info().Int64("artist_id", artist.ID).Str("name", artist.Name).Msg("enrich music: heya.media has no record yet")
		return nil
	}

	log.Info().
		Int64("artist_id", artist.ID).
		Str("name", artist.Name).
		Str("source", job.Args.Source).
		Int("albums_matched", res.AlbumsMatched).
		Int("albums_updated", res.AlbumsUpdated).
		Int("tracks_updated", res.TracksUpdated).
		Msg("enrich music complete")

	return nil
}

func (w *EnrichMediaItemWorker) markFailed(ctx context.Context, q *sqlc.Queries, itemID int64, reason string) error {
	_ = q.MarkEnrichFailed(ctx, sqlc.MarkEnrichFailedParams{ID: itemID, LastEnrichError: reason})
	log.Warn().Int64("item_id", itemID).Str("reason", reason).Msg("enrich failed")
	// Return nil so River doesn't retry on a hard data error — the status
	// row carries the failure and the user / scheduler can decide whether
	// to retry. Transport errors that should retry are returned by the
	// callers below.
	return nil
}

const enrichStatusComplete = "complete"

// buildPendingImages flattens detail.PosterURL / BackdropURL plus per-season
// posters and per-episode stills into the PendingImage list that
// DetectLocalAssetsWorker downloads. Lifted unchanged from the old
// MetadataFetchWorker so behaviour is preserved.
func buildPendingImages(detail *metadata.MediaDetail) []PendingImage {
	var pending []PendingImage
	if detail.PosterURL != "" {
		pending = append(pending, PendingImage{URL: detail.PosterURL, AssetType: "poster", SortOrder: 0, Priority: 1})
	}
	if detail.BackdropURL != "" {
		pending = append(pending, PendingImage{URL: detail.BackdropURL, AssetType: "backdrop", SortOrder: 0, Priority: 1})
	}
	for _, season := range detail.Seasons {
		if season.PosterURL != "" {
			pending = append(pending, PendingImage{
				URL: season.PosterURL, AssetType: "poster",
				Label: fmt.Sprintf("season-%d", season.Number), SortOrder: 1000 + season.Number, Priority: 2,
			})
		}
		for _, ep := range season.Episodes {
			if ep.StillURL != "" {
				pending = append(pending, PendingImage{
					URL: ep.StillURL, AssetType: "still",
					Label: fmt.Sprintf("s%02de%02d", season.Number, ep.Number), SortOrder: 2000 + season.Number*100 + ep.Number, Priority: 3,
				})
			}
		}
	}
	return pending
}

// enqueuePersonFetches fans out PersonFetch jobs for every TMDB-identified
// cast / crew member. Lifted from the old MetadataFetchWorker.
func enqueuePersonFetches(ctx context.Context, client *river.Client[pgx.Tx], q *sqlc.Queries, detail *metadata.MediaDetail, lang string) {
	seen := map[int32]bool{}

	enqueue := func(ids map[string]string) {
		tmdbStr := ids["tmdb"]
		if tmdbStr == "" {
			return
		}
		n, err := strconv.ParseInt(tmdbStr, 10, 32)
		if err != nil || n == 0 {
			return
		}
		tmdbID := int32(n)
		if seen[tmdbID] {
			return
		}
		seen[tmdbID] = true

		extJSON, _ := json.Marshal(map[string]string{"tmdb": strconv.FormatInt(int64(tmdbID), 10)})
		person, err := q.FindPersonByExternalID(ctx, extJSON)
		if err != nil {
			return
		}
		_, _ = client.Insert(ctx, PersonFetchArgs{
			PersonID: person.ID,
			TmdbID:   tmdbID,
			Language: lang,
		}, &river.InsertOpts{Priority: 4})
	}

	for _, c := range detail.Cast {
		enqueue(c.ExternalIDs)
	}
	for _, c := range detail.Crew {
		enqueue(c.ExternalIDs)
	}
}
