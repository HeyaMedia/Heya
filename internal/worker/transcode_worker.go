package worker

import (
	"context"

	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type TranscodeWorker struct {
	river.WorkerDefaults[TranscodeArgs]
	Progress *TaskProgressBroadcaster
}

func (w *TranscodeWorker) Work(ctx context.Context, job *river.Job[TranscodeArgs]) error {
	// Background transcode is a stub — real transcoding happens
	// on-demand via the streaming pipeline. Emit progress anyway so
	// the field is wired the day this gets a real implementation.
	w.Progress.SetCurrentByKind(TranscodeArgs{}.Kind(), job.Args.Profile)
	log.Debug().Int64("file_id", job.Args.LibraryFileID).Msg("background transcode skipped (on-demand only)")
	return nil
}
