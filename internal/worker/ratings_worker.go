package worker

import (
	"context"
	"encoding/json"
	"math/big"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type RatingsFetchWorker struct {
	river.WorkerDefaults[RatingsFetchArgs]
	DB       *pgxpool.Pool
	Heya     *heyamedia.HeyaProvider
	Progress *TaskProgressBroadcaster
}

func (w *RatingsFetchWorker) Work(ctx context.Context, job *river.Job[RatingsFetchArgs]) error {
	q := sqlc.New(w.DB)

	item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
	if err != nil {
		return nil
	}

	w.Progress.SetCurrentByKind(RatingsFetchArgs{}.Kind(), item.Title)

	var externalIDs map[string]string
	if err := json.Unmarshal(item.ExternalIds, &externalIDs); err != nil {
		return nil
	}

	data, err := w.Heya.FetchRatings(ctx, metadata.MediaKind(item.MediaType), externalIDs)
	if err != nil {
		log.Debug().Err(err).Msg("ratings fetch failed")
		return nil
	}
	if data == nil {
		return nil
	}

	totalStored := 0
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
			Votes:    int32(r.Votes),
			RawValue: r.RawValue,
		})
		totalStored++
	}

	if totalStored > 0 {
		log.Info().Int64("media_id", job.Args.MediaItemID).Int("ratings", totalStored).Msg("ratings stored")
	}

	return nil
}
