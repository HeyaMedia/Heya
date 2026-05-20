package worker

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type EnrichmentWorker struct {
	river.WorkerDefaults[EnrichmentArgs]
	DB       *pgxpool.Pool
	Registry *metadata.Registry
}

func (w *EnrichmentWorker) Work(ctx context.Context, job *river.Job[EnrichmentArgs]) error {
	q := sqlc.New(w.DB)

	item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
	if err != nil {
		return nil
	}

	var externalIDs map[string]string
	if err := decodeJSON(item.ExternalIds, &externalIDs); err != nil {
		externalIDs = make(map[string]string)
	}

	kind := metadata.MediaKind(job.Args.MediaType)

	lib, err := q.GetLibraryByID(ctx, item.LibraryID)
	if err != nil {
		log.Warn().Err(err).Msg("enrichment: library not found")
		return nil
	}
	settings := metadata.ParseSettings(lib.Settings)
	artworkProviders := w.Registry.ArtworkProviders(settings.ArtworkProviders, kind)

	client := river.ClientFromContext[pgx.Tx](ctx)

	maxPerType := map[string]int{
		"backdrop":  5,
		"poster":    1,
		"clearlogo": 1,
		"banner":    1,
		"clearart":  1,
		"landscape": 1,
		"disc":      1,
	}
	countPerType := map[string]int{}

	sortOrder := 10
	for _, ap := range artworkProviders {
		artworks, err := ap.FetchArtwork(ctx, kind, externalIDs)
		if err != nil {
			log.Debug().Err(err).Str("provider", ap.Name()).Msg("artwork fetch failed")
			continue
		}
		for _, art := range artworks {
			if art.URL == "" {
				continue
			}
			limit := maxPerType[art.AssetType]
			if limit == 0 {
				limit = 1
			}
			if countPerType[art.AssetType] >= limit {
				continue
			}
			countPerType[art.AssetType]++
			client.Insert(ctx, DownloadImageArgs{
				MediaItemID: job.Args.MediaItemID,
				URL:         art.URL,
				AssetType:   art.AssetType,
				MediaType:   job.Args.MediaType,
				Label:       art.Language,
				SortOrder:   sortOrder,
			}, nil)
			sortOrder++
		}
	}

	log.Debug().Int64("media_item_id", job.Args.MediaItemID).Int("artworks_queued", sortOrder-10).Msg("enrichment complete")
	return nil
}

func decodeJSON(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}
