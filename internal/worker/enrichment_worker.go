package worker

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// FetchArtworkWorker (formerly EnrichmentWorker) runs the secondary
// artwork pass — fetches the full artwork catalogue from heya.media
// and fans out DownloadImageArgs for additional backdrops + alternate
// posters/logos beyond what the primary enrich populated. See the doc
// on FetchArtworkArgs for the trigger paths.
type FetchArtworkWorker struct {
	river.WorkerDefaults[FetchArtworkArgs]
	DB       *pgxpool.Pool
	Heya     *heyametadata.HeyaProvider
	Progress *TaskProgressBroadcaster
}

func (w *FetchArtworkWorker) Work(ctx context.Context, job *river.Job[FetchArtworkArgs]) error {
	start := time.Now()
	q := sqlc.New(w.DB)

	item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
	if err != nil {
		log.Debug().Err(err).Int64("media_item_id", job.Args.MediaItemID).Msg("artwork: media item not found, skipping")
		return nil
	}

	w.Progress.SetCurrentByKind(FetchArtworkArgs{}.Kind(), item.Title)

	log.Debug().Int64("media_item_id", item.ID).Str("title", item.Title).Str("media_type", job.Args.MediaType).Msg("artwork: fetch starting")

	var externalIDs map[string]string
	if err := decodeJSON(item.ExternalIds, &externalIDs); err != nil {
		log.Debug().Err(err).Int64("media_item_id", item.ID).Msg("artwork: external_ids decode failed, using empty set")
		externalIDs = make(map[string]string)
	}

	kind := metadata.MediaKind(job.Args.MediaType)

	lib, err := q.GetLibraryByID(ctx, item.LibraryID)
	if err != nil {
		log.Warn().Err(err).Msg("enrichment: library not found")
		return nil
	}
	settings := metadata.ParseSettings(lib.Settings)

	var fetchOpts *metadata.FetchOptions
	if settings.PreferredLanguage != "" || settings.PreferredCountry != "" {
		fetchOpts = &metadata.FetchOptions{Language: settings.PreferredLanguage, Country: settings.PreferredCountry}
		log.Debug().Int64("media_item_id", item.ID).Str("language", settings.PreferredLanguage).Str("country", settings.PreferredCountry).Msg("artwork: using library language/country preference")
	}

	maxPerType := map[string]int{
		"backdrop": 5,
		"poster":   1,
		"logo":     1,
		"banner":   1,
		"art":      1,
		"thumb":    1,
		"disc":     1,
	}

	existingAssets, _ := q.ListMediaAssets(ctx, job.Args.MediaItemID)
	countPerType := map[string]int{}
	for _, a := range existingAssets {
		if a.Label == "" {
			countPerType[string(a.AssetType)]++
		}
	}
	log.Debug().Int64("media_item_id", item.ID).Int("existing_assets", len(existingAssets)).Msg("artwork: existing asset counts loaded")

	artworks, err := w.Heya.FetchArtwork(ctx, kind, externalIDs, fetchOpts)
	if err != nil {
		log.Debug().Err(err).Msg("artwork fetch failed")
		return nil
	}
	log.Debug().Int64("media_item_id", item.ID).Int("returned", len(artworks)).Msg("artwork: heya.media returned candidates")

	sortOrder := 1
	skippedCap := 0
	for _, art := range artworks {
		if art.URL == "" {
			continue
		}
		limit := maxPerType[art.AssetType]
		if limit == 0 {
			limit = 1
		}
		if countPerType[art.AssetType] >= limit {
			skippedCap++
			continue
		}
		countPerType[art.AssetType]++
		// On-demand images: record the secondary artwork as a pending remote
		// asset row (keeps the carousel + alternate-art picker populated) instead
		// of downloading it now. The serve path fetches bytes on first view.
		var err error
		if SingleAssetTypes[art.AssetType] {
			_, err = q.UpsertPrimaryMediaAsset(ctx, sqlc.UpsertPrimaryMediaAssetParams{
				MediaItemID: job.Args.MediaItemID,
				AssetType:   sqlc.AssetType(art.AssetType),
				Source:      "remote",
				RemoteUrl:   art.URL,
				Language:    art.Language,
			})
		} else {
			_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: job.Args.MediaItemID,
				AssetType:   sqlc.AssetType(art.AssetType),
				Source:      "remote",
				RemoteUrl:   art.URL,
				Language:    art.Language,
				Label:       art.Language,
				SortOrder:   int32(sortOrder),
			})
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			log.Debug().Err(err).Int64("media_item_id", job.Args.MediaItemID).Str("asset_type", art.AssetType).Msg("pending artwork row insert skipped")
		}
		sortOrder++
	}

	log.Debug().Int64("media_item_id", job.Args.MediaItemID).Int("skipped_cap", skippedCap).Dur("duration", time.Since(start)).Msg("artwork: fan-out summary")
	log.Debug().Int64("media_item_id", job.Args.MediaItemID).Int("artworks_queued", sortOrder-1).Msg("enrichment complete")

	// No emit here: this worker only enqueues DownloadImageArgs jobs — nothing
	// is stored yet. DownloadImageWorker emits media.updated at store-time when
	// the primary poster/backdrop actually lands.

	return nil
}

func decodeJSON(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}
