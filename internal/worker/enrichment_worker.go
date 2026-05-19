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
	DB               *pgxpool.Pool
	ArtworkProviders []metadata.ArtworkProvider
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

	client := river.ClientFromContext[pgx.Tx](ctx)

	sortOrder := 10
	for _, ap := range w.ArtworkProviders {
		artworks, err := ap.FetchArtwork(ctx, kind, externalIDs)
		if err != nil {
			log.Debug().Err(err).Str("provider", ap.Name()).Msg("artwork fetch failed")
			continue
		}
		for _, art := range artworks {
			if art.URL == "" {
				continue
			}
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
