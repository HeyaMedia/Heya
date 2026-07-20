package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/worker"
)

// TestAddRecursiveBounded verifies the happy path: a healthy tree arms fully
// (non-hidden dirs watched, hidden dirs skipped) and the bounded wrapper
// returns without tripping the stall watchdog.
func TestAddRecursiveBounded(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a", filepath.Join("a", "b"), ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer fsw.Close()

	if err := addRecursiveBounded(context.Background(), fsw, root); err != nil {
		t.Fatalf("bounded walk of a healthy tree must succeed: %v", err)
	}

	watched := map[string]bool{}
	for _, p := range fsw.WatchList() {
		watched[p] = true
	}
	for _, want := range []string{root, filepath.Join(root, "a"), filepath.Join(root, "a", "b")} {
		if !watched[want] {
			t.Errorf("expected %q watched; watch list = %v", want, fsw.WatchList())
		}
	}
	if watched[filepath.Join(root, ".hidden")] {
		t.Error("hidden directory must be skipped by the recursive walk")
	}
}

// TestAddRecursiveBoundedStallTrips verifies the watchdog: a walk that makes
// zero progress for a full window (the stalled-mount signature) is abandoned
// with an error.
func TestAddRecursiveBoundedStallTrips(t *testing.T) {
	origTimeout := watchWalkStallTimeout
	watchWalkStallTimeout = 20 * time.Millisecond
	origWalk := recursiveWalkFn
	block := make(chan struct{})
	recursiveWalkFn = func(_ *fsnotify.Watcher, _ string, _ *atomic.Int64) error {
		<-block // wedged Getdents: no progress, ever
		return nil
	}
	t.Cleanup(func() {
		watchWalkStallTimeout = origTimeout
		recursiveWalkFn = origWalk
		close(block)
	})

	err := addRecursiveBounded(context.Background(), nil, "/stalled")
	if err == nil {
		t.Fatal("a walk with zero progress must trip the stall watchdog")
	}
	if !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("stall error should say so, got: %v", err)
	}
}

// TestAddRecursiveBoundedSlowWalkSurvives verifies the point of the watchdog
// being progress-based: a walk that takes many times the stall window but
// keeps visiting entries (huge tree, busy disk) must NOT be aborted.
func TestAddRecursiveBoundedSlowWalkSurvives(t *testing.T) {
	origTimeout := watchWalkStallTimeout
	watchWalkStallTimeout = 20 * time.Millisecond
	origWalk := recursiveWalkFn
	recursiveWalkFn = func(_ *fsnotify.Watcher, _ string, visited *atomic.Int64) error {
		for range 40 { // ~10x the stall window, progressing throughout
			visited.Add(1)
			time.Sleep(5 * time.Millisecond)
		}
		return nil
	}
	t.Cleanup(func() {
		watchWalkStallTimeout = origTimeout
		recursiveWalkFn = origWalk
	})

	if err := addRecursiveBounded(context.Background(), nil, "/slow-but-alive"); err != nil {
		t.Fatalf("a slow-but-progressing walk must arm, got: %v", err)
	}
}

// TestWatchDoesNotResurrectUnwatched guards the arm/unwatch race: a library
// unwatched while its (slow) recursive walk is still in flight must NOT be
// re-added to the watcher set when the walk finishes.
func TestWatchDoesNotResurrectUnwatched(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	orig := recursiveWalkFn
	recursiveWalkFn = func(_ *fsnotify.Watcher, _ string, _ *atomic.Int64) error {
		close(started) // slot is already reserved by the time the walk runs
		<-release      // block mid-arm until the test lets go
		return nil
	}
	t.Cleanup(func() { recursiveWalkFn = orig })

	m := &Manager{watchers: make(map[string]*LibraryWatcher)}
	done := make(chan struct{})
	go func() {
		m.Watch(context.Background(), 7, t.TempDir())
		close(done)
	}()

	<-started      // Watch has reserved the slot and is inside the walk
	m.Unwatch(7)   // remove the library mid-arm
	close(release) // let the walk complete
	<-done         // Watch returns

	m.mu.Lock()
	n := len(m.watchers)
	m.mu.Unlock()
	if n != 0 {
		t.Fatalf("Watch resurrected a library unwatched during arming: %d watcher(s) remain", n)
	}
}

func TestSyncLibraryDisabledUnwatches(t *testing.T) {
	m := &Manager{watchers: make(map[string]*LibraryWatcher)}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	_, cancel := context.WithCancel(context.Background())
	m.watchers[watcherKey(9, "/library")] = &LibraryWatcher{
		libraryID: 9,
		rootPath:  "/library",
		fsw:       fsw,
		cancel:    cancel,
	}

	settings, err := json.Marshal(metadata.LibrarySettings{Watch: false})
	if err != nil {
		t.Fatal(err)
	}
	m.SyncLibrary(context.Background(), sqlc.Library{
		ID:       9,
		Name:     "Disabled",
		Paths:    []string{"/library"},
		Settings: settings,
	})

	m.mu.Lock()
	n := len(m.watchers)
	m.mu.Unlock()
	if n != 0 {
		t.Fatalf("disabled library should remove watchers, got %d", n)
	}
}

func TestReconcileLibrariesIsIdempotentAndRemovesDeleted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := NewManager(nil, nil, nil)
	t.Cleanup(m.StopAll)
	root := t.TempDir()
	settings, err := json.Marshal(metadata.LibrarySettings{Watch: true})
	if err != nil {
		t.Fatal(err)
	}
	lib := sqlc.Library{ID: 71, Name: "Durable", Paths: []string{root}, Settings: settings}

	m.reconcileMu.Lock()
	m.reconcileLibraries(ctx, []sqlc.Library{lib})
	m.reconcileMu.Unlock()
	watcher := waitForLibraryWatcher(t, m, lib.ID, root)

	// An unchanged durable snapshot must not tear down and re-arm the tree.
	m.reconcileMu.Lock()
	m.reconcileLibraries(ctx, []sqlc.Library{lib})
	m.reconcileMu.Unlock()
	if current := waitForLibraryWatcher(t, m, lib.ID, root); current != watcher {
		t.Fatal("unchanged reconciliation replaced the live watcher")
	}

	// A missed delete notification is repaired by the next durable snapshot.
	m.reconcileMu.Lock()
	m.reconcileLibraries(ctx, nil)
	m.reconcileMu.Unlock()
	m.mu.Lock()
	remaining := len(m.watchers)
	m.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("deleted library retained %d watcher(s)", remaining)
	}
}

func TestReconcileSuppressesRetryUntilAbandonedWalkExits(t *testing.T) {
	origTimeout := watchWalkStallTimeout
	watchWalkStallTimeout = 20 * time.Millisecond
	origWalk := recursiveWalkFn

	var calls atomic.Int32
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstReleased := false
	recursiveWalkFn = func(_ *fsnotify.Watcher, _ string, _ *atomic.Int64) error {
		switch calls.Add(1) {
		case 1:
			close(firstStarted)
			<-releaseFirst
		case 2:
			close(secondStarted)
		}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := NewManager(nil, nil, nil)
	t.Cleanup(func() {
		cancel()
		if !firstReleased {
			close(releaseFirst)
		}
		m.StopAll()
		recursiveWalkFn = origWalk
		watchWalkStallTimeout = origTimeout
	})

	root := t.TempDir()
	settings, err := json.Marshal(metadata.LibrarySettings{Watch: true})
	if err != nil {
		t.Fatal(err)
	}
	lib := sqlc.Library{ID: 72, Name: "Stalled", Paths: []string{root}, Settings: settings}

	m.reconcileMu.Lock()
	m.reconcileLibraries(ctx, []sqlc.Library{lib})
	m.reconcileMu.Unlock()
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("initial recursive walk did not start")
	}

	key := watcherKey(lib.ID, root)
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		_, watcherPresent := m.watchers[key]
		walkPresent := m.watchWalks[key] != nil
		m.mu.Unlock()
		if !watcherPresent && walkPresent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bounded arm did not retain its abandoned walk after timing out")
		}
		time.Sleep(time.Millisecond)
	}

	// Durable reconciliation can run any number of times while the original
	// syscall is stuck, but it must not accumulate another blocked goroutine for
	// the same root.
	for range 5 {
		m.reconcileMu.Lock()
		m.reconcileLibraries(ctx, []sqlc.Library{lib})
		m.reconcileMu.Unlock()
	}
	time.Sleep(2 * watchWalkStallTimeout)
	if got := calls.Load(); got != 1 {
		t.Fatalf("reconciliation started %d recursive walks while the first remained stuck, want 1", got)
	}

	firstReleased = true
	close(releaseFirst)
	deadline = time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		walkPresent := m.watchWalks[key] != nil
		m.mu.Unlock()
		if !walkPresent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completed abandoned walk remained registered")
		}
		time.Sleep(time.Millisecond)
	}

	// Once the exact abandoned goroutine has exited, the next reconciliation is
	// allowed to retry and can arm a healthy replacement.
	m.reconcileMu.Lock()
	m.reconcileLibraries(ctx, []sqlc.Library{lib})
	m.reconcileMu.Unlock()
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("replacement recursive walk did not start")
	}
	_ = waitForLibraryWatcher(t, m, lib.ID, root)
	if got := calls.Load(); got != 2 {
		t.Fatalf("reconciliation made %d recursive walk attempts after recovery, want 2", got)
	}
}

func TestPauseDepthSurvivesWatcherReconciliation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	m := NewManager(nil, nil, nil)
	t.Cleanup(m.StopAll)
	const libraryID = int64(41)
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	thirdRoot := t.TempDir()

	// Pauses are library state, not properties of whichever fsnotify objects
	// happen to exist at the moment.
	m.Pause(libraryID)
	m.Pause(libraryID)
	m.Watch(ctx, libraryID, firstRoot)
	if got := watcherPauseDepth(t, m, libraryID, firstRoot); got != 2 {
		t.Fatalf("watch armed after two pauses has depth %d, want 2", got)
	}

	m.Resume(libraryID)
	if got := watcherPauseDepth(t, m, libraryID, firstRoot); got != 1 {
		t.Fatalf("first resume produced depth %d, want 1", got)
	}

	settings, err := json.Marshal(metadata.LibrarySettings{Watch: true})
	if err != nil {
		t.Fatal(err)
	}
	m.SyncLibrary(ctx, sqlc.Library{
		ID:       libraryID,
		Name:     "Reconciled",
		Paths:    []string{secondRoot, thirdRoot},
		Settings: settings,
	})

	if got := watcherPauseDepth(t, m, libraryID, secondRoot); got != 1 {
		t.Fatalf("first reconciled watcher has depth %d, want 1", got)
	}
	if got := watcherPauseDepth(t, m, libraryID, thirdRoot); got != 1 {
		t.Fatalf("second reconciled watcher has depth %d, want 1", got)
	}
	m.mu.Lock()
	_, oldStillPresent := m.watchers[watcherKey(libraryID, firstRoot)]
	m.mu.Unlock()
	if oldStillPresent {
		t.Fatal("reconciliation left the old watcher armed")
	}

	m.Resume(libraryID)
	m.Resume(libraryID) // an unmatched resume must remain at the zero floor
	for _, root := range []string{secondRoot, thirdRoot} {
		if got := watcherPauseDepth(t, m, libraryID, root); got != 0 {
			t.Fatalf("watcher %q has depth %d after final/extra resume, want 0", root, got)
		}
	}
}

func TestPausedWatcherChangeReconcilesAfterFinalResume(t *testing.T) {
	restore := useShortDebounces()
	t.Cleanup(restore)

	rescans := make(chan int64, 2)
	m := NewManager(nil, nil, func(libraryID int64, _ bool) { rescans <- libraryID })
	t.Cleanup(m.StopAll)
	lw, ctx := registerTestWatcher(t, m, 42, 1)

	m.Pause(lw.libraryID)
	m.Pause(lw.libraryID)
	m.markWatcherDirty(ctx, lw)

	m.Resume(lw.libraryID)
	select {
	case libraryID := <-rescans:
		t.Fatalf("intermediate resume reconciled library %d", libraryID)
	case <-time.After(2 * rescanDebounceDelay):
	}

	m.Resume(lw.libraryID)
	select {
	case libraryID := <-rescans:
		if libraryID != lw.libraryID {
			t.Fatalf("reconciled library %d, want %d", libraryID, lw.libraryID)
		}
	case <-time.After(time.Second):
		t.Fatal("paused watcher change did not reconcile after the final resume")
	}
}

func TestWatcherOverflowSchedulesLibraryReconciliation(t *testing.T) {
	restore := useShortDebounces()
	t.Cleanup(restore)

	rescans := make(chan int64, 2)
	m := NewManager(nil, nil, func(libraryID int64, _ bool) { rescans <- libraryID })
	t.Cleanup(m.StopAll)
	lw, ctx := registerTestWatcher(t, m, 43, 1)

	m.handleWatcherError(ctx, lw, fmt.Errorf("wrapped watcher failure: %w", fsnotify.ErrEventOverflow))
	select {
	case libraryID := <-rescans:
		if libraryID != lw.libraryID {
			t.Fatalf("reconciled library %d, want %d", libraryID, lw.libraryID)
		}
	case <-time.After(time.Second):
		t.Fatal("watcher overflow did not schedule a full library reconciliation")
	}
}

func TestNewestSyncGenerationWins(t *testing.T) {
	oldRoot := t.TempDir()
	newRoot := t.TempDir()
	oldWalkStarted := make(chan struct{})
	releaseOldWalk := make(chan struct{})
	oldWalkReturned := make(chan struct{})

	origWalk := recursiveWalkFn
	recursiveWalkFn = func(_ *fsnotify.Watcher, root string, _ *atomic.Int64) error {
		if root == oldRoot {
			close(oldWalkStarted)
			<-releaseOldWalk
			close(oldWalkReturned)
		}
		return nil
	}
	t.Cleanup(func() { recursiveWalkFn = origWalk })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := NewManager(nil, nil, nil)
	t.Cleanup(m.StopAll)
	settings, err := json.Marshal(metadata.LibrarySettings{Watch: true})
	if err != nil {
		t.Fatal(err)
	}

	m.SyncLibrary(ctx, sqlc.Library{ID: 52, Name: "Old", Paths: []string{oldRoot}, Settings: settings})
	select {
	case <-oldWalkStarted:
	case <-time.After(time.Second):
		t.Fatal("old reconciliation never began arming")
	}

	// A newer reconciliation invalidates the old generation even while the old
	// filesystem walk is still in flight.
	m.SyncLibrary(ctx, sqlc.Library{ID: 52, Name: "New", Paths: []string{newRoot}, Settings: settings})
	if got := watcherPauseDepth(t, m, 52, newRoot); got != 0 {
		t.Fatalf("new watcher unexpectedly paused at depth %d", got)
	}
	close(releaseOldWalk)
	select {
	case <-oldWalkReturned:
	case <-time.After(time.Second):
		t.Fatal("old test walk did not return")
	}

	deadline := time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		_, oldPresent := m.watchers[watcherKey(52, oldRoot)]
		_, newPresent := m.watchers[watcherKey(52, newRoot)]
		m.mu.Unlock()
		if !oldPresent && newPresent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stale reconciliation survived: old=%v new=%v", oldPresent, newPresent)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestUnwatchCancelsPendingDebounces(t *testing.T) {
	restore := useShortDebounces()
	t.Cleanup(restore)

	var calls atomic.Int32
	m := NewManager(nil, nil, func(_ int64, _ bool) { calls.Add(1) })
	m.softDeleteInsert = func(_ context.Context, _ worker.SoftDeleteArgs) error {
		calls.Add(1)
		return nil
	}
	m.sidecarRescanInsert = func(_ context.Context, _ int64, _ []string) error {
		calls.Add(1)
		return nil
	}
	lw, wctx := registerTestWatcher(t, m, 61, 1)
	scheduleEveryDebounce(m, wctx, lw)
	assertPendingDebounces(t, m, lw, true)

	m.Unwatch(61)
	assertPendingDebounces(t, m, lw, false)
	time.Sleep(100 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Fatalf("%d debounce callback(s) ran after Unwatch", got)
	}
}

func TestStopAllCancelsPendingDebouncesAndIsTerminal(t *testing.T) {
	restore := useShortDebounces()
	t.Cleanup(restore)

	var calls atomic.Int32
	m := NewManager(nil, nil, func(_ int64, _ bool) { calls.Add(1) })
	m.softDeleteInsert = func(_ context.Context, _ worker.SoftDeleteArgs) error {
		calls.Add(1)
		return nil
	}
	m.sidecarRescanInsert = func(_ context.Context, _ int64, _ []string) error {
		calls.Add(1)
		return nil
	}
	lw, wctx := registerTestWatcher(t, m, 62, 1)
	m.Pause(62)
	scheduleEveryDebounce(m, wctx, lw)
	assertPendingDebounces(t, m, lw, true)

	m.StopAll()
	assertPendingDebounces(t, m, lw, false)
	m.Watch(context.Background(), 62, t.TempDir())
	m.Pause(62)
	if got := len(m.Status()); got != 0 {
		t.Fatalf("StopAll allowed %d watcher(s) to be armed afterward", got)
	}
	m.mu.Lock()
	pauseStates := len(m.pauseDepths)
	m.mu.Unlock()
	if pauseStates != 0 {
		t.Fatalf("StopAll retained %d pause state(s)", pauseStates)
	}

	time.Sleep(100 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Fatalf("%d debounce callback(s) ran after StopAll", got)
	}
}

func TestTeardownWaitsForAdmittedDebounceCallbacks(t *testing.T) {
	tests := []struct {
		name      string
		stopAll   bool
		database  bool
		libraryID int64
	}{
		{name: "unwatch scanner callback", libraryID: 71},
		{name: "unwatch database enqueue", database: true, libraryID: 72},
		{name: "stop all scanner callback", stopAll: true, libraryID: 73},
		{name: "stop all database enqueue", stopAll: true, database: true, libraryID: 74},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restore := useShortDebounces()
			t.Cleanup(restore)

			entered := make(chan struct{})
			release := make(chan struct{})
			released := false
			defer func() {
				if !released {
					close(release)
				}
			}()
			var teardownReturned atomic.Bool
			var sideEffects atomic.Int32
			var postReturnEffects atomic.Int32
			effect := func() {
				close(entered)
				<-release
				if teardownReturned.Load() {
					postReturnEffects.Add(1)
				}
				sideEffects.Add(1)
			}

			var onScan ScanFunc
			if !tt.database {
				onScan = func(_ int64, _ bool) { effect() }
			}
			m := NewManager(nil, nil, onScan)
			if tt.database {
				m.softDeleteInsert = func(_ context.Context, _ worker.SoftDeleteArgs) error {
					effect()
					return nil
				}
			}
			lw, wctx := registerTestWatcher(t, m, tt.libraryID, 1)
			if tt.database {
				m.handleEvent(wctx, lw, fsnotify.Event{Name: "/music/album/track.flac", Op: fsnotify.Remove})
			} else {
				m.handleEvent(wctx, lw, fsnotify.Event{Name: "/music/album/track.flac", Op: fsnotify.Write})
			}

			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("debounce callback did not reach its admitted side-effect boundary")
			}

			teardownDone := make(chan struct{})
			go func() {
				if tt.stopAll {
					m.StopAll()
				} else {
					m.Unwatch(tt.libraryID)
				}
				teardownReturned.Store(true)
				close(teardownDone)
			}()

			waitForInvalidation(t, m, tt.libraryID, 1, tt.stopAll)
			returnedEarly := false
			select {
			case <-teardownDone:
				returnedEarly = true
			case <-time.After(50 * time.Millisecond):
			}

			// Let the admitted operation finish. Correct teardown now joins it and
			// returns; a broken teardown has already returned and the operation is
			// recorded as post-return work.
			released = true
			close(release)
			select {
			case <-teardownDone:
			case <-time.After(time.Second):
				t.Fatal("teardown did not return after the admitted callback finished")
			}
			if returnedEarly {
				t.Error("teardown returned while an admitted timer callback was still running")
			}
			if got := sideEffects.Load(); got != 1 {
				t.Errorf("side effects = %d, want exactly one completed before teardown returned", got)
			}
			if got := postReturnEffects.Load(); got != 0 {
				t.Errorf("post-return side effects = %d, want zero", got)
			}
		})
	}
}

func TestUnwatchJoinsEventLoop(t *testing.T) {
	const libraryID = int64(75)
	root := t.TempDir()
	entered := make(chan struct{})
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	var teardownReturned atomic.Bool
	var postReturnEffects atomic.Int32

	m := NewManager(nil, nil, func(_ int64, _ bool) {
		close(entered)
		<-release
		if teardownReturned.Load() {
			postReturnEffects.Add(1)
		}
	})
	m.Watch(context.Background(), libraryID, root)

	// A newly-created directory takes the synchronous event-loop path (there is
	// no debounce timer), so blocking onScan here holds the loop itself open.
	if err := os.Mkdir(filepath.Join(root, "new-directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		released = true
		close(release)
		m.StopAll()
		t.Fatal("filesystem event did not reach the event-loop callback")
	}

	teardownDone := make(chan struct{})
	go func() {
		m.Unwatch(libraryID)
		teardownReturned.Store(true)
		close(teardownDone)
	}()
	waitForInvalidation(t, m, libraryID, 1, false)
	returnedEarly := false
	select {
	case <-teardownDone:
		returnedEarly = true
	case <-time.After(50 * time.Millisecond):
	}
	released = true
	close(release)
	select {
	case <-teardownDone:
	case <-time.After(time.Second):
		t.Fatal("Unwatch did not return after the event loop was released")
	}
	if returnedEarly {
		t.Error("Unwatch returned while its event loop was still running")
	}
	if got := postReturnEffects.Load(); got != 0 {
		t.Errorf("event-loop side effects after Unwatch returned = %d, want zero", got)
	}
}

func waitForInvalidation(t *testing.T, m *Manager, libraryID int64, generation uint64, stopAll bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		invalidated := m.generations[libraryID] != generation
		if stopAll {
			invalidated = m.stopped
		}
		m.mu.Unlock()
		if invalidated {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("teardown did not invalidate the watcher generation")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestEventLoopChannelClosureDrainsPendingDebounces(t *testing.T) {
	restore := useShortDebounces()
	t.Cleanup(restore)

	var calls atomic.Int32
	m := NewManager(nil, nil, func(_ int64, _ bool) { calls.Add(1) })
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	lw := &LibraryWatcher{libraryID: 63, fsw: fsw, cancel: cancel}
	m.handleEvent(ctx, lw, fsnotify.Event{Name: "/music/album/new.flac", Op: fsnotify.Write})

	done := make(chan struct{})
	go func() {
		m.eventLoop(ctx, lw)
		close(done)
	}()
	if err := fsw.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event loop did not return after fsnotify channels closed")
	}
	lw.pendingMu.Lock()
	pending := len(lw.pending)
	lw.pendingMu.Unlock()
	if pending != 0 {
		t.Fatalf("event loop left %d local debounce timer(s) after channel closure", pending)
	}
	time.Sleep(100 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Fatalf("%d local debounce callback(s) ran after event-loop shutdown", got)
	}
}

func watcherPauseDepth(t *testing.T, m *Manager, libraryID int64, root string) int32 {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		lw := m.watchers[watcherKey(libraryID, root)]
		m.mu.Unlock()
		if lw != nil {
			return lw.pauseDepth.Load()
		}
		if time.Now().After(deadline) {
			t.Fatalf("watcher for library %d at %q did not arm", libraryID, root)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForLibraryWatcher(t *testing.T, m *Manager, libraryID int64, root string) *LibraryWatcher {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		watcher := m.watchers[watcherKey(libraryID, root)]
		m.mu.Unlock()
		if watcher != nil {
			return watcher
		}
		if time.Now().After(deadline) {
			t.Fatalf("watcher for library %d root %q did not arm", libraryID, root)
		}
		time.Sleep(time.Millisecond)
	}
}

func useShortDebounces() func() {
	origEvent := eventDebounceDelay
	origRescan := rescanDebounceDelay
	origSoftDelete := softDeleteDebounceDelay
	origSidecar := sidecarRescanDebounceDelay
	eventDebounceDelay = 20 * time.Millisecond
	rescanDebounceDelay = 20 * time.Millisecond
	softDeleteDebounceDelay = 20 * time.Millisecond
	sidecarRescanDebounceDelay = 20 * time.Millisecond
	return func() {
		eventDebounceDelay = origEvent
		rescanDebounceDelay = origRescan
		softDeleteDebounceDelay = origSoftDelete
		sidecarRescanDebounceDelay = origSidecar
	}
}

func registerTestWatcher(t *testing.T, m *Manager, libraryID int64, generation uint64) (*LibraryWatcher, context.Context) {
	t.Helper()
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	wctx, cancel := context.WithCancel(context.Background())
	root := t.TempDir()
	lw := &LibraryWatcher{
		libraryID:  libraryID,
		rootPath:   root,
		fsw:        fsw,
		ctx:        wctx,
		cancel:     cancel,
		generation: generation,
	}
	m.mu.Lock()
	m.initLifecycleMapsLocked()
	m.generations[libraryID] = generation
	m.watchers[watcherKey(libraryID, root)] = lw
	m.mu.Unlock()
	return lw, wctx
}

func scheduleEveryDebounce(m *Manager, ctx context.Context, lw *LibraryWatcher) {
	m.handleEvent(ctx, lw, fsnotify.Event{Name: "/music/album/track.flac", Op: fsnotify.Remove})
	m.handleEvent(ctx, lw, fsnotify.Event{Name: "/music/album/album.nfo", Op: fsnotify.Remove})
	m.handleEvent(ctx, lw, fsnotify.Event{Name: "/music/deleted-directory", Op: fsnotify.Remove})
	m.handleEvent(ctx, lw, fsnotify.Event{Name: "/music/album/new.flac", Op: fsnotify.Write})
}

func assertPendingDebounces(t *testing.T, m *Manager, lw *LibraryWatcher, want bool) {
	t.Helper()
	lw.pendingMu.Lock()
	eventPending := len(lw.pending) > 0
	lw.pendingMu.Unlock()
	m.softDeleteMu.Lock()
	softDeletePending := m.softDeletes[lw.libraryID] != nil
	m.softDeleteMu.Unlock()
	m.sidecarRescanMu.Lock()
	sidecarPending := m.sidecarRescans[lw.libraryID] != nil
	m.sidecarRescanMu.Unlock()
	m.rescanMu.Lock()
	rescanPending := m.rescanTimers[lw.libraryID] != nil
	m.rescanMu.Unlock()

	if eventPending != want || softDeletePending != want || sidecarPending != want || rescanPending != want {
		t.Fatalf("pending state event=%v soft-delete=%v sidecar=%v rescan=%v, want all %v",
			eventPending, softDeletePending, sidecarPending, rescanPending, want)
	}
}

func TestSidecarRemovalBurstCoalescesIntoOneScopedCoordinator(t *testing.T) {
	origDelay := sidecarRescanDebounceDelay
	sidecarRescanDebounceDelay = 10 * time.Millisecond
	t.Cleanup(func() { sidecarRescanDebounceDelay = origDelay })

	var softDeletes atomic.Int32
	jobs := make(chan []string, 2)
	m := &Manager{
		watchers: make(map[string]*LibraryWatcher),
		sidecarRescanInsert: func(_ context.Context, libraryID int64, paths []string) error {
			if libraryID != 4 {
				t.Errorf("unexpected library: %d", libraryID)
			}
			jobs <- paths
			return nil
		},
		softDeleteInsert: func(_ context.Context, _ worker.SoftDeleteArgs) error {
			softDeletes.Add(1)
			return nil
		},
	}
	t.Cleanup(m.stopPendingEnqueues)
	lw := &LibraryWatcher{libraryID: 4}

	for i := range 100 {
		path := filepath.Join("/music", fmt.Sprintf("Artist %d", i), "artist.nfo")
		m.handleEvent(context.Background(), lw, fsnotify.Event{Name: path, Op: fsnotify.Remove})
	}

	select {
	case paths := <-jobs:
		if len(paths) != 100 {
			t.Fatalf("coalesced sidecar job has %d paths, want 100", len(paths))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for coalesced sidecar rescan")
	}
	select {
	case extra := <-jobs:
		t.Fatalf("sidecar burst produced an extra coordinator with %d paths", len(extra))
	case <-time.After(30 * time.Millisecond):
	}
	if got := softDeletes.Load(); got != 0 {
		t.Fatalf("sidecars produced %d soft-delete jobs, want 0", got)
	}
}

func TestPrimaryMediaRemovalBurstCreatesOneBatchedSoftDelete(t *testing.T) {
	origDelay := softDeleteDebounceDelay
	softDeleteDebounceDelay = 10 * time.Millisecond
	t.Cleanup(func() { softDeleteDebounceDelay = origDelay })

	jobs := make(chan worker.SoftDeleteArgs, 2)
	m := &Manager{
		watchers: make(map[string]*LibraryWatcher),
		softDeleteInsert: func(_ context.Context, args worker.SoftDeleteArgs) error {
			jobs <- args
			return nil
		},
	}
	t.Cleanup(m.stopPendingEnqueues)
	lw := &LibraryWatcher{libraryID: 4}

	for i := range 100 {
		path := filepath.Join("/music/Artist/Album", fmt.Sprintf("%03d.flac", i))
		m.handleEvent(context.Background(), lw, fsnotify.Event{Name: path, Op: fsnotify.Remove})
	}

	select {
	case job := <-jobs:
		if job.LibraryID != 4 || len(job.Paths) != 100 {
			t.Fatalf("unexpected batched job: library=%d paths=%d", job.LibraryID, len(job.Paths))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for batched soft-delete job")
	}
	select {
	case extra := <-jobs:
		t.Fatalf("removal burst produced an extra job with %d paths", len(extra.Paths))
	case <-time.After(30 * time.Millisecond):
	}
}

func TestScannerTriggerClassificationSeparatesMediaAndSidecars(t *testing.T) {
	for _, path := range []string{"track.flac", "movie.mkv"} {
		if !isPrimaryMediaPath(path) || isSidecarTriggerPath(path) {
			t.Fatalf("%q should be primary media only", path)
		}
	}
	for _, path := range []string{"artist.nfo", "folder.jpg", "captions.srt", "lyrics.lrc", ".plexmatch"} {
		if isPrimaryMediaPath(path) || !isSidecarTriggerPath(path) {
			t.Fatalf("%q should be a sidecar only", path)
		}
	}
}

func TestScannerScopesForSidecarBurstCollapseToMusicOwners(t *testing.T) {
	lib := sqlc.Library{
		ID:        4,
		MediaType: sqlc.MediaTypeMusic,
		Paths:     []string{"/music"},
	}
	scopes := scannerScopesForTriggerPaths(lib, []string{
		"/music/Ado/artist.nfo",
		"/music/Ado/2022 - Kyougen/album.nfo",
		"/music/Ado/2024 - Zanmu/folder.jpg",
		"/music/Queen/1975 - A Night at the Opera/album.nfo",
	})
	want := []string{"/music/Ado", "/music/Queen"}
	if len(scopes) != len(want) {
		t.Fatalf("got scopes %v, want %v", scopes, want)
	}
	for i := range want {
		if scopes[i] != want[i] {
			t.Fatalf("got scopes %v, want %v", scopes, want)
		}
	}
}
