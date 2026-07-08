package watcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
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
