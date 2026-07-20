package watcher

import (
	"context"
	"crypto/sha256"
	"errors"
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
	"github.com/karbowiak/heya/internal/atomicfile"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediafile"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// Variables so the coalescing behaviour can be exercised without slow tests.
var (
	eventDebounceDelay         = 2 * time.Second
	rescanDebounceDelay        = 5 * time.Second
	softDeleteDebounceDelay    = 2 * time.Second
	sidecarRescanDebounceDelay = 2 * time.Second
	generatedWriteTTL          = 2 * time.Minute
)

const maxGeneratedWriteSuppressions = 10_000

// watchWalkStallTimeout bounds *stalls* in the recursive directory walk when
// arming a watcher — not total walk time. A big tree under heavy I/O pressure
// (degraded pool, concurrent write storm) can legitimately take minutes to
// walk and must still arm eventually; a stalled network mount or suspended
// pool instead blocks forever in a Getdents syscall that neither context nor
// a deadline can interrupt, which shows up as the walk visiting nothing at
// all. Only when a full window passes with zero new entries do we give up
// live-watching that path (the scheduled library scan remains the safety
// net) rather than wedge the whole watcher subsystem. A var so tests can
// shrink the window.
var watchWalkStallTimeout = 60 * time.Second

type LibraryWatcher struct {
	libraryID  int64
	rootPath   string
	fsw        *fsnotify.Watcher
	ctx        context.Context
	cancel     context.CancelFunc
	generation uint64
	pauseDepth atomic.Int32
	dirty      atomic.Bool
	pendingMu  sync.Mutex
	pending    map[string]*pendingWatcherEvent
}

// pendingWatcherEvent keeps every path that contributed to one directory's
// debounce window. A generated sidecar must not hide a real audio/video edit
// merely because the sidecar happened to be the last event in that directory.
type pendingWatcherEvent struct {
	paths map[string]struct{}
	timer *time.Timer
}

type ScanFunc func(libraryID int64, force bool)

type pendingSoftDelete struct {
	paths      map[string]struct{}
	timer      *time.Timer
	generation uint64
	version    uint64
	ctx        context.Context
}

type pendingRescan struct {
	timer      *time.Timer
	generation uint64
	ctx        context.Context
}

// generationActivity is the join point for every asynchronous operation that
// belongs to one library generation: its fsnotify event loops and timer
// callbacks that passed the generation admission check. Add is always made
// while Manager.mu is held, before teardown can invalidate the generation;
// teardown waits only after releasing every manager/library queue lock.
type generationActivity struct {
	wg sync.WaitGroup
}

// watchWalkAttempt identifies one recursive root walk that has not returned
// yet. addRecursiveBounded may abandon that walk when the underlying filesystem
// is stuck in an uninterruptible syscall; retaining the attempt prevents the
// durable reconciler from starting another walk for the same root every tick.
// The original walk goroutine removes its own attempt when it eventually exits.
type watchWalkAttempt struct{}

type generatedWriteSignature struct {
	size         int64
	modTimeNanos int64
	sha256       [sha256.Size]byte
}

type generatedWriteSuppression struct {
	signature generatedWriteSignature
	expiresAt time.Time
	recorded  time.Time
}

type Manager struct {
	reconcileMu         sync.Mutex
	mu                  sync.Mutex
	watchers            map[string]*LibraryWatcher
	watchWalks          map[string]*watchWalkAttempt
	desiredRoots        map[int64]map[string]struct{}
	pauseDepths         map[int64]int32
	generations         map[int64]uint64
	activities          map[int64]map[uint64]*generationActivity
	stopped             bool
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
	rescanTimers        map[int64]*pendingRescan
	generatedWriteMu    sync.Mutex
	generatedWrites     map[string]generatedWriteSuppression
	now                 func() time.Time
}

func NewManager(db *pgxpool.Pool, riverClient *river.Client[pgx.Tx], onScan ScanFunc) *Manager {
	m := &Manager{
		watchers:        make(map[string]*LibraryWatcher),
		watchWalks:      make(map[string]*watchWalkAttempt),
		desiredRoots:    make(map[int64]map[string]struct{}),
		pauseDepths:     make(map[int64]int32),
		generations:     make(map[int64]uint64),
		activities:      make(map[int64]map[uint64]*generationActivity),
		db:              db,
		river:           riverClient,
		onScan:          onScan,
		softDeletes:     make(map[int64]*pendingSoftDelete),
		sidecarRescans:  make(map[int64]*pendingSoftDelete),
		rescanTimers:    make(map[int64]*pendingRescan),
		generatedWrites: make(map[string]generatedWriteSuppression),
		now:             time.Now,
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
	if err := m.Reconcile(ctx); err != nil {
		return err
	}
	log.Info().Msg("filesystem watchers arming in background")
	return nil
}

// Reconcile makes the live watcher set converge on the durable libraries
// table. Postgres NOTIFY remains the low-latency path, while periodic calls to
// Reconcile repair any create/update/delete event lost during a relay reconnect.
func (m *Manager) Reconcile(ctx context.Context) error {
	m.reconcileMu.Lock()
	defer m.reconcileMu.Unlock()

	q := sqlc.New(m.db)
	libs, err := q.ListLibraries(ctx)
	if err != nil {
		return err
	}
	m.reconcileLibraries(ctx, libs)
	return nil
}

func (m *Manager) reconcileLibraries(ctx context.Context, libs []sqlc.Library) {
	present := make(map[int64]struct{}, len(libs))
	for _, lib := range libs {
		present[lib.ID] = struct{}{}
		m.syncLibraryIfChanged(ctx, lib)
	}

	for libraryID := range m.managedLibraryIDsSnapshot() {
		if _, ok := present[libraryID]; !ok {
			m.resetLibrary(libraryID)
			m.forgetLibraryState(libraryID)
		}
	}
}

func (m *Manager) SyncLibrary(ctx context.Context, lib sqlc.Library) {
	m.reconcileMu.Lock()
	defer m.reconcileMu.Unlock()
	m.syncLibraryIfChanged(ctx, lib)
}

func (m *Manager) syncLibraryIfChanged(ctx context.Context, lib sqlc.Library) {
	desired := desiredWatcherRoots(lib)
	previous, known, actual := m.libraryWatcherStateSnapshot(lib.ID, desired)
	if known && sameWatcherRoots(previous, desired) && sameWatcherRoots(actual, desired) {
		return
	}
	m.syncLibrary(ctx, lib)
	m.mu.Lock()
	m.initLifecycleMapsLocked()
	m.desiredRoots[lib.ID] = cloneWatcherRoots(desired)
	m.mu.Unlock()
}

func (m *Manager) syncLibrary(ctx context.Context, lib sqlc.Library) {
	settings := metadata.ParseSettings(lib.Settings)
	generation := m.resetLibrary(lib.ID)
	if !settings.Watch {
		log.Debug().Int64("library_id", lib.ID).Str("name", lib.Name).Msg("skipping watcher (watch disabled)")
		return
	}

	for _, p := range lib.Paths {
		if err := vfs.ValidateLocalPath(p); err != nil {
			log.Error().Err(err).Int64("library_id", lib.ID).Str("path", vfs.RedactPath(p)).Msg("cannot watch unsupported library path; update the library configuration")
			continue
		}
		// Arm each watcher concurrently: the recursive walk can be slow
		// (or stall on a mounted network filesystem), and one library must never
		// block startup or its siblings. Watch is self-synchronizing.
		go m.watch(ctx, lib.ID, p, generation)
	}
}

func desiredWatcherRoots(lib sqlc.Library) map[string]struct{} {
	roots := make(map[string]struct{})
	if !metadata.ParseSettings(lib.Settings).Watch {
		return roots
	}
	for _, root := range lib.Paths {
		if vfs.ValidateLocalPath(root) == nil {
			roots[root] = struct{}{}
		}
	}
	return roots
}

func (m *Manager) libraryWatcherStateSnapshot(libraryID int64, desired map[string]struct{}) (map[string]struct{}, bool, map[string]struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initLifecycleMapsLocked()
	previous, known := m.desiredRoots[libraryID]
	actual := make(map[string]struct{})
	for _, watcher := range m.watchers {
		if watcher.libraryID == libraryID {
			actual[watcher.rootPath] = struct{}{}
		}
	}
	// A bounded wrapper can return while its underlying recursive walk remains
	// blocked in the kernel. Count only still-desired roots covered by such a
	// walk: this suppresses duplicate retries without making an obsolete root
	// keep a changed library perpetually out of sync.
	for root := range desired {
		if m.watchWalks[watcherKey(libraryID, root)] != nil {
			actual[root] = struct{}{}
		}
	}
	return cloneWatcherRoots(previous), known, actual
}

func (m *Manager) managedLibraryIDsSnapshot() map[int64]struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initLifecycleMapsLocked()
	ids := make(map[int64]struct{}, len(m.desiredRoots))
	for libraryID := range m.desiredRoots {
		ids[libraryID] = struct{}{}
	}
	for _, watcher := range m.watchers {
		ids[watcher.libraryID] = struct{}{}
	}
	return ids
}

func (m *Manager) forgetLibraryState(libraryID int64) {
	m.mu.Lock()
	delete(m.desiredRoots, libraryID)
	delete(m.pauseDepths, libraryID)
	delete(m.generations, libraryID)
	m.mu.Unlock()
}

func cloneWatcherRoots(roots map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(roots))
	for root := range roots {
		cloned[root] = struct{}{}
	}
	return cloned
}

func sameWatcherRoots(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for root := range left {
		if _, ok := right[root]; !ok {
			return false
		}
	}
	return true
}

func (m *Manager) Watch(ctx context.Context, libraryID int64, rootPath string) {
	m.mu.Lock()
	m.initLifecycleMapsLocked()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	generation := m.generations[libraryID]
	if generation == 0 {
		generation = 1
		m.generations[libraryID] = generation
	}
	m.mu.Unlock()

	m.watch(ctx, libraryID, rootPath, generation)
}

func (m *Manager) watch(ctx context.Context, libraryID int64, rootPath string, generation uint64) {
	key := watcherKey(libraryID, rootPath)

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		log.Error().Err(err).Int64("library_id", libraryID).Msg("failed to create watcher")
		return
	}
	wctx, cancel := context.WithCancel(ctx)
	lw := &LibraryWatcher{
		libraryID:  libraryID,
		rootPath:   rootPath,
		fsw:        fsw,
		ctx:        wctx,
		cancel:     cancel,
		generation: generation,
	}

	// Reserve the slot BEFORE the (unlocked, possibly slow) walk — with the real
	// fsw + cancel in place — so a concurrent Unwatch can find and tear us down
	// mid-arm. The commit check after the walk then refuses to resurrect a
	// library that was unwatched while arming. This also dedups concurrent Watch
	// calls for the same key.
	m.mu.Lock()
	m.initLifecycleMapsLocked()
	if m.stopped || m.generations[libraryID] != generation {
		m.mu.Unlock()
		cancel()
		_ = fsw.Close()
		return
	}
	if _, exists := m.watchers[key]; exists {
		m.mu.Unlock()
		cancel()
		_ = fsw.Close()
		return
	}
	if m.watchWalks[key] != nil {
		m.mu.Unlock()
		cancel()
		_ = fsw.Close()
		return
	}
	attempt := &watchWalkAttempt{}
	lw.pauseDepth.Store(m.pauseDepths[libraryID])
	m.watchers[key] = lw
	m.watchWalks[key] = attempt
	m.mu.Unlock()

	// Arm the recursive watch WITHOUT holding m.mu (a stalled mount must not
	// deadlock Pause/Resume/Unwatch or any scan that toggles the watcher) and
	// with a timeout; wctx lets Unwatch abort a slow arm.
	walkErr := addRecursiveBoundedWithExit(wctx, fsw, rootPath, func() {
		m.finishWatchWalk(key, attempt)
	})

	m.mu.Lock()
	mine := !m.stopped && m.generations[libraryID] == generation && m.watchers[key] == lw
	if mine && walkErr != nil {
		delete(m.watchers, key)
	} else if mine {
		// Launch while the successful commit still owns m.mu. Teardown can now
		// linearize either before this commit (mine=false) or after a live loop
		// exists and can observe cancellation; there is no committed-but-not-yet-
		// started gap.
		activity := m.activityLocked(libraryID, generation)
		activity.wg.Add(1)
		go func() {
			defer activity.wg.Done()
			m.eventLoop(wctx, lw)
		}()
		log.Info().Int64("library_id", libraryID).Str("path", vfs.RedactPath(rootPath)).Msg("watching directory")
	}
	m.mu.Unlock()

	if !mine {
		// Unwatch removed us during arming; it already cancelled + closed our
		// fsw, so do NOT resurrect the watcher.
		return
	}
	if walkErr != nil {
		log.Error().Err(vfs.RedactError(walkErr)).Str("path", vfs.RedactPath(rootPath)).Msg("failed to arm recursive watch; scheduled library scans remain available")
		cancel()
		_ = fsw.Close()
		return
	}

}

func (m *Manager) finishWatchWalk(key string, attempt *watchWalkAttempt) {
	m.mu.Lock()
	if m.watchWalks[key] == attempt {
		delete(m.watchWalks, key)
	}
	m.mu.Unlock()
}

func (m *Manager) Unwatch(libraryID int64) {
	m.reconcileMu.Lock()
	defer m.reconcileMu.Unlock()
	m.resetLibrary(libraryID)
	m.forgetLibraryState(libraryID)
}

// resetLibrary invalidates all work belonging to the current generation and
// returns the generation a subsequent reconciliation may arm. Pause depth is
// deliberately retained: a scan may pause a library while its watcher is
// being rebuilt, and the replacement must inherit that pause.
func (m *Manager) resetLibrary(libraryID int64) uint64 {
	m.mu.Lock()
	m.initLifecycleMapsLocked()
	m.generations[libraryID]++
	if m.generations[libraryID] == 0 {
		// Generation zero is reserved for tests and legacy zero-value watcher
		// values. In practice this only matters after uint64 overflow.
		m.generations[libraryID] = 1
	}
	generation := m.generations[libraryID]
	activities := m.activitiesForLibraryLocked(libraryID)
	var removed []*LibraryWatcher

	for key, lw := range m.watchers {
		if lw.libraryID == libraryID {
			delete(m.watchers, key)
			removed = append(removed, lw)
		}
	}
	m.mu.Unlock()

	// Closing fsnotify can wait for its internal goroutines, so never do it
	// while holding the manager lock. Marking the generation stale first also
	// prevents an event already in flight from installing new debounce work.
	for _, lw := range removed {
		lw.stopPendingEvents()
		lw.cancel()
		_ = lw.fsw.Close()
		log.Info().Int64("library_id", libraryID).Str("path", vfs.RedactPath(lw.rootPath)).Msg("stopped watching")
	}
	m.cancelPendingEnqueues(libraryID, generation)
	for _, activity := range activities {
		activity.wg.Wait()
	}
	// An event loop may have entered handleEvent immediately before
	// invalidation and installed a local debounce after the first drain. It is
	// joined now, so this second drain closes that final scheduling window.
	for _, lw := range removed {
		lw.stopPendingEvents()
	}
	m.forgetActivities(libraryID, activities)
	return generation
}

func (m *Manager) Pause(libraryID int64) {
	m.mu.Lock()
	m.initLifecycleMapsLocked()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	depth := m.pauseDepths[libraryID]
	if depth < int32(^uint32(0)>>1) {
		depth++
	}
	m.pauseDepths[libraryID] = depth
	for _, lw := range m.watchers {
		if lw.libraryID == libraryID {
			lw.pauseDepth.Store(depth)
		}
	}
	m.mu.Unlock()
	log.Debug().Int64("library_id", libraryID).Int32("depth", depth).Msg("watcher paused")
}

func (m *Manager) Resume(libraryID int64) {
	type reconcileRequest struct {
		ctx        context.Context
		generation uint64
	}
	var reconciles []reconcileRequest

	m.mu.Lock()
	m.initLifecycleMapsLocked()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	depth := m.pauseDepths[libraryID]
	if depth > 0 {
		depth--
	}
	if depth == 0 {
		delete(m.pauseDepths, libraryID)
	} else {
		m.pauseDepths[libraryID] = depth
	}
	for _, lw := range m.watchers {
		if lw.libraryID == libraryID {
			lw.pauseDepth.Store(depth)
			if depth == 0 && lw.dirty.Swap(false) {
				ctx := lw.ctx
				if ctx == nil {
					ctx = context.Background()
				}
				reconciles = append(reconciles, reconcileRequest{ctx: ctx, generation: lw.generation})
			}
		}
	}
	m.mu.Unlock()
	for _, reconcile := range reconciles {
		m.enqueueRescan(reconcile.ctx, libraryID, reconcile.generation)
	}
	log.Debug().Int64("library_id", libraryID).Int32("depth", depth).Msg("watcher resumed")
}

func (m *Manager) StopAll() {
	m.reconcileMu.Lock()
	defer m.reconcileMu.Unlock()
	m.mu.Lock()
	m.initLifecycleMapsLocked()
	m.stopped = true
	activities := m.allActivitiesLocked()
	var removed []*LibraryWatcher
	for key, lw := range m.watchers {
		m.generations[lw.libraryID]++
		delete(m.watchers, key)
		removed = append(removed, lw)
	}
	clear(m.pauseDepths)
	clear(m.desiredRoots)
	clear(m.generations)
	// Outstanding walk goroutines retain their own attempt pointers and safely
	// no-op when they eventually return. The manager is terminal, so no retry
	// suppression state is needed after this point.
	clear(m.watchWalks)
	m.mu.Unlock()

	for _, lw := range removed {
		lw.stopPendingEvents()
		lw.cancel()
		_ = lw.fsw.Close()
	}
	m.stopPendingEnqueues()
	for _, activity := range activities {
		activity.wg.Wait()
	}
	for _, lw := range removed {
		lw.stopPendingEvents()
	}
	m.forgetAllActivities(activities)
	log.Info().Msg("all watchers stopped")
}

func (m *Manager) initLifecycleMapsLocked() {
	if m.watchers == nil {
		m.watchers = make(map[string]*LibraryWatcher)
	}
	if m.watchWalks == nil {
		m.watchWalks = make(map[string]*watchWalkAttempt)
	}
	if m.desiredRoots == nil {
		m.desiredRoots = make(map[int64]map[string]struct{})
	}
	if m.pauseDepths == nil {
		m.pauseDepths = make(map[int64]int32)
	}
	if m.generations == nil {
		m.generations = make(map[int64]uint64)
	}
	if m.activities == nil {
		m.activities = make(map[int64]map[uint64]*generationActivity)
	}
}

func (m *Manager) activityLocked(libraryID int64, generation uint64) *generationActivity {
	byGeneration := m.activities[libraryID]
	if byGeneration == nil {
		byGeneration = make(map[uint64]*generationActivity)
		m.activities[libraryID] = byGeneration
	}
	activity := byGeneration[generation]
	if activity == nil {
		activity = &generationActivity{}
		byGeneration[generation] = activity
	}
	return activity
}

// beginGenerationActivity atomically admits a timer callback into the current
// generation. This closes the check/use race: either teardown invalidates
// first and the callback performs no work, or the callback is counted and
// teardown cannot return until it finishes.
func (m *Manager) beginGenerationActivity(libraryID int64, generation uint64) (*generationActivity, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initLifecycleMapsLocked()
	if !m.generationCurrentLocked(libraryID, generation) {
		return nil, false
	}
	activity := m.activityLocked(libraryID, generation)
	activity.wg.Add(1)
	return activity, true
}

func (m *Manager) activitiesForLibraryLocked(libraryID int64) []*generationActivity {
	byGeneration := m.activities[libraryID]
	activities := make([]*generationActivity, 0, len(byGeneration))
	for _, activity := range byGeneration {
		activities = append(activities, activity)
	}
	return activities
}

func (m *Manager) allActivitiesLocked() []*generationActivity {
	var activities []*generationActivity
	for libraryID := range m.activities {
		activities = append(activities, m.activitiesForLibraryLocked(libraryID)...)
	}
	return activities
}

func (m *Manager) forgetActivities(libraryID int64, completed []*generationActivity) {
	if len(completed) == 0 {
		return
	}
	completedSet := make(map[*generationActivity]struct{}, len(completed))
	for _, activity := range completed {
		completedSet[activity] = struct{}{}
	}

	m.mu.Lock()
	for generation, activity := range m.activities[libraryID] {
		if _, ok := completedSet[activity]; ok {
			delete(m.activities[libraryID], generation)
		}
	}
	if len(m.activities[libraryID]) == 0 {
		delete(m.activities, libraryID)
	}
	m.mu.Unlock()
}

func (m *Manager) forgetAllActivities(completed []*generationActivity) {
	if len(completed) == 0 {
		return
	}
	completedSet := make(map[*generationActivity]struct{}, len(completed))
	for _, activity := range completed {
		completedSet[activity] = struct{}{}
	}

	m.mu.Lock()
	for libraryID, byGeneration := range m.activities {
		for generation, activity := range byGeneration {
			if _, ok := completedSet[activity]; ok {
				delete(byGeneration, generation)
			}
		}
		if len(byGeneration) == 0 {
			delete(m.activities, libraryID)
		}
	}
	m.mu.Unlock()
}

func (m *Manager) generationCurrent(libraryID int64, generation uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generationCurrentLocked(libraryID, generation)
}

func (m *Manager) generationCurrentLocked(libraryID int64, generation uint64) bool {
	if m.stopped {
		return false
	}
	// Generation zero permits focused unit tests to exercise event batching
	// without constructing a real fsnotify watcher. Any Unwatch advances the
	// manager to a non-zero generation and invalidates such work.
	return m.generations[libraryID] == generation
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
	defer lw.stopPendingEvents()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-lw.fsw.Events:
			if !ok {
				return
			}
			if lw.pauseDepth.Load() > 0 {
				m.markWatcherDirty(ctx, lw)
				continue
			}
			m.handleEvent(ctx, lw, event)

		case err, ok := <-lw.fsw.Errors:
			if !ok {
				return
			}
			m.handleWatcherError(ctx, lw, err)
		}
	}
}

// handleWatcherError turns an fsnotify overflow into a durable reconciliation
// request. An overflow means the kernel has already dropped an unknown number
// of events, so continuing to consume the watcher without a full inventory can
// leave a library silently stale forever.
func (m *Manager) handleWatcherError(ctx context.Context, lw *LibraryWatcher, err error) {
	log.Error().Err(err).Int64("library_id", lw.libraryID).Msg("watcher error")
	if errors.Is(err, fsnotify.ErrEventOverflow) {
		m.markWatcherDirty(ctx, lw)
	}
}

// markWatcherDirty records that at least one event was discarded while the
// watcher was paused (or by the kernel during overflow). The manager lock
// closes the Resume race: either Resume observes dirty=true, or this method
// observes the library at depth zero and schedules the reconciliation itself.
func (m *Manager) markWatcherDirty(ctx context.Context, lw *LibraryWatcher) {
	if lw == nil {
		return
	}
	lw.dirty.Store(true)
	m.mu.Lock()
	m.initLifecycleMapsLocked()
	ready := !m.stopped &&
		m.generations[lw.libraryID] == lw.generation &&
		m.pauseDepths[lw.libraryID] == 0 &&
		lw.dirty.Swap(false)
	m.mu.Unlock()
	if ready {
		m.enqueueRescan(ctx, lw.libraryID, lw.generation)
	}
}

func (m *Manager) handleEvent(ctx context.Context, lw *LibraryWatcher, event fsnotify.Event) {
	path := event.Name
	if atomicfile.IsInternalPath(path) {
		return
	}

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
					log.Warn().Err(vfs.RedactError(err)).Str("path", vfs.RedactPath(path)).Msg("failed to watch new directory; scheduled library scans remain available")
				} else {
					log.Debug().Str("path", vfs.RedactPath(path)).Msg("watching new directory")
				}
				m.enqueueScannerRescan(ctx, lw.libraryID, path, lw.generation)
			}
			return
		}
	}

	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		ext := strings.ToLower(filepath.Ext(path))
		if isPrimaryMediaPath(path) {
			log.Info().Str("path", vfs.RedactPath(path)).Str("op", event.Op.String()).Msg("primary media removed; batching soft delete")
			m.enqueueSoftDelete(ctx, lw.libraryID, path, lw.generation)
		} else if isSidecarTriggerPath(path) {
			// Sidecars never have library_files rows, so soft-deleting them one by
			// one is both ineffective and catastrophically noisy during a bulk
			// cleanup. Coalesce their owner scopes behind one process coordinator.
			log.Info().Str("path", vfs.RedactPath(path)).Str("op", event.Op.String()).Int64("library_id", lw.libraryID).Msg("scanner sidecar removed; scheduling coalesced scoped rescan")
			m.enqueueSidecarRescan(ctx, lw.libraryID, path, lw.generation)
		} else if ext == "" {
			log.Info().Str("path", vfs.RedactPath(path)).Str("op", event.Op.String()).Int64("library_id", lw.libraryID).Msg("directory removed, scheduling rescan")
			m.enqueueRescan(ctx, lw.libraryID, lw.generation)
		}
		return
	}

	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
		if !isScannerTriggerPath(path) {
			return
		}

		dir := filepath.Dir(path)
		key := strconv.FormatInt(lw.libraryID, 10) + "\x00" + dir
		lw.pendingMu.Lock()
		if lw.pending == nil {
			lw.pending = make(map[string]*pendingWatcherEvent)
		}
		pending := lw.pending[key]
		if pending == nil {
			pending = &pendingWatcherEvent{paths: make(map[string]struct{})}
			lw.pending[key] = pending
		}
		pending.paths[path] = struct{}{}
		if pending.timer != nil {
			pending.timer.Stop()
		}
		var timer *time.Timer
		timer = time.AfterFunc(eventDebounceDelay, func() {
			activity, admitted := m.beginGenerationActivity(lw.libraryID, lw.generation)
			if !admitted {
				return
			}
			defer activity.wg.Done()

			lw.pendingMu.Lock()
			if lw.pending[key] != pending || pending.timer != timer {
				lw.pendingMu.Unlock()
				return
			}
			paths := make([]string, 0, len(pending.paths))
			for pendingPath := range pending.paths {
				paths = append(paths, pendingPath)
			}
			delete(lw.pending, key)
			lw.pendingMu.Unlock()

			sort.Strings(paths)
			for _, triggerPath := range paths {
				if m.shouldSuppressGeneratedEvent(triggerPath) {
					log.Debug().Str("path", vfs.RedactPath(triggerPath)).Int64("library_id", lw.libraryID).Msg("suppressed scanner-generated sidecar event")
					continue
				}
				m.enqueueScannerRescan(ctx, lw.libraryID, triggerPath, lw.generation)
				return
			}
		})
		pending.timer = timer
		lw.pendingMu.Unlock()
	}
}

func (lw *LibraryWatcher) stopPendingEvents() {
	lw.pendingMu.Lock()
	for key, pending := range lw.pending {
		if pending.timer != nil {
			pending.timer.Stop()
		}
		delete(lw.pending, key)
	}
	lw.pendingMu.Unlock()
}

func (m *Manager) enqueueSoftDelete(ctx context.Context, libraryID int64, path string, generation uint64) {
	if m.softDeleteInsert == nil {
		log.Warn().Str("path", vfs.RedactPath(path)).Msg("cannot enqueue soft delete: river client unavailable")
		return
	}

	m.mu.Lock()
	if !m.generationCurrentLocked(libraryID, generation) {
		m.mu.Unlock()
		return
	}
	m.softDeleteMu.Lock()
	if m.softDeletes == nil {
		m.softDeletes = make(map[int64]*pendingSoftDelete)
	}
	batch := m.softDeletes[libraryID]
	if batch == nil || batch.generation != generation {
		if batch != nil && batch.timer != nil {
			batch.timer.Stop()
		}
		batch = &pendingSoftDelete{paths: make(map[string]struct{}), generation: generation, ctx: ctx}
		m.softDeletes[libraryID] = batch
	}
	batch.paths[path] = struct{}{}
	batch.ctx = ctx
	if batch.timer != nil {
		batch.timer.Stop()
	}
	batch.version++
	version := batch.version
	batch.timer = time.AfterFunc(softDeleteDebounceDelay, func() { m.flushSoftDeletes(libraryID, batch, version) })
	m.softDeleteMu.Unlock()
	m.mu.Unlock()
}

func (m *Manager) flushSoftDeletes(libraryID int64, batch *pendingSoftDelete, version uint64) {
	activity, admitted := m.beginGenerationActivity(libraryID, batch.generation)
	if !admitted {
		return
	}
	defer activity.wg.Done()

	m.softDeleteMu.Lock()
	if m.softDeletes[libraryID] != batch || batch.version != version {
		m.softDeleteMu.Unlock()
		return
	}
	delete(m.softDeletes, libraryID)
	m.softDeleteMu.Unlock()
	if len(batch.paths) == 0 || batch.ctx.Err() != nil || !m.generationCurrent(libraryID, batch.generation) {
		return
	}

	paths := make([]string, 0, len(batch.paths))
	for path := range batch.paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	ctx, cancel := context.WithTimeout(batch.ctx, 30*time.Second)
	defer cancel()
	if err := m.softDeleteInsert(ctx, worker.SoftDeleteArgs{LibraryID: libraryID, Paths: paths}); err != nil {
		log.Warn().Err(err).Int64("library_id", libraryID).Int("count", len(paths)).Msg("enqueue batched soft delete failed")
		return
	}
	log.Info().Int64("library_id", libraryID).Int("count", len(paths)).Msg("batched soft delete enqueued")
}

func (m *Manager) enqueueSidecarRescan(ctx context.Context, libraryID int64, path string, generation uint64) {
	if m.sidecarRescanInsert == nil {
		log.Warn().Str("path", vfs.RedactPath(path)).Msg("cannot enqueue sidecar rescan: river client unavailable")
		return
	}
	m.mu.Lock()
	if !m.generationCurrentLocked(libraryID, generation) {
		m.mu.Unlock()
		return
	}
	m.sidecarRescanMu.Lock()
	if m.sidecarRescans == nil {
		m.sidecarRescans = make(map[int64]*pendingSoftDelete)
	}
	batch := m.sidecarRescans[libraryID]
	if batch == nil || batch.generation != generation {
		if batch != nil && batch.timer != nil {
			batch.timer.Stop()
		}
		batch = &pendingSoftDelete{paths: make(map[string]struct{}), generation: generation, ctx: ctx}
		m.sidecarRescans[libraryID] = batch
	}
	batch.paths[path] = struct{}{}
	batch.ctx = ctx
	if batch.timer != nil {
		batch.timer.Stop()
	}
	batch.version++
	version := batch.version
	batch.timer = time.AfterFunc(sidecarRescanDebounceDelay, func() { m.flushSidecarRescans(libraryID, batch, version) })
	m.sidecarRescanMu.Unlock()
	m.mu.Unlock()
}

func (m *Manager) flushSidecarRescans(libraryID int64, batch *pendingSoftDelete, version uint64) {
	activity, admitted := m.beginGenerationActivity(libraryID, batch.generation)
	if !admitted {
		return
	}
	defer activity.wg.Done()

	m.sidecarRescanMu.Lock()
	if m.sidecarRescans[libraryID] != batch || batch.version != version {
		m.sidecarRescanMu.Unlock()
		return
	}
	delete(m.sidecarRescans, libraryID)
	m.sidecarRescanMu.Unlock()
	if len(batch.paths) == 0 || batch.ctx.Err() != nil || !m.generationCurrent(libraryID, batch.generation) {
		return
	}
	paths := make([]string, 0, len(batch.paths))
	for path := range batch.paths {
		if m.shouldSuppressGeneratedEvent(path) {
			log.Debug().Str("path", vfs.RedactPath(path)).Int64("library_id", libraryID).Msg("suppressed scanner-generated sidecar rename event")
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(batch.ctx, 30*time.Second)
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

func (m *Manager) enqueueScannerRescan(ctx context.Context, libraryID int64, triggerPath string, generation uint64) {
	if ctx.Err() != nil || !m.generationCurrent(libraryID, generation) {
		return
	}
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
		log.Warn().Err(vfs.RedactError(err)).Int64("library_id", libraryID).Str("path", vfs.RedactPath(triggerPath)).Msg("enqueue scanner run failed: library lookup failed")
		return
	}
	args := worker.ProcessLibraryScanArgs{
		LibraryID:  libraryID,
		MediaType:  lib.MediaType,
		ScopePaths: []string{worker.ScannerScopeForLibraryPath(lib, triggerPath)},
	}
	if err := worker.EnqueueProcessLibraryScan(ctx, m.river, m.db, args, worker.PriorityWatcher, ""); err != nil {
		log.Warn().Err(vfs.RedactError(err)).Int64("library_id", libraryID).Str("path", vfs.RedactPath(triggerPath)).Msg("enqueue scanner run failed")
		return
	}
	log.Info().Int64("library_id", libraryID).Str("path", vfs.RedactPath(triggerPath)).Msg("watcher-triggered scanner run enqueued")
}

func (m *Manager) enqueueRescan(ctx context.Context, libraryID int64, generation uint64) {
	m.mu.Lock()
	if !m.generationCurrentLocked(libraryID, generation) {
		m.mu.Unlock()
		return
	}
	m.rescanMu.Lock()
	if m.rescanTimers == nil {
		m.rescanTimers = make(map[int64]*pendingRescan)
	}

	if pending := m.rescanTimers[libraryID]; pending != nil {
		pending.timer.Stop()
	}

	pending := &pendingRescan{generation: generation, ctx: ctx}
	pending.timer = time.AfterFunc(rescanDebounceDelay, func() {
		activity, admitted := m.beginGenerationActivity(libraryID, pending.generation)
		if !admitted {
			return
		}
		defer activity.wg.Done()

		m.rescanMu.Lock()
		if m.rescanTimers[libraryID] != pending {
			m.rescanMu.Unlock()
			return
		}
		delete(m.rescanTimers, libraryID)
		m.rescanMu.Unlock()

		if pending.ctx.Err() != nil || !m.generationCurrent(libraryID, pending.generation) {
			return
		}
		if m.onScan != nil {
			m.onScan(libraryID, false)
		}
		log.Info().Int64("library_id", libraryID).Msg("coalesced library rescan enqueued after filesystem change")
	})
	m.rescanTimers[libraryID] = pending
	m.rescanMu.Unlock()
	m.mu.Unlock()
}

func (m *Manager) cancelPendingEnqueues(libraryID int64, throughGeneration uint64) {
	m.softDeleteMu.Lock()
	if batch := m.softDeletes[libraryID]; batch != nil && batch.generation <= throughGeneration {
		if batch.timer != nil {
			batch.timer.Stop()
		}
		delete(m.softDeletes, libraryID)
	}
	m.softDeleteMu.Unlock()

	m.sidecarRescanMu.Lock()
	if batch := m.sidecarRescans[libraryID]; batch != nil && batch.generation <= throughGeneration {
		if batch.timer != nil {
			batch.timer.Stop()
		}
		delete(m.sidecarRescans, libraryID)
	}
	m.sidecarRescanMu.Unlock()

	m.rescanMu.Lock()
	if pending := m.rescanTimers[libraryID]; pending != nil && pending.generation <= throughGeneration {
		pending.timer.Stop()
		delete(m.rescanTimers, libraryID)
	}
	m.rescanMu.Unlock()
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
	for libraryID, pending := range m.rescanTimers {
		pending.timer.Stop()
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
	return addRecursiveBoundedWithExit(ctx, fsw, root, nil)
}

// addRecursiveBoundedWithExit is addRecursiveBounded with a completion hook
// executed by the original walk goroutine. The hook is deliberately not run by
// a separate waiter: if the filesystem never recovers, tracking that one stuck
// walk must not itself cost another permanently blocked goroutine.
func addRecursiveBoundedWithExit(ctx context.Context, fsw *fsnotify.Watcher, root string, onExit func()) error {
	var visited atomic.Int64
	done := make(chan error, 1)
	walk := recursiveWalkFn // capture before spawning: the goroutine may outlive a test's stub swap
	go func() {
		err := walk(fsw, root, &visited)
		if onExit != nil {
			onExit()
		}
		done <- err
	}()

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
