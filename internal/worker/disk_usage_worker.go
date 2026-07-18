package worker

import (
	"context"
	"io/fs"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// ScanLibraryDiskWorker walks every library_paths[] entry for the given
// library and upserts a row into library_disk_usage per (library_id, path)
// with the total byte size and file count. Used by the Storage page to show
// "real" library footprints — filesystem statfs only gives you the volume
// total, which is useless when multiple libraries share a disk.
//
// Implementation notes:
//   - filepath.WalkDir is used (lighter than Walk because no Lstat per entry)
//   - errors on individual files are logged and skipped — a permissions hiccup
//     on one file shouldn't abort the whole scan
//   - symlinks aren't followed (WalkDir default) so a recursive symlink in
//     the tree won't cause an infinite loop
//   - the worker doesn't update progress per-file; on a 10TB library that
//     would spam the event hub. The page shows last-scanned-at instead.
type ScanLibraryDiskWorker struct {
	river.WorkerDefaults[ScanLibraryDiskArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *ScanLibraryDiskWorker) Work(ctx context.Context, job *river.Job[ScanLibraryDiskArgs]) error {
	q := sqlc.New(w.DB)

	lib, err := q.GetLibraryByID(ctx, job.Args.LibraryID)
	if err != nil {
		// Library was deleted between the kickoff and the worker pickup —
		// not retryable, just drop.
		return nil
	}

	w.Progress.SetCurrentByKind(ScanLibraryDiskArgs{}.Kind(), lib.Name)

	for _, path := range lib.Paths {
		bytes, files, err := walkPath(ctx, path)
		if err != nil {
			log.Warn().Err(vfs.RedactError(err)).Int64("library_id", lib.ID).Str("path", vfs.RedactPath(path)).Msg("disk-usage walk failed")
			// Persist whatever we got — partial is better than nothing.
		}
		if err := q.UpsertLibraryDiskUsage(ctx, sqlc.UpsertLibraryDiskUsageParams{
			LibraryID: lib.ID,
			Path:      path,
			Bytes:     bytes,
			FileCount: files,
		}); err != nil {
			log.Warn().Err(vfs.RedactError(err)).Int64("library_id", lib.ID).Str("path", vfs.RedactPath(path)).Msg("disk-usage upsert failed")
		}
	}

	log.Info().Int64("library_id", lib.ID).Str("library", lib.Name).Msg("disk-usage scan complete")
	return nil
}

func walkPath(ctx context.Context, root string) (bytes, files int64, err error) {
	werr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		// Honour ctx cancellation: WalkDir is otherwise unbounded.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			// Don't abort — log + continue. WalkDir surfaces permission
			// errors here.
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		bytes += info.Size()
		files++
		return nil
	})
	if werr != nil {
		return bytes, files, werr
	}
	return bytes, files, nil
}
