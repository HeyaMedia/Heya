package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
)

// TestAddRecursiveBounded verifies the happy path: a healthy tree arms fully
// (non-hidden dirs watched, hidden dirs skipped) and the bounded wrapper
// returns without hitting the timeout. The stalled-mount timeout path can't be
// simulated deterministically in a unit test (it needs a wedged Getdents), but
// the select logic that guards it is exercised in production.
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

// TestWatchDoesNotResurrectUnwatched guards the arm/unwatch race: a library
// unwatched while its (slow) recursive walk is still in flight must NOT be
// re-added to the watcher set when the walk finishes.
func TestWatchDoesNotResurrectUnwatched(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	orig := recursiveWalkFn
	recursiveWalkFn = func(_ *fsnotify.Watcher, _ string) error {
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
