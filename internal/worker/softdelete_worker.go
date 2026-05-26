package worker

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type SoftDeleteWorker struct {
	river.WorkerDefaults[SoftDeleteArgs]
	DB       *pgxpool.Pool
	Hub      EventPublisher
	Progress *TaskProgressBroadcaster
}

func (w *SoftDeleteWorker) Work(ctx context.Context, job *river.Job[SoftDeleteArgs]) error {
	w.Progress.SetCurrentByKind(SoftDeleteArgs{}.Kind(), fmt.Sprintf("library %d (%d paths)", job.Args.LibraryID, len(job.Args.Paths)))

	q := sqlc.New(w.DB)

	err := q.SoftDeleteLibraryFilesByPath(ctx, sqlc.SoftDeleteLibraryFilesByPathParams{
		LibraryID: job.Args.LibraryID,
		Column2:   job.Args.Paths,
	})
	if err != nil {
		return err
	}

	log.Info().Int64("library_id", job.Args.LibraryID).Int("count", len(job.Args.Paths)).Msg("soft-deleted missing files")

	if w.Hub != nil {
		w.Hub.Emit(eventhub.EventMediaRemoved, eventhub.MediaPayload{LibraryID: job.Args.LibraryID})
	}

	return nil
}
