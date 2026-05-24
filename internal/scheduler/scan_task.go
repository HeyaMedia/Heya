package scheduler

import (
	"context"
	"sort"
	"sync"

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

	// Re-drain after each library so any Enqueue()s that arrived during the
	// previous scan (e.g. adding 4 libraries back-to-back, where each post-1
	// Enqueue's TriggerNow gets rejected as "already running" but the queue
	// entry still sits in t.queue) get picked up without needing the
	// scheduler to trigger us again.
	processed := make(map[int64]bool)
	totalSeen := 0

	for {
		if ctx.Err() != nil {
			return nil
		}

		queued := t.drainQueue()

		var libs []sqlc.Library
		forceMap := make(map[int64]bool)
		if len(queued) > 0 {
			for _, req := range queued {
				if processed[req.LibraryID] {
					continue
				}
				lib, err := q.GetLibraryByID(ctx, req.LibraryID)
				if err != nil {
					log.Warn().Err(err).Int64("library_id", req.LibraryID).Msg("scan task: library not found, skipping")
					progress.Fail("")
					continue
				}
				libs = append(libs, lib)
				forceMap[req.LibraryID] = req.Force
			}
		} else if totalSeen == 0 {
			// First iteration with no explicit queue: scan every library (the
			// scheduled-cadence path). Skip on subsequent iterations so we
			// don't keep rescanning everything every loop.
			var err error
			libs, err = q.ListLibraries(ctx)
			if err != nil {
				return err
			}
		}

		if len(libs) == 0 {
			return nil
		}

		// Process in the same priority order the enrich queue uses:
		// movies → tv → music → books. Otherwise users on a fresh DB watch
		// libraries fill in whatever order ListLibraries happened to
		// return, which is jarring when their main library type is buried
		// under another.
		sortLibrariesByPriority(libs)

		totalSeen += len(libs)
		progress.SetTotal(totalSeen)

		for _, lib := range libs {
			if ctx.Err() != nil {
				return nil
			}
			t.scanOneLibrary(ctx, s, q, lib, forceMap[lib.ID], progress)
			processed[lib.ID] = true
		}
	}
}

// sortLibrariesByPriority orders libs by media_type to match the enrich
// queue's priority bands: movies first, then tv (covers an "Anime"-named
// library which is still media_type=tv), then music, then books. Stable
// — libraries inside the same media_type keep their relative order.
func sortLibrariesByPriority(libs []sqlc.Library) {
	rank := func(mt sqlc.MediaType) int {
		switch mt {
		case sqlc.MediaTypeMovie:
			return 0
		case sqlc.MediaTypeTv:
			return 1
		case sqlc.MediaTypeMusic:
			return 2
		case sqlc.MediaTypeBook:
			return 3
		}
		return 4
	}
	sort.SliceStable(libs, func(i, j int) bool {
		return rank(libs[i].MediaType) < rank(libs[j].MediaType)
	})
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

	// Stale-artist refresh used to be fanned out here for music libraries.
	// After the match/enrich split, stub-matched items get picked up by
	// refresh_stale_items naturally (metadata_refreshed_at is NULL until
	// the first enrich), so this fan-out is redundant.
	_ = force

	log.Info().
		Int64("library_id", lib.ID).
		Int("discovered", result.Discovered).
		Int("new", result.New).
		Int("deleted", result.Deleted).
		Int("enqueued", enqueued).
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
