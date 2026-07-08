package worker

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediafile"
	"github.com/karbowiak/heya/internal/mediatype"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type ProcessFileWorker struct {
	river.WorkerDefaults[ProcessFileArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
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

	w.Progress.SetCurrent(ProcessFileArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(file.Path))
	log.Debug().Int64("file_id", file.ID).Str("path", vfs.RedactPath(file.Path)).Msg("processing file")

	client := river.ClientFromContext[pgx.Tx](ctx)

	// ffprobe only makes sense on actual audio/video bytes. Lyrics, NFOs,
	// subtitle sidecars, and book formats are companion files the matcher
	// reads directly via filesystem walks — sending them through ffprobe
	// just produces "exit status 1" noise in the logs.
	//
	// Populated media_info means the bytes haven't changed since the last
	// probe — the scanner clears it whenever size/mtime drift (see
	// UpsertLibraryFile), so a pending file that still carries probe data got
	// here via an NFO-only re-apply and doesn't need another probe.
	if isFFProbeable(file.Path) && !hasProbeData(file.MediaInfo) {
		if _, err := client.Insert(ctx, FFProbeArgs{
			LibraryFileID:   file.ID,
			FilePath:        file.Path,
			ScheduledTaskID: job.Args.ScheduledTaskID,
		}, nil); err != nil {
			return fmt.Errorf("enqueue ffprobe: %w", err)
		}
	}

	if _, err := client.Insert(ctx, MetadataMatchArgs{
		LibraryFileID: file.ID,
		LibraryID:     lib.ID,
		// Normalize the library's declared type to its runtime type here so the
		// match/enrich pipeline (and the WS media-added event it emits) only
		// ever sees real content types — anime enters as tv. The 'anime' domain
		// signal stays on the library row for the v2 scanner. See internal/mediatype.
		MediaType:       string(mediatype.Runtime(lib.MediaType)),
		ScheduledTaskID: job.Args.ScheduledTaskID,
	}, nil); err != nil {
		return fmt.Errorf("enqueue metadata match: %w", err)
	}

	return nil
}

// isFFProbeable reports whether a file has audio or video bytes ffmpeg can
// read. Companion sidecars (.lrc, .nfo, .srt, .ass, .vtt, .jpg) and ebook
// formats (.epub, .pdf) get a false here.
func isFFProbeable(path string) bool {
	return mediafile.IsProbeable(path)
}

// hasProbeData reports whether media_info holds a real probe result (the
// column defaults to '{}'; a failed probe can leave 'null').
func hasProbeData(mediaInfo []byte) bool {
	s := strings.TrimSpace(string(mediaInfo))
	return s != "" && s != "{}" && s != "null"
}
