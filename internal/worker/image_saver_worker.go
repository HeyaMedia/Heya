package worker

import (
	"context"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/saver"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type SaveImagesWorker struct {
	river.WorkerDefaults[SaveImagesArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *SaveImagesWorker) Work(ctx context.Context, job *river.Job[SaveImagesArgs]) error {
	w.Progress.SetCurrentByKind(SaveImagesArgs{}.Kind(), job.Args.AssetType+" → "+filepath.Base(job.Args.FilePath))

	mediaDir := saver.MediaDir(job.Args.FilePath)

	if err := saver.SaveImageToMediaDir(mediaDir, job.Args.CachedPath, job.Args.AssetType, job.Args.SortOrder); err != nil {
		log.Warn().Err(err).Str("asset", job.Args.AssetType).Msg("failed to save image to media dir")
	}

	return nil
}
