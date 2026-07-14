package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata"
	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
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
	Heya    *heyametadata.HeyaProvider
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

	log.Debug().Int64("item_id", item.ID).Str("title", item.Title).Str("media_type", string(item.MediaType)).Str("source", job.Args.Source).Bool("force", job.Args.Force).Msg("enrich: job started")

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
func (w *EnrichMediaItemWorker) enrichGeneric(ctx context.Context, q *sqlc.Queries, item sqlc.MediaItemCard, job *river.Job[EnrichMediaItemArgs]) error {
	start := time.Now()
	kind := matcher.MediaTypeToKind(item.MediaType)

	var externalIDs map[string]string
	if err := json.Unmarshal(item.ExternalIds, &externalIDs); err != nil {
		log.Debug().Err(err).Int64("item_id", item.ID).Msg("enrich: external_ids decode failed, using empty set")
		externalIDs = map[string]string{}
	}

	providerIDs := heyametadata.BuildLookupIDs(kind, externalIDs, item.HeyaSlug)
	if len(providerIDs) == 0 {
		// A pure-local entity (materialized from filename/tags, no NFO/filename
		// provider id) has nothing to fetch yet — leave it visible and 'local'
		// rather than marking it failed. (A title-search fast path can upgrade
		// these later.) A non-local item missing an id is a real failure.
		if item.ProviderKind == "local" {
			log.Debug().Int64("item_id", item.ID).Str("provider_kind", item.ProviderKind).Msg("enrich: no lookup ids and provider_kind=local, leaving as local")
			return nil
		}
		return w.markFailed(ctx, q, item.ID, "no provider lookup id in external_ids")
	}
	log.Debug().Int64("item_id", item.ID).Str("kind", string(kind)).Int("candidate_ids", len(providerIDs)).Str("preferred_id", providerIDs[0]).Msg("enrich: lookup ids resolved")

	lib, err := q.GetLibraryByID(ctx, item.LibraryID)
	if err != nil {
		return fmt.Errorf("library %d: %w", item.LibraryID, err)
	}
	settings := metadata.ParseSettings(lib.Settings)

	fetchOpts := &metadata.FetchOptions{
		Language: settings.PreferredLanguage,
		Country:  settings.PreferredCountry,
		Title:    item.Title,
		Year:     item.Year,
	}
	switch item.MediaType {
	case sqlc.MediaTypeAnime:
		fetchOpts.CanonicalKind = "anime"
	case sqlc.MediaTypeTv:
		fetchOpts.CanonicalKind = "tv_show"
	}
	if settings.PreferredLanguage != "" || settings.PreferredCountry != "" {
		log.Debug().Int64("item_id", item.ID).Str("language", settings.PreferredLanguage).Str("country", settings.PreferredCountry).Msg("enrich: using library language/country preference")
	}

	detail, usedID, err := w.Heya.GetDetailFallback(ctx, providerIDs, fetchOpts)
	if err != nil {
		// Transient upstream failure (429/5xx, timeout, connection blip) — let
		// River retry the whole job rather than stamping the item failed on
		// something HeyaMetadata will likely serve on a later attempt. Only a
		// terminal error (every id 404'd, bad data) marks it failed for the
		// stale-refresh sweep to re-drive.
		if ctx.Err() != nil || heyametadata.IsRetryable(err) {
			return fmt.Errorf("enrich %d: get detail (tried %d ids): %w", item.ID, len(providerIDs), err)
		}
		return w.markFailed(ctx, q, item.ID, fmt.Sprintf("get detail (tried %d ids): %v", len(providerIDs), err))
	}
	log.Debug().Int64("item_id", item.ID).Str("used_id", usedID).Str("title", detail.Title).Msg("enrich: detail fetched")
	if usedID != providerIDs[0] {
		log.Info().Int64("item_id", item.ID).Str("used", usedID).Str("preferred", providerIDs[0]).Msg("enrich: fell back to non-preferred lookup id")
	}

	// Base: type-specific row (movies / tv_series / books) + seasons for TV.
	// Skip when a prior (partial) attempt already did it and this isn't a forced
	// refresh — re-running would re-link networks/creators (delete+recreate) and
	// re-walk the season tree. The *_enriched_at stamps exist precisely to resume
	// without redoing successful components (migration 00017); Force refreshes all.
	if job.Args.Force || !item.BaseEnrichedAt.Valid {
		// If the type-specific row can't be written the item would be invisible
		// (library grid INNER JOINs movies/tv_series/books) — so mark it failed
		// (the refresh-stale sweep re-drives 'failed') instead of stamping it
		// base-done/complete on a phantom success.
		if err := w.Matcher.StoreEntityMetadata(ctx, item.ID, kind, detail); err != nil {
			return w.markFailed(ctx, q, item.ID, fmt.Sprintf("store base metadata: %v", err))
		}
		_ = q.MarkEnrichBaseDone(ctx, item.ID)
		if kind == metadata.KindTV {
			_ = q.MarkEnrichStructureDone(ctx, item.ID)
		}
		log.Debug().Int64("item_id", item.ID).Bool("force", job.Args.Force).Msg("enrich: base component stored")
	} else {
		log.Debug().Int64("item_id", item.ID).Msg("enrich: base component already enriched, skipping")
	}

	// The episode catalog (with absolute_number) now exists. Resolve any
	// absolute-numbered anime files ("Series - 24 - Title", parsed with no
	// season) onto their real season/episode and write it into parse_result so
	// every downstream file<->episode join sees them. Idempotent; runs even on a
	// skipped re-enrich to pick up newly-added files.
	if kind == metadata.KindTV {
		if n, rErr := matcher.ReconcileAbsoluteEpisodes(ctx, q, item.ID); rErr != nil {
			log.Warn().Err(rErr).Int64("item_id", item.ID).Msg("enrich: reconcile absolute episodes failed")
		} else if n > 0 {
			log.Info().Int64("item_id", item.ID).Int("resolved", n).Msg("enrich: reconciled absolute anime files")
		}
	}

	// Preserve the presentation slug for compatibility. Canonical identity and
	// future refreshes use the bound HeyaMetadata UUID, never this slug.
	if detail.HeyaSlug != "" && detail.HeyaSlug != item.HeyaSlug {
		if err := q.UpdateMediaItemHeyaSlug(ctx, sqlc.UpdateMediaItemHeyaSlugParams{
			ID:       item.ID,
			HeyaSlug: detail.HeyaSlug,
		}); err != nil {
			log.Warn().Err(err).Int64("item_id", item.ID).Msg("update heya_slug failed")
		}
	}

	// People + extras come from the same StoreRichMetadata call. Skip when a
	// prior attempt already enriched people and this isn't a forced refresh —
	// re-running rewrites (and, lacking dedup, can duplicate) cast/crew/keywords.
	// Force does a full refresh. We stamp both timestamps even though one call
	// does the work, so the UI can surface them independently if we ever split.
	if job.Args.Force || !item.PeopleEnrichedAt.Valid {
		// A partial rich write (some cast rows failed, person creates errored)
		// must not be stamped done — mark the item failed so the refresh-stale
		// sweep re-drives it; the fan-out is ON CONFLICT-idempotent, so the
		// retry fills exactly the gaps.
		if err := w.Matcher.StoreRichMetadata(ctx, item.ID, detail); err != nil {
			return w.markFailed(ctx, q, item.ID, fmt.Sprintf("store rich metadata: %v", err))
		}
		_ = q.MarkEnrichPeopleDone(ctx, item.ID)
		_ = q.MarkEnrichExtrasDone(ctx, item.ID)
		log.Debug().Int64("item_id", item.ID).Int("cast", len(detail.Cast)).Int("crew", len(detail.Crew)).Int("keywords", len(detail.Keywords)).Msg("enrich: people/extras component stored")
	} else {
		log.Debug().Int64("item_id", item.ID).Msg("enrich: people/extras component already enriched, skipping")
	}

	// Image pipeline: enqueue DetectLocalAssets (local sidecar detection +
	// pending rows for the primary poster/backdrop + per-season/episode art).
	// We stamp images_enriched_at here meaning "URLs known"; the bytes are
	// fetched on-demand when first viewed.
	client := river.ClientFromContext[pgx.Tx](ctx)
	pending := buildPendingImages(detail)
	log.Debug().Int64("item_id", item.ID).Int("pending_images", len(pending)).Msg("enrich: image urls collected")
	if _, err := client.Insert(ctx, DetectLocalAssetsArgs{
		MediaItemID:     item.ID,
		LibraryFileID:   0, // looked up by DetectLocalAssetsWorker if needed
		MediaType:       string(item.MediaType),
		PendingImages:   pending,
		QueueEnrich:     true,
		LibraryID:       item.LibraryID,
		ScheduledTaskID: job.Args.ScheduledTaskID,
	}, scheduledJobInsertOpts(scheduledJobSource(job.Metadata))); err != nil {
		log.Warn().Err(err).Int64("item_id", item.ID).Msg("enqueue DetectLocalAssets failed")
	}
	_ = q.MarkEnrichImagesDone(ctx, item.ID)

	// Secondary artwork (extra backdrops for the carousel, logos, banners, ...)
	// comes from the SAME detail response we already fetched — record it as
	// pending rows here instead of firing a second heya.media call for it.
	writeSecondaryArtwork(ctx, q, item.ID, detail)

	// Person deep-fetch is LAZY. The cast/crew LIST (name, role, tmdb id,
	// profile URL) is already persisted in-process by StoreRichMetadata above —
	// that's everything media detail pages render. The expensive per-person
	// deep-fetch (biography, full filmography, birth/death) is consumed only on
	// the person page, so we defer it to GetPerson's on-view backfill kicker
	// rather than fanning out ~200 person_fetch jobs per title here. heya.media
	// also pre-warms top-billed cast/crew on its own side, so the common people
	// are usually warm by the time anyone opens their page.

	_, _ = client.Insert(ctx, RatingsFetchArgs{MediaItemID: item.ID, LibraryID: item.LibraryID}, nil)
	log.Debug().Int64("item_id", item.ID).Msg("enrich: ratings fetch queued")

	if settings.SaveNFO {
		files, err := q.ListLibraryFilesByMediaItem(ctx, pgtype.Int8{Int64: item.ID, Valid: true})
		if err == nil && len(files) > 0 {
			_, _ = client.Insert(ctx, SaveNFOArgs{
				MediaItemID:   item.ID,
				LibraryFileID: files[0].ID,
				FilePath:      files[0].Path,
				MediaType:     string(item.MediaType),
			}, nil)
			log.Debug().Int64("item_id", item.ID).Msg("enrich: save nfo queued")
		}
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

	log.Debug().Int64("item_id", item.ID).Dur("duration", time.Since(start)).Msg("enrich: generic pipeline finished")
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
func (w *EnrichMediaItemWorker) enrichMusic(ctx context.Context, q *sqlc.Queries, item sqlc.MediaItemCard, job *river.Job[EnrichMediaItemArgs]) error {
	start := time.Now()
	artist, err := q.GetArtistByMediaItemID(ctx, item.ID)
	if err != nil {
		return w.markFailed(ctx, q, item.ID, fmt.Sprintf("get artist for media item: %v", err))
	}
	log.Debug().Int64("item_id", item.ID).Int64("artist_id", artist.ID).Str("name", artist.Name).Msg("enrich: music artist resolved")

	res, err := w.Matcher.RefreshMusicArtist(ctx, artist.ID)
	if err != nil {
		return w.markFailed(ctx, q, item.ID, fmt.Sprintf("refresh music artist: %v", err))
	}
	log.Debug().Int64("artist_id", artist.ID).Str("entity_id", res.HeyaEntityID).Bool("skipped", res.Skipped).Int("albums_matched", res.AlbumsMatched).Int("albums_updated", res.AlbumsUpdated).Int("tracks_updated", res.TracksUpdated).Msg("enrich: music artist refreshed")

	// RefreshMusicArtist already stamps artists.discography_enriched_at
	// inside the matcher. Mirror that onto media_items' base/structure
	// stamps so the UI's component view stays accurate.
	_ = q.MarkEnrichBaseDone(ctx, item.ID)
	_ = q.MarkEnrichStructureDone(ctx, item.ID) // artist → albums → tracks tree
	settings := metadata.LibrarySettings{UseLocalData: true}
	if lib, err := q.GetLibraryByID(ctx, item.LibraryID); err == nil {
		settings = metadata.ParseSettings(lib.Settings)
	}

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
	local := detectLocalMusicAssets(ctx, q, w.DataDir, item.ID, settings.UseLocalData)
	log.Debug().Int64("item_id", item.ID).Int("local_poster", local.Poster).Int("local_backdrop", local.Backdrop).Int("local_logo", local.Logo).Int("local_banner", local.Banner).Msg("enrich music: local asset scan done")
	// A skipped refresh (no upstream record, or an identity-conflict guard
	// fired) carries no trustworthy upstream artwork — only fill gaps from
	// the remote pool when the refresh actually adopted the record.
	remote := remoteArtistImages{}
	if !res.Skipped {
		remote = rankRemoteArtistImages(res.ArtistImages, res.PosterURL, res.BackdropURL)
		log.Debug().Int64("item_id", item.ID).Int("remote_backdrops", len(remote.Backdrops)).Bool("remote_poster", remote.Poster != "").Bool("remote_logo", remote.Logo != "").Msg("enrich music: remote artwork ranked")
	} else {
		log.Debug().Int64("item_id", item.ID).Msg("enrich music: refresh skipped, no remote artwork gap-fill")
	}
	client := river.ClientFromContext[pgx.Tx](ctx)
	queueArtistArtworkGaps(ctx, client, item, string(item.MediaType), local, remote)
	_ = q.MarkEnrichImagesDone(ctx, item.ID)

	// SaveMusicNFO is the music-specific analogue of SaveNFO. Mirror the
	// behaviour from the old RefreshMusicArtistWorker.
	if settings.SaveNFO {
		if _, err := client.Insert(ctx, SaveMusicNFOArgs{ArtistID: artist.ID}, nil); err != nil {
			log.Warn().Err(err).Int64("artist_id", artist.ID).Msg("enqueue SaveMusicNFO failed")
		} else {
			log.Debug().Int64("artist_id", artist.ID).Msg("enrich music: save nfo queued")
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
		log.Debug().Int64("artist_id", artist.ID).Dur("duration", time.Since(start)).Msg("enrich: music pipeline finished (skipped)")
		log.Info().Int64("artist_id", artist.ID).Str("name", artist.Name).Msg("enrich music: heya.media has no record yet")
		return nil
	}

	log.Debug().Int64("artist_id", artist.ID).Dur("duration", time.Since(start)).Msg("enrich: music pipeline finished")
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

// writeSecondaryArtwork records the alternate/secondary artwork carried by the
// enrich detail response (extra backdrops for the carousel, logos, banners,
// clearart, thumbs, disc art) as pending media_assets rows — using the artwork
// the initial GetDetail already returned, so there's no second heya.media call.
// The serve path pulls the bytes on first view.
//
// The primary poster/backdrop are skipped by URL (they're emitted at sort 0 by
// buildPendingImages and held in media_items columns). Secondary rows always
// carry a non-empty label, so a bare/primary image request (which matches the
// empty-label rows) never resolves to one of them.
func writeSecondaryArtwork(ctx context.Context, q *sqlc.Queries, itemID int64, detail *metadata.MediaDetail) {
	if len(detail.Artwork) == 0 {
		return
	}
	maxPerType := map[string]int{"backdrop": 5, "poster": 1, "logo": 1, "banner": 1, "clearart": 1, "thumb": 1, "disc": 1}
	count := map[string]int{}
	existing, _ := q.ListMediaAssets(ctx, itemID)
	for _, a := range existing {
		if a.Label == "" {
			count[string(a.AssetType)]++
		}
	}
	// The primary poster/backdrop may not have an asset row yet (they live in
	// media_items columns until first served), so seed their count so we don't
	// emit e.g. a 6th backdrop.
	if detail.PosterURL != "" && count["poster"] == 0 {
		count["poster"] = 1
	}
	if detail.BackdropURL != "" && count["backdrop"] == 0 {
		count["backdrop"] = 1
	}
	sortOrder := 1
	for _, art := range detail.Artwork {
		if art.URL == "" || art.URL == detail.PosterURL || art.URL == detail.BackdropURL {
			continue
		}
		limit := maxPerType[art.AssetType]
		if limit == 0 {
			limit = 1
		}
		if count[art.AssetType] >= limit {
			continue
		}
		count[art.AssetType]++
		var err error
		if SingleAssetTypes[art.AssetType] {
			_, err = q.UpsertPrimaryMediaAsset(ctx, sqlc.UpsertPrimaryMediaAssetParams{
				MediaItemID: itemID,
				AssetType:   sqlc.AssetType(art.AssetType),
				Source:      "remote",
				RemoteUrl:   art.URL,
				Language:    art.Language,
			})
		} else {
			label := art.Language
			if label == "" {
				label = "extra"
			}
			_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: itemID,
				AssetType:   sqlc.AssetType(art.AssetType),
				Source:      "remote",
				RemoteUrl:   art.URL,
				Language:    art.Language,
				Label:       label,
				SortOrder:   int32(sortOrder),
			})
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			log.Debug().Err(err).Int64("item_id", itemID).Str("asset_type", art.AssetType).Msg("pending artwork row insert skipped")
		}
		sortOrder++
	}
}
