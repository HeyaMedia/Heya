package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/scanner"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type scanRequest struct {
	LibraryID int64
	Force     bool
}

type WatcherPauser interface {
	Pause(libraryID int64)
	Resume(libraryID int64)
}

type ScanLibrariesTask struct {
	DB      *pgxpool.Pool
	River   *river.Client[pgx.Tx]
	Hub     *eventhub.Hub
	Watcher WatcherPauser

	mu    sync.Mutex
	queue []scanRequest
}

func (t *ScanLibrariesTask) ID() TaskID { return TaskScanLibraries }

func (t *ScanLibrariesTask) Enqueue(libraryID int64, force bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range t.queue {
		if r.LibraryID == libraryID {
			return
		}
	}
	t.queue = append(t.queue, scanRequest{LibraryID: libraryID, Force: force})
}

func (t *ScanLibrariesTask) drainQueue() []scanRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	q := t.queue
	t.queue = nil
	return q
}

func (t *ScanLibrariesTask) CountPending(ctx context.Context) (int, error) {
	t.mu.Lock()
	queued := len(t.queue)
	t.mu.Unlock()
	if queued > 0 {
		return queued, nil
	}

	q := sqlc.New(t.DB)
	libs, err := q.ListLibraries(ctx)
	if err != nil {
		return 0, err
	}
	if len(libs) == 0 {
		return 0, nil
	}
	return 1, nil
}

func (t *ScanLibrariesTask) Run(ctx context.Context, progress *ProgressTracker) error {
	q := sqlc.New(t.DB)
	s := scanner.New(t.DB)

	queued := t.drainQueue()

	var libs []sqlc.Library
	if len(queued) > 0 {
		for _, req := range queued {
			lib, err := q.GetLibraryByID(ctx, req.LibraryID)
			if err != nil {
				log.Warn().Err(err).Int64("library_id", req.LibraryID).Msg("scan task: library not found, skipping")
				progress.Fail("")
				continue
			}
			libs = append(libs, lib)
		}
	} else {
		var err error
		libs, err = q.ListLibraries(ctx)
		if err != nil {
			return err
		}
	}

	progress.SetTotal(len(libs))

	forceMap := make(map[int64]bool)
	for _, req := range queued {
		forceMap[req.LibraryID] = req.Force
	}

	for _, lib := range libs {
		if ctx.Err() != nil {
			return nil
		}

		t.scanOneLibrary(ctx, s, q, lib, forceMap[lib.ID], progress)
	}

	return nil
}

func (t *ScanLibrariesTask) scanOneLibrary(ctx context.Context, s *scanner.Scanner, q *sqlc.Queries, lib sqlc.Library, force bool, progress *ProgressTracker) {
	log.Info().Int64("library_id", lib.ID).Str("name", lib.Name).Msg("scan task: scanning library")
	progress.SetCurrentItem(lib.Name)

	if t.Watcher != nil {
		t.Watcher.Pause(lib.ID)
		defer t.Watcher.Resume(lib.ID)
	}

	if t.Hub != nil {
		t.Hub.Emit(eventhub.EventScanStarted, eventhub.ScanPayload{
			LibraryID:   lib.ID,
			LibraryName: lib.Name,
		})
	}

	opts := scanner.ScanOptions{
		ForceRescan: force,
		OnProgress: func(discovered int, current string) {
			progress.SetDiscovered(discovered, current)
		},
	}
	result, err := s.ScanLibrary(ctx, lib, opts)
	if err != nil {
		log.Error().Err(err).Int64("library_id", lib.ID).Msg("scan task: scan error")
		progress.Fail(lib.Name)
		return
	}

	enqueued := t.enqueuePendingFiles(ctx, q, lib.ID)

	// Music libraries get a fan-out of per-artist refresh jobs after the scan
	// completes (the matcher will create new artist rows when MetadataMatchWorker
	// processes the per-file jobs above; this loop covers existing artists that
	// have stale or never-fetched enrichment data).
	refreshed := 0
	if lib.MediaType == sqlc.MediaTypeMusic {
		refreshed = t.enqueueMusicArtistRefreshes(ctx, q, lib, force)
	}

	log.Info().
		Int64("library_id", lib.ID).
		Int("discovered", result.Discovered).
		Int("new", result.New).
		Int("deleted", result.Deleted).
		Int("enqueued", enqueued).
		Int("music_refreshed", refreshed).
		Msg("scan task: library done")

	if t.Hub != nil {
		t.Hub.Emit(eventhub.EventScanCompleted, eventhub.ScanPayload{
			LibraryID:   lib.ID,
			LibraryName: lib.Name,
			Discovered:  result.Discovered,
			New:         result.New,
			Missing:     result.Deleted,
		})

		if result.Deleted > 0 {
			t.Hub.Emit(eventhub.EventMediaRemoved, eventhub.MediaPayload{LibraryID: lib.ID})
		}
	}

}

// enqueueMusicArtistRefreshes fans out one RefreshMusicArtist job per artist
// in the library. When force=true (operator explicitly re-scanned), it
// refreshes every artist; otherwise it skips artists enriched within the last
// 7 days. The match-side path (MetadataMatchWorker) handles new-artist refresh
// for incremental file adds; this covers existing-artist staleness on scan.
func (t *ScanLibrariesTask) enqueueMusicArtistRefreshes(ctx context.Context, q *sqlc.Queries, lib sqlc.Library, force bool) int {
	artists, err := q.ListArtistsByLibrary(ctx, lib.ID)
	if err != nil {
		log.Warn().Err(err).Int64("library_id", lib.ID).Msg("scan task: list artists failed")
		return 0
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)

	// First pass: figure out batch size so each enqueued job carries the
	// correct total. (Worker reports per-artist progress against this total.)
	var due []sqlc.Artist
	for _, a := range artists {
		if !force && a.EnrichedAt.Valid && a.EnrichedAt.Time.After(cutoff) {
			continue
		}
		due = append(due, a)
	}

	if len(due) > 0 && t.Hub != nil {
		t.Hub.Emit(eventhub.EventScanProgress, eventhub.ScanPayload{
			LibraryID:   lib.ID,
			LibraryName: lib.Name,
			Phase:       "refresh",
			Total:       len(due),
			Done:        0,
		})
	}

	count := 0
	for i, a := range due {
		if _, err := t.River.Insert(ctx, worker.RefreshMusicArtistArgs{
			ArtistID:       a.ID,
			Force:          force,
			BatchLibraryID: lib.ID,
			BatchTotal:     len(due),
			BatchPosition:  i + 1, // 1-indexed; matches MaxWorkers=1 serial order
		}, nil); err != nil {
			log.Warn().Err(err).Int64("artist_id", a.ID).Msg("enqueue RefreshMusicArtist failed")
			continue
		}
		count++
	}
	return count
}

func (t *ScanLibrariesTask) enqueuePendingFiles(ctx context.Context, q *sqlc.Queries, libraryID int64) int {
	files, err := q.ListLibraryFilesByStatus(ctx, sqlc.ListLibraryFilesByStatusParams{
		LibraryID: libraryID,
		Status:    sqlc.FileStatusPending,
		Limit:     100000,
		Offset:    0,
	})
	if err != nil {
		log.Error().Err(err).Int64("library_id", libraryID).Msg("scan task: failed to list pending files")
		return 0
	}

	for _, f := range files {
		t.River.Insert(ctx, worker.ProcessFileArgs{
			LibraryFileID: f.ID,
			LibraryID:     libraryID,
			FilePath:      f.Path,
		}, nil)
	}

	return len(files)
}
