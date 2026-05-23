package worker

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type TranscodeWorker struct {
	river.WorkerDefaults[TranscodeArgs]
	DB      *pgxpool.Pool
	Cache   *transcoder.CacheManager
	HWAccel *transcoder.HwAccelProvider
}

func (w *TranscodeWorker) Work(ctx context.Context, job *river.Job[TranscodeArgs]) error {
	log.Debug().Int64("file_id", job.Args.LibraryFileID).Msg("background transcode skipped (on-demand only)")
	return nil
}
