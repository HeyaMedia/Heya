package worker

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type ProcessFileWorker struct {
	river.WorkerDefaults[ProcessFileArgs]
	DB *pgxpool.Pool
}

func (w *ProcessFileWorker) Work(ctx context.Context, job *river.Job[ProcessFileArgs]) error {
	q := sqlc.New(w.DB)

	file, err := q.GetLibraryFileByID(ctx, job.Args.LibraryFileID)
	if err != nil {
		return err
	}

	lib, err := q.GetLibraryByID(ctx, job.Args.LibraryID)
	if err != nil {
		return err
	}

	log.Debug().Int64("file_id", file.ID).Str("path", file.Path).Msg("processing file")

	client := river.ClientFromContext[pgx.Tx](ctx)

	client.Insert(ctx, FFProbeArgs{
		LibraryFileID: file.ID,
		FilePath:      file.Path,
	}, nil)

	client.Insert(ctx, MetadataMatchArgs{
		LibraryFileID: file.ID,
		LibraryID:     lib.ID,
		MediaType:     string(lib.MediaType),
	}, nil)

	return nil
}
