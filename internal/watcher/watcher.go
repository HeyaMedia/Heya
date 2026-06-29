package watcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediafile"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

const debounceDelay = 2 * time.Second

type LibraryWatcher struct {
	libraryID int64
	rootPath  string
	fsw       *fsnotify.Watcher
	cancel    context.CancelFunc
	paused    atomic.Bool
}

type ScanFunc func(libraryID int64, force bool)

type Manager struct {
	mu       sync.Mutex
	watchers map[string]*LibraryWatcher
	db       *pgxpool.Pool
	river    *river.Client[pgx.Tx]
	onScan   ScanFunc
}

func NewManager(db *pgxpool.Pool, riverClient *river.Client[pgx.Tx], onScan ScanFunc) *Manager {
	return &Manager{
		watchers: make(map[string]*LibraryWatcher),
		db:       db,
		river:    riverClient,
		onScan:   onScan,
	}
}

func (m *Manager) StartAll(ctx context.Context) error {
	q := sqlc.New(m.db)
	libs, err := q.ListLibraries(ctx)
	if err != nil {
		return err
	}

	for _, lib := range libs {
		settings := metadata.ParseSettings(lib.Settings)
		if !settings.Watch {
			log.Debug().Int64("library_id", lib.ID).Str("name", lib.Name).Msg("skipping watcher (watch disabled)")
			continue
		}
		for _, p := range lib.Paths {
			if isLocalPath(p) {
				m.Watch(ctx, lib.ID, p)
			}
		}
	}

	log.Info().Int("count", len(m.watchers)).Msg("filesystem watchers started")
	return nil
}

func (m *Manager) Watch(ctx context.Context, libraryID int64, rootPath string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := watcherKey(libraryID, rootPath)
	if _, exists := m.watchers[key]; exists {
		return
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		log.Error().Err(err).Int64("library_id", libraryID).Msg("failed to create watcher")
		return
	}

	if err := addRecursive(fsw, rootPath); err != nil {
		log.Error().Err(err).Str("path", vfs.RedactPath(rootPath)).Msg("failed to add path to watcher")
		fsw.Close()
		return
	}

	wctx, cancel := context.WithCancel(ctx)
	lw := &LibraryWatcher{
		libraryID: libraryID,
		rootPath:  rootPath,
		fsw:       fsw,
		cancel:    cancel,
	}
	m.watchers[key] = lw

	go m.eventLoop(wctx, lw)
	log.Info().Int64("library_id", libraryID).Str("path", vfs.RedactPath(rootPath)).Msg("watching directory")
}

func (m *Manager) Unwatch(libraryID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, lw := range m.watchers {
		if lw.libraryID == libraryID {
			lw.cancel()
			_ = lw.fsw.Close()
			delete(m.watchers, key)
			log.Info().Int64("library_id", libraryID).Str("path", vfs.RedactPath(lw.rootPath)).Msg("stopped watching")
		}
	}
}

func (m *Manager) Pause(libraryID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, lw := range m.watchers {
		if lw.libraryID == libraryID {
			lw.paused.Store(true)
		}
	}
	log.Debug().Int64("library_id", libraryID).Msg("watcher paused")
}

func (m *Manager) Resume(libraryID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, lw := range m.watchers {
		if lw.libraryID == libraryID {
			lw.paused.Store(false)
		}
	}
	log.Debug().Int64("library_id", libraryID).Msg("watcher resumed")
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, lw := range m.watchers {
		lw.cancel()
		lw.fsw.Close()
		delete(m.watchers, key)
	}
	log.Info().Msg("all watchers stopped")
}

func (m *Manager) Status() map[int64]string {
	m.mu.Lock()
	defer m.mu.Unlock()

	status := make(map[int64]string)
	for _, lw := range m.watchers {
		if status[lw.libraryID] == "" {
			status[lw.libraryID] = lw.rootPath
		} else {
			status[lw.libraryID] += "," + lw.rootPath
		}
	}
	return status
}

func watcherKey(libraryID int64, rootPath string) string {
	return strconv.FormatInt(libraryID, 10) + "\x00" + rootPath
}

func (m *Manager) eventLoop(ctx context.Context, lw *LibraryWatcher) {
	pending := make(map[string]*time.Timer)
	var mu sync.Mutex

	for {
		select {
		case <-ctx.Done():
			mu.Lock()
			for _, t := range pending {
				t.Stop()
			}
			mu.Unlock()
			return

		case event, ok := <-lw.fsw.Events:
			if !ok {
				return
			}
			if lw.paused.Load() {
				continue
			}
			m.handleEvent(ctx, lw, event, pending, &mu)

		case err, ok := <-lw.fsw.Errors:
			if !ok {
				return
			}
			log.Error().Err(err).Int64("library_id", lw.libraryID).Msg("watcher error")
		}
	}
}

func (m *Manager) handleEvent(ctx context.Context, lw *LibraryWatcher, event fsnotify.Event, pending map[string]*time.Timer, mu *sync.Mutex) {
	path := event.Name

	if event.Has(fsnotify.Create) {
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		if info.IsDir() {
			name := filepath.Base(path)
			if !strings.HasPrefix(name, ".") && !mediafile.IsExtrasDir(name) {
				addRecursive(lw.fsw, path)
				log.Debug().Str("path", vfs.RedactPath(path)).Msg("watching new directory")
			}
			return
		}
	}

	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		ext := strings.ToLower(filepath.Ext(path))
		if parser.IsMediaExtension(ext) {
			log.Info().Str("path", vfs.RedactPath(path)).Str("op", event.Op.String()).Msg("media file removed")
			m.enqueueSoftDelete(ctx, lw.libraryID, path)
		} else if ext == "" {
			log.Info().Str("path", vfs.RedactPath(path)).Str("op", event.Op.String()).Int64("library_id", lw.libraryID).Msg("directory removed, scheduling rescan")
			m.enqueueRescan(ctx, lw.libraryID)
		}
		return
	}

	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
		ext := strings.ToLower(filepath.Ext(path))
		if !parser.IsMediaExtension(ext) {
			return
		}

		mu.Lock()
		if t, ok := pending[path]; ok {
			t.Stop()
		}
		pending[path] = time.AfterFunc(debounceDelay, func() {
			mu.Lock()
			delete(pending, path)
			mu.Unlock()
			m.enqueueNewFile(ctx, lw, path)
		})
		mu.Unlock()
	}
}

func (m *Manager) enqueueNewFile(ctx context.Context, lw *LibraryWatcher, filePath string) {
	info, err := os.Stat(filePath)
	if err != nil {
		return
	}

	relPath, _ := filepath.Rel(lw.rootPath, filePath)
	parsed := parser.ParseStoragePath(relPath)
	parseJSON, _ := json.Marshal(map[string]any{"parsed": parsed})

	q := sqlc.New(m.db)
	file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID:   lw.libraryID,
		Path:        filePath,
		Size:        info.Size(),
		Mtime:       pgtype.Timestamptz{Time: info.ModTime(), Valid: true},
		ParseResult: parseJSON,
		Status:      sqlc.FileStatusPending,
	})
	if err != nil {
		log.Error().Err(err).Str("path", vfs.RedactPath(filePath)).Msg("upsert failed")
		return
	}

	// fsnotify-discovered file: the user just dropped this into the library,
	// so it jumps ahead of any in-flight bulk scan. PriorityWatcher (1) wins
	// against PriorityScan (2) which is what the scheduler enqueues at.
	if m.river == nil {
		log.Warn().Str("path", vfs.RedactPath(filePath)).Msg("cannot enqueue process file: river client unavailable")
		return
	}
	if _, err := m.river.Insert(ctx, worker.ProcessFileArgs{
		LibraryFileID: file.ID,
		LibraryID:     lw.libraryID,
		FilePath:      filePath,
	}, &river.InsertOpts{Priority: worker.PriorityWatcher}); err != nil {
		log.Warn().Err(err).Str("path", vfs.RedactPath(filePath)).Int64("file_id", file.ID).Msg("enqueue process file failed")
		return
	}

	log.Info().Str("path", relPath).Int64("file_id", file.ID).Msg("new media file detected, enqueued for processing")
}

func (m *Manager) enqueueSoftDelete(ctx context.Context, libraryID int64, path string) {
	if m.river == nil {
		log.Warn().Str("path", vfs.RedactPath(path)).Msg("cannot enqueue soft delete: river client unavailable")
		return
	}
	if _, err := m.river.Insert(ctx, worker.SoftDeleteArgs{
		LibraryID: libraryID,
		Paths:     []string{path},
	}, nil); err != nil {
		log.Warn().Err(err).Str("path", vfs.RedactPath(path)).Int64("library_id", libraryID).Msg("enqueue soft delete failed")
	}
}

var (
	rescanTimers   = make(map[int64]*time.Timer)
	rescanTimersMu sync.Mutex
)

func (m *Manager) enqueueRescan(_ context.Context, libraryID int64) {
	rescanTimersMu.Lock()
	defer rescanTimersMu.Unlock()

	if t, ok := rescanTimers[libraryID]; ok {
		t.Stop()
	}

	rescanTimers[libraryID] = time.AfterFunc(5*time.Second, func() {
		rescanTimersMu.Lock()
		delete(rescanTimers, libraryID)
		rescanTimersMu.Unlock()

		m.onScan(libraryID, false)
		log.Info().Int64("library_id", libraryID).Msg("rescan enqueued after directory change")
	})
}

func addRecursive(fsw *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || mediafile.IsExtrasDir(name) || mediafile.IsSkipDir(name) {
				return filepath.SkipDir
			}
			return fsw.Add(path)
		}
		return nil
	})
}

func isLocalPath(p string) bool {
	return !strings.HasPrefix(p, "smb://")
}
