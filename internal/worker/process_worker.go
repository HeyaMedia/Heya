package worker

import (
	"context"
	"path/filepath"
	"strings"

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

	// ffprobe only makes sense on actual audio/video bytes. Lyrics, NFOs,
	// subtitle sidecars, and book formats are companion files the matcher
	// reads directly via filesystem walks — sending them through ffprobe
	// just produces "exit status 1" noise in the logs.
	if isFFProbeable(file.Path) {
		_, _ = client.Insert(ctx, FFProbeArgs{
			LibraryFileID: file.ID,
			FilePath:      file.Path,
		}, nil)
	}

	client.Insert(ctx, MetadataMatchArgs{
		LibraryFileID: file.ID,
		LibraryID:     lib.ID,
		MediaType:     string(lib.MediaType),
	}, nil)

	return nil
}

// isFFProbeable reports whether a file has audio or video bytes ffmpeg can
// read. Companion sidecars (.lrc, .nfo, .srt, .ass, .vtt, .jpg) and ebook
// formats (.epub, .pdf) get a false here.
func isFFProbeable(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mkv", ".mp4", ".m4v", ".avi", ".mov", ".wmv", ".webm", ".ts", ".mpg", ".mpeg":
		return true
	case ".flac", ".mp3", ".m4a", ".aac", ".wav", ".ogg", ".oga", ".opus", ".wma", ".alac", ".aiff", ".aif":
		return true
	}
	return false
}
