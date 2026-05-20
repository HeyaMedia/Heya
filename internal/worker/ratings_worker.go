package worker

import (
	"context"
	"encoding/json"
	"math/big"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type RatingsFetchWorker struct {
	river.WorkerDefaults[RatingsFetchArgs]
	DB       *pgxpool.Pool
	Registry *metadata.Registry
}

func (w *RatingsFetchWorker) Work(ctx context.Context, job *river.Job[RatingsFetchArgs]) error {
	q := sqlc.New(w.DB)

	item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
	if err != nil {
		return nil
	}

	var externalIDs map[string]string
	if err := json.Unmarshal(item.ExternalIds, &externalIDs); err != nil {
		return nil
	}

	lib, err := q.GetLibraryByID(ctx, job.Args.LibraryID)
	if err != nil {
		return nil
	}
	settings := metadata.ParseSettings(lib.Settings)

	providers := w.Registry.RatingsProviders(settings.RatingsProviders)
	if len(providers) == 0 {
		return nil
	}

	kind := metadata.MediaKind(item.MediaType)
	totalStored := 0

	for _, rp := range providers {
		if !rp.Supports(kind) {
			continue
		}

		data, err := rp.FetchRatings(ctx, externalIDs)
		if err != nil {
			log.Debug().Err(err).Str("provider", rp.Name()).Msg("ratings fetch failed")
			continue
		}
		if data == nil {
			continue
		}

		for _, r := range data.Ratings {
			q.UpsertExternalRating(ctx, sqlc.UpsertExternalRatingParams{
				MediaItemID: job.Args.MediaItemID,
				Source:      r.Source,
				Value:       r.Value,
				Score: pgtype.Numeric{
					Int:   big.NewInt(int64(r.Score * 10)),
					Exp:   -1,
					Valid: true,
				},
			})
			totalStored++
		}
	}

	if totalStored > 0 {
		log.Info().Int64("media_id", job.Args.MediaItemID).Int("ratings", totalStored).Msg("ratings stored")
	}

	return nil
}
