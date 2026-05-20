package worker

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/saver"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type SaveNFOWorker struct {
	river.WorkerDefaults[SaveNFOArgs]
	DB *pgxpool.Pool
}

func (w *SaveNFOWorker) Work(ctx context.Context, job *river.Job[SaveNFOArgs]) error {
	q := sqlc.New(w.DB)

	item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
	if err != nil {
		return nil
	}

	mediaDir := saver.MediaDir(job.Args.FilePath)

	switch sqlc.MediaType(job.Args.MediaType) {
	case sqlc.MediaTypeMovie:
		movie, err := q.GetMovieByMediaItemID(ctx, item.ID)
		if err != nil {
			return nil
		}
		if err := saver.WriteMovieNFO(mediaDir, item, movie); err != nil {
			log.Warn().Err(err).Int64("media_id", item.ID).Msg("failed to write movie NFO")
		}

	case sqlc.MediaTypeTv:
		series, err := q.GetTVSeriesByMediaItemID(ctx, item.ID)
		if err != nil {
			return nil
		}
		if err := saver.WriteTVShowNFO(mediaDir, item, series); err != nil {
			log.Warn().Err(err).Int64("media_id", item.ID).Msg("failed to write tvshow NFO")
		}
	}

	return nil
}
