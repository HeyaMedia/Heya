package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediafile"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

const debounceDelay = 2 * time.Second

// Variables so the coalescing behaviour can be exercised without slow tests.
var (
	rescanDebounceDelay        = 5 * time.Second
	softDeleteDebounceDelay    = 2 * time.Second
	sidecarRescanDebounceDelay = 2 * time.Second
)

// watchWalkStallTimeout bounds *stalls* in the recursive directory walk when
// arming a watcher — not total walk time. A big tree under heavy I/O pressure
// (degraded pool, concurrent write storm) can legitimately take minutes to
// walk and must still arm eventually; a stalled network mount or suspended
// pool instead blocks forever in a Getdents syscall that neither context nor
// a deadline can interrupt, which shows up as the walk visiting nothing at
// all. Only when a full window passes with zero new entries do we give up
// live-watching that path (periodic rescans still catch changes) rather than
// wedge the whole watcher subsystem. A var so tests can shrink the window.
var watchWalkStallTimeout = 60 * time.Second

type LibraryWatcher struct {
	libraryID  int64
	rootPath   string
	fsw        *fsnotify.Watcher
	cancel     context.CancelFunc
	pauseDepth atomic.Int32
}

type ScanFunc func(libraryID int64, force bool)

type pendingSoftDelete struct {
	paths map[string]struct{}
	timer *time.Timer
}

type Manager struct {
	mu                  sync.Mutex
	watchers            map[string]*LibraryWatcher
	db                  *pgxpool.Pool
	river               *river.Client[pgx.Tx]
	onScan              ScanFunc
	softDeleteMu        sync.Mutex
	softDeletes         map[int64]*pendingSoftDelete
	softDeleteInsert    func(context.Context, worker.SoftDeleteArgs) error
	sidecarRescanMu     sync.Mutex
	sidecarRescans      map[int64]*pendingSoftDelete
	sidecarRescanInsert func(context.Context, int64, []string) error
	rescanMu            sync.Mutex
	rescanTimers        map[int64]*time.Timer
}

func NewManager(db *pgxpool.Pool, riverClient *river.Client[pgx.Tx], onScan ScanFunc) *Manager {
	m := &Manager{
		watchers:       make(map[string]*LibraryWatcher),
		db:             db,
		river:          riverClient,
		onScan:         onScan,
		softDeletes:    make(map[int64]*pendingSoftDelete),
		sidecarRescans: make(map[int64]*pendingSoftDelete),
		rescanTimers:   make(map[int64]*time.Timer),
	}
	if riverClient != nil {
		m.softDeleteInsert = func(ctx context.Context, args worker.SoftDeleteArgs) error {
			_, err := riverClient.Insert(ctx, args, nil)
			return err
		}
		m.sidecarRescanInsert = m.insertSidecarRescan
	}
	return m
}

func (m *Manager) StartAll(ctx context.Context) error {
	q := sqlc.New(m.db)
	libs, err := q.ListLibraries(ctx)
	if err != nil {
		return err
	}

	for _, lib := range libs {
		m.SyncLibrary(ctx, lib)
	}

	log.Info().Msg("filesystem watchers arming in background")
	return nil
}

func (m *Manager) SyncLibrary(ctx context.Context, lib sqlc.Library) {
	m.Unwatch(lib.ID)

	settings := metadata.ParseSettings(lib.Settings)
	if !settings.Watch {
		log.Debug().Int64("library_id", lib.ID).Str("name", lib.Name).Msg("skipping watcher (watch disabled)")
		return
	}

	for _, p := range lib.Paths {
		if isLocalPath(p) {
			// Arm each watcher concurrently: the recursive walk can be slow
			// (or stall on a flaky mount), and one library must never block
			// startup or its siblings. Watch is self-synchronizing.
			go m.Watch(ctx, lib.ID, p)
		}
	}
}

func (m *Manager) Watch(ctx context.Context, libraryID int64, rootPath string) {
	key := watcherKey(libraryID, rootPath)

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		log.Error().Err(err).Int64("library_id", libraryID).Msg("failed to create watcher")
		return
	}
	wctx, cancel := context.WithCancel(ctx)
	lw := &LibraryWatcher{
		libraryID: libraryID,
		rootPath:  rootPath,
		fsw:       fsw,
		cancel:    cancel,
	}

	// Reserve the slot BEFORE the (unlocked, possibly slow) walk — with the real
	// fsw + cancel in place — so a concurrent Unwatch can find and tear us down
	// mid-arm. The commit check after the walk then refuses to resurrect a
	// library that was unwatched while arming. This also dedups concurrent Watch
	// calls for the same key.
	m.mu.Lock()
	if _, exists := m.watchers[key]; exists {
		m.mu.Unlock()
		cancel()
		_ = fsw.Close()
		return
	}
	m.watchers[key] = lw
	m.mu.Unlock()

	// Arm the recursive watch WITHOUT holding m.mu (a stalled mount must not
	// deadlock Pause/Resume/Unwatch or any scan that toggles the watcher) and
	// with a timeout; wctx lets Unwatch abort a slow arm.
	walkErr := addRecursiveBounded(wctx, fsw, rootPath)

	m.mu.Lock()
	mine := m.watchers[key] == lw
	if mine && walkErr != nil {
		delete(m.watchers, key)
	}
	m.mu.Unlock()

	if !mine {
		// Unwatch removed us during arming; it already cancelled + closed our
		// fsw, so do NOT resurrect the watcher.
		return
	}
	if walkErr != nil {
		log.Error().Err(walkErr).Str("path", vfs.RedactPath(rootPath)).Msg("failed to arm recursive watch; library falls back to periodic rescans")
		cancel()
		_ = fsw.Close()
		return
	}

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
			lw.pauseDepth.Add(1)
		}
	}
	log.Debug().Int64("library_id", libraryID).Msg("watcher paused")
}

func (m *Manager) Resume(libraryID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, lw := range m.watchers {
		if lw.libraryID == libraryID {
			for {
				depth := lw.pauseDepth.Load()
				if depth <= 0 {
					break
				}
				if lw.pauseDepth.CompareAndSwap(depth, depth-1) {
					break
				}
			}
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
	m.stopPendingEnqueues()
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
			if lw.pauseDepth.Load() > 0 {
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
				// Bounded like the initial arm — a new subdir on a stalled mount
				// must not wedge this library's eventLoop in an uninterruptible
				// Getdents (the gap the v0.1.10 arm-stall fix didn't cover).
				if err := addRecursiveBounded(ctx, lw.fsw, path); err != nil {
					log.Warn().Err(err).Str("path", vfs.RedactPath(path)).Msg("failed to watch new directory (will rely on periodic rescans)")
				} else {
					log.Debug().Str("path", vfs.RedactPath(path)).Msg("watching new directory")
				}
				m.enqueueScannerRescan(ctx, lw.libraryID, path)
			}
			return
		}
	}

	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		ext := strings.ToLower(filepath.Ext(path))
		if isPrimaryMediaPath(path) {
			log.Info().Str("path", vfs.RedactPath(path)).Str("op", event.Op.String()).Msg("primary media removed; batching soft delete")
			m.enqueueSoftDelete(ctx, lw.libraryID, path)
		} else if isSidecarTriggerPath(path) {
			// Sidecars never have library_files rows, so soft-deleting them one by
			// one is both ineffective and catastrophically noisy during a bulk
			// cleanup. Coalesce their owner scopes behind one process coordinator.
			log.Info().Str("path", vfs.RedactPath(path)).Str("op", event.Op.String()).Int64("library_id", lw.libraryID).Msg("scanner sidecar removed; scheduling coalesced scoped rescan")
			m.enqueueSidecarRescan(ctx, lw.libraryID, path)
		} else if ext == "" {
			log.Info().Str("path", vfs.RedactPath(path)).Str("op", event.Op.String()).Int64("library_id", lw.libraryID).Msg("directory removed, scheduling rescan")
			m.enqueueRescan(ctx, lw.libraryID)
		}
		return
	}

	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
		if !isScannerTriggerPath(path) {
			return
		}

		dir := filepath.Dir(path)
		key := strconv.FormatInt(lw.libraryID, 10) + "\x00" + dir
		mu.Lock()
		if t, ok := pending[key]; ok {
			t.Stop()
		}
		pending[key] = time.AfterFunc(debounceDelay, func() {
			mu.Lock()
			delete(pending, key)
			mu.Unlock()
			m.enqueueScannerRescan(ctx, lw.libraryID, path)
		})
		mu.Unlock()
	}
}

func (m *Manager) enqueueSoftDelete(ctx context.Context, libraryID int64, path string) {
	if m.softDeleteInsert == nil {
		log.Warn().Str("path", vfs.RedactPath(path)).Msg("cannot enqueue soft delete: river client unavailable")
		return
	}

	m.softDeleteMu.Lock()
	defer m.softDeleteMu.Unlock()
	if m.softDeletes == nil {
		m.softDeletes = make(map[int64]*pendingSoftDelete)
	}
	batch := m.softDeletes[libraryID]
	if batch == nil {
		batch = &pendingSoftDelete{paths: make(map[string]struct{})}
		m.softDeletes[libraryID] = batch
	}
	batch.paths[path] = struct{}{}
	if batch.timer != nil {
		batch.timer.Stop()
	}
	batch.timer = time.AfterFunc(softDeleteDebounceDelay, func() { m.flushSoftDeletes(libraryID) })
	_ = ctx // insertion deliberately outlives the individual fsnotify event
}

func (m *Manager) flushSoftDeletes(libraryID int64) {
	m.softDeleteMu.Lock()
	batch := m.softDeletes[libraryID]
	delete(m.softDeletes, libraryID)
	m.softDeleteMu.Unlock()
	if batch == nil || len(batch.paths) == 0 {
		return
	}

	paths := make([]string, 0, len(batch.paths))
	for path := range batch.paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := m.softDeleteInsert(ctx, worker.SoftDeleteArgs{LibraryID: libraryID, Paths: paths}); err != nil {
		log.Warn().Err(err).Int64("library_id", libraryID).Int("count", len(paths)).Msg("enqueue batched soft delete failed")
		return
	}
	log.Info().Int64("library_id", libraryID).Int("count", len(paths)).Msg("batched soft delete enqueued")
}

func (m *Manager) enqueueSidecarRescan(ctx context.Context, libraryID int64, path string) {
	if m.sidecarRescanInsert == nil {
		log.Warn().Str("path", vfs.RedactPath(path)).Msg("cannot enqueue sidecar rescan: river client unavailable")
		return
	}
	m.sidecarRescanMu.Lock()
	defer m.sidecarRescanMu.Unlock()
	if m.sidecarRescans == nil {
		m.sidecarRescans = make(map[int64]*pendingSoftDelete)
	}
	batch := m.sidecarRescans[libraryID]
	if batch == nil {
		batch = &pendingSoftDelete{paths: make(map[string]struct{})}
		m.sidecarRescans[libraryID] = batch
	}
	batch.paths[path] = struct{}{}
	if batch.timer != nil {
		batch.timer.Stop()
	}
	batch.timer = time.AfterFunc(sidecarRescanDebounceDelay, func() { m.flushSidecarRescans(libraryID) })
	_ = ctx // insertion deliberately outlives the individual fsnotify event
}

func (m *Manager) flushSidecarRescans(libraryID int64) {
	m.sidecarRescanMu.Lock()
	batch := m.sidecarRescans[libraryID]
	delete(m.sidecarRescans, libraryID)
	m.sidecarRescanMu.Unlock()
	if batch == nil || len(batch.paths) == 0 {
		return
	}
	paths := make([]string, 0, len(batch.paths))
	for path := range batch.paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := m.sidecarRescanInsert(ctx, libraryID, paths); err != nil {
		log.Warn().Err(err).Int64("library_id", libraryID).Int("count", len(paths)).Msg("enqueue coalesced sidecar rescan failed")
		return
	}
	log.Info().Int64("library_id", libraryID).Int("count", len(paths)).Msg("coalesced sidecar rescan enqueued")
}

func (m *Manager) insertSidecarRescan(ctx context.Context, libraryID int64, triggerPaths []string) error {
	lib, err := sqlc.New(m.db).GetLibraryByID(ctx, libraryID)
	if err != nil {
		return fmt.Errorf("library lookup: %w", err)
	}
	scopes := scannerScopesForTriggerPaths(lib, triggerPaths)
	if len(scopes) == 0 {
		return nil
	}
	return worker.EnqueueProcessLibraryScan(ctx, m.river, m.db, worker.ProcessLibraryScanArgs{
		LibraryID:  libraryID,
		MediaType:  lib.MediaType,
		ScopePaths: scopes,
		Force:      true,
	}, worker.PriorityWatcher, "")
}

func scannerScopesForTriggerPaths(lib sqlc.Library, triggerPaths []string) []string {
	scopeSet := make(map[string]struct{}, len(triggerPaths))
	for _, path := range triggerPaths {
		if scope := worker.ScannerScopeForLibraryPath(lib, path); scope != "" {
			scopeSet[scope] = struct{}{}
		}
	}
	scopes := make([]string, 0, len(scopeSet))
	for scope := range scopeSet {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	return scopes
}

func (m *Manager) enqueueScannerRescan(ctx context.Context, libraryID int64, triggerPath string) {
	if m.river == nil {
		if m.onScan != nil {
			m.onScan(libraryID, false)
			log.Info().Int64("library_id", libraryID).Str("path", vfs.RedactPath(triggerPath)).Msg("watcher-triggered scanner run enqueued via direct callback")
			return
		}
		log.Warn().Int64("library_id", libraryID).Str("path", vfs.RedactPath(triggerPath)).Msg("cannot enqueue scanner run: river client unavailable")
		return
	}
	lib, err := sqlc.New(m.db).GetLibraryByID(ctx, libraryID)
	if err != nil {
		log.Warn().Err(err).Int64("library_id", libraryID).Str("path", vfs.RedactPath(triggerPath)).Msg("enqueue scanner run failed: library lookup failed")
		return
	}
	args := worker.ProcessLibraryScanArgs{
		LibraryID:  libraryID,
		MediaType:  lib.MediaType,
		ScopePaths: []string{worker.ScannerScopeForLibraryPath(lib, triggerPath)},
	}
	if err := worker.EnqueueProcessLibraryScan(ctx, m.river, m.db, args, worker.PriorityWatcher, ""); err != nil {
		log.Warn().Err(err).Int64("library_id", libraryID).Str("path", vfs.RedactPath(triggerPath)).Msg("enqueue scanner run failed")
		return
	}
	log.Info().Int64("library_id", libraryID).Str("path", vfs.RedactPath(triggerPath)).Msg("watcher-triggered scanner run enqueued")
}

func (m *Manager) enqueueRescan(_ context.Context, libraryID int64) {
	m.rescanMu.Lock()
	defer m.rescanMu.Unlock()
	if m.rescanTimers == nil {
		m.rescanTimers = make(map[int64]*time.Timer)
	}

	if t, ok := m.rescanTimers[libraryID]; ok {
		t.Stop()
	}

	m.rescanTimers[libraryID] = time.AfterFunc(rescanDebounceDelay, func() {
		m.rescanMu.Lock()
		delete(m.rescanTimers, libraryID)
		m.rescanMu.Unlock()

		if m.onScan != nil {
			m.onScan(libraryID, false)
		}
		log.Info().Int64("library_id", libraryID).Msg("coalesced library rescan enqueued after filesystem change")
	})
}

func (m *Manager) stopPendingEnqueues() {
	m.softDeleteMu.Lock()
	for libraryID, batch := range m.softDeletes {
		if batch.timer != nil {
			batch.timer.Stop()
		}
		delete(m.softDeletes, libraryID)
	}
	m.softDeleteMu.Unlock()

	m.sidecarRescanMu.Lock()
	for libraryID, batch := range m.sidecarRescans {
		if batch.timer != nil {
			batch.timer.Stop()
		}
		delete(m.sidecarRescans, libraryID)
	}
	m.sidecarRescanMu.Unlock()

	m.rescanMu.Lock()
	for libraryID, timer := range m.rescanTimers {
		timer.Stop()
		delete(m.rescanTimers, libraryID)
	}
	m.rescanMu.Unlock()
}

func isScannerTriggerPath(path string) bool {
	return isPrimaryMediaPath(path) || isSidecarTriggerPath(path)
}

func isPrimaryMediaPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(filepath.Base(path)))
	return mediafile.IsVideoExt(ext) || mediafile.IsAudioExt(ext)
}

func isSidecarTriggerPath(path string) bool {
	name := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case mediafile.IsImageExt(ext), mediafile.IsSubtitleExt(ext), mediafile.IsLyricsExt(ext):
		return true
	case ext == ".nfo":
		return true
	case strings.EqualFold(name, ".plexmatch"):
		return true
	default:
		return false
	}
}

// addRecursiveBounded runs addRecursive with a stall watchdog. The walk issues
// blocking Getdents syscalls that neither context nor a deadline can interrupt
// once a mount stalls, so it runs in a goroutine that we abandon on stall.
// The orphaned goroutine holds no locks and returns (into a buffered channel)
// if the mount ever recovers, so it can't wedge anything — worst case one
// leaked goroutine per stalled arm attempt. Total walk time is unbounded on
// purpose: a huge tree on a slow or busy disk keeps making progress and must
// arm eventually. Only a window with zero visited entries — the stalled-mount
// signature — is surfaced as an error so the caller skips live-watching that
// path.
// recursiveWalkFn is the walk implementation, overridable in tests so the
// arm/unwatch race and the stall watchdog can be exercised deterministically.
var recursiveWalkFn = addRecursive

func addRecursiveBounded(ctx context.Context, fsw *fsnotify.Watcher, root string) error {
	var visited atomic.Int64
	done := make(chan error, 1)
	walk := recursiveWalkFn // capture before spawning: the goroutine may outlive a test's stub swap
	go func() { done <- walk(fsw, root, &visited) }()

	ticker := time.NewTicker(watchWalkStallTimeout)
	defer ticker.Stop()
	var last int64
	for {
		select {
		case err := <-done:
			return err
		case <-ticker.C:
			if n := visited.Load(); n == last {
				return fmt.Errorf("recursive watch of %s stalled after %d entries (no progress in %s; stalled mount?)", vfs.RedactPath(root), n, watchWalkStallTimeout)
			} else {
				last = n
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func addRecursive(fsw *fsnotify.Watcher, root string, visited *atomic.Int64) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		visited.Add(1)
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
