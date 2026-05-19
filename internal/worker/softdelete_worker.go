package worker

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/kura/internal/database/sqlc"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type SoftDeleteWorker struct {
	river.WorkerDefaults[SoftDeleteArgs]
	DB *pgxpool.Pool
}

func (w *SoftDeleteWorker) Work(ctx context.Context, job *river.Job[SoftDeleteArgs]) error {
	q := sqlc.New(w.DB)

	err := q.SoftDeleteLibraryFilesByPath(ctx, sqlc.SoftDeleteLibraryFilesByPathParams{
		LibraryID: job.Args.LibraryID,
		Column2:   job.Args.Paths,
	})
	if err != nil {
		return err
	}

	log.Info().Int64("library_id", job.Args.LibraryID).Int("count", len(job.Args.Paths)).Msg("soft-deleted missing files")
	return nil
}
