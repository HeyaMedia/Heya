package worker

import (
	"context"
	"encoding/json"
	"math/big"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type RatingsFetchWorker struct {
	river.WorkerDefaults[RatingsFetchArgs]
	DB       *pgxpool.Pool
	Heya     *heyamedia.HeyaProvider
	Hub      EventPublisher
	Progress *TaskProgressBroadcaster
}

func (w *RatingsFetchWorker) Work(ctx context.Context, job *river.Job[RatingsFetchArgs]) error {
	q := sqlc.New(w.DB)

	item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
	if err != nil {
		log.Debug().Err(err).Int64("media_id", job.Args.MediaItemID).Msg("ratings: media item not found, skipping")
		return nil
	}

	w.Progress.SetCurrentByKind(RatingsFetchArgs{}.Kind(), item.Title)

	var externalIDs map[string]string
	if err := json.Unmarshal(item.ExternalIds, &externalIDs); err != nil {
		log.Debug().Err(err).Int64("media_id", item.ID).Msg("ratings: external_ids decode failed, skipping")
		return nil
	}

	log.Debug().Int64("media_id", item.ID).Str("title", item.Title).Str("media_type", string(item.MediaType)).Msg("ratings: fetch starting")

	data, err := w.Heya.FetchRatings(ctx, metadata.MediaKind(item.MediaType), externalIDs)
	if err != nil {
		log.Debug().Err(err).Msg("ratings fetch failed")
		return nil
	}
	if data == nil {
		log.Debug().Int64("media_id", job.Args.MediaItemID).Msg("ratings: no data returned, skipping")
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
		if w.Hub != nil {
			w.Hub.Emit(eventhub.EventMediaUpdated, eventhub.MediaPayload{
				MediaItemID: item.ID,
				LibraryID:   item.LibraryID,
				Title:       item.Title,
				MediaType:   string(item.MediaType),
			})
		}
	} else {
		log.Debug().Int64("media_id", job.Args.MediaItemID).Int("candidates", len(data.Ratings)).Msg("ratings: no ratings stored")
	}

	return nil
}
