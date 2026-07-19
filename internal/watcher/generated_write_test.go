package watcher

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/karbowiak/heya/internal/generatedwrite"
	"github.com/stretchr/testify/require"
)

func TestGeneratedWriteSuppressionRequiresExactCurrentFile(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	path := filepath.Join(t.TempDir(), "album.nfo")
	generated := []byte("generated sidecar")
	require.NoError(t, os.WriteFile(path, generated, 0o644))
	require.NoError(t, manager.SuppressGeneratedWrite(generatedwrite.FromBytes(path, generated)))
	require.True(t, manager.shouldSuppressGeneratedEvent(path))

	// Same-sized user content must invalidate the record rather than inheriting
	// the generated file's quiet period.
	require.NoError(t, os.WriteFile(path, []byte("user edit differs"), 0o644))
	require.False(t, manager.shouldSuppressGeneratedEvent(path))
	require.False(t, manager.shouldSuppressGeneratedEvent(path), "a mismatched record must be discarded")
}

func TestGeneratedWriteSuppressionExpires(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	now := time.Unix(1_700_000_000, 0)
	manager.now = func() time.Time { return now }
	path := filepath.Join(t.TempDir(), "artist.nfo")
	content := []byte("generated")
	require.NoError(t, os.WriteFile(path, content, 0o644))
	require.NoError(t, manager.SuppressGeneratedWrite(generatedwrite.FromBytes(path, content)))

	now = now.Add(generatedWriteTTL + time.Nanosecond)
	require.False(t, manager.shouldSuppressGeneratedEvent(path))
}

func TestGeneratedWriteRetryDoesNotInstallLiveSuppression(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	path := filepath.Join(t.TempDir(), "artist.nfo")
	content := []byte("already published generated sidecar")
	require.NoError(t, os.WriteFile(path, content, 0o644))

	require.NoError(t, manager.SuppressGeneratedWrite(generatedwrite.AttestBytes(path, content)))
	require.False(t, manager.shouldSuppressGeneratedEvent(path), "an attestation-only retry emitted no fsnotify event")
}

func TestGeneratedSidecarCannotHideRealMediaEventInDirectoryDebounce(t *testing.T) {
	restore := useShortDebounces()
	t.Cleanup(restore)

	var scans atomic.Int32
	manager := NewManager(nil, nil, func(_ int64, _ bool) { scans.Add(1) })
	watcher, ctx := registerTestWatcher(t, manager, 71, 1)
	t.Cleanup(manager.StopAll)

	dir := filepath.Join(watcher.rootPath, "Ado", "Kyougen")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	trackPath := filepath.Join(dir, "01 - New Genesis.flac")
	nfoPath := filepath.Join(dir, "album.nfo")
	require.NoError(t, os.WriteFile(trackPath, []byte("audio"), 0o644))
	nfoContent := []byte("generated nfo")
	require.NoError(t, os.WriteFile(nfoPath, nfoContent, 0o644))
	require.NoError(t, manager.SuppressGeneratedWrite(generatedwrite.FromBytes(nfoPath, nfoContent)))

	manager.handleEvent(ctx, watcher, fsnotify.Event{Name: trackPath, Op: fsnotify.Write})
	manager.handleEvent(ctx, watcher, fsnotify.Event{Name: nfoPath, Op: fsnotify.Write})
	require.Eventually(t, func() bool { return scans.Load() == 1 }, time.Second, time.Millisecond)

	// A later duplicate event for the exact generated sidecar stays quiet.
	manager.handleEvent(ctx, watcher, fsnotify.Event{Name: nfoPath, Op: fsnotify.Write})
	time.Sleep(4 * eventDebounceDelay)
	require.Equal(t, int32(1), scans.Load())
}

func TestGeneratedWriteInternalExchangePathsNeverTriggerScanOrHideMediaEdit(t *testing.T) {
	restore := useShortDebounces()
	t.Cleanup(restore)
	var scans atomic.Int32
	manager := NewManager(nil, nil, func(_ int64, _ bool) { scans.Add(1) })
	watcher, ctx := registerTestWatcher(t, manager, 73, 1)
	t.Cleanup(manager.StopAll)
	dir := filepath.Join(watcher.rootPath, "Ado")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	temporary := filepath.Join(dir, ".heya-atomic-artist.nfo.123456.tmp")
	previous := filepath.Join(dir, ".heya-generated-550e8400-e29b-41d4-a716-446655440000.previous")

	manager.handleEvent(ctx, watcher, fsnotify.Event{Name: temporary, Op: fsnotify.Create | fsnotify.Write})
	manager.handleEvent(ctx, watcher, fsnotify.Event{Name: previous, Op: fsnotify.Rename})
	time.Sleep(4 * eventDebounceDelay)
	require.Zero(t, scans.Load(), "private staging/exchange entries must stay invisible")

	track := filepath.Join(dir, "01.flac")
	require.NoError(t, os.WriteFile(track, []byte("audio"), 0o644))
	manager.handleEvent(ctx, watcher, fsnotify.Event{Name: temporary, Op: fsnotify.Create})
	manager.handleEvent(ctx, watcher, fsnotify.Event{Name: track, Op: fsnotify.Write})
	require.Eventually(t, func() bool { return scans.Load() == 1 }, time.Second, time.Millisecond)
}

func TestGeneratedSidecarRenameSuppressionPreservesLaterUserEdit(t *testing.T) {
	restore := useShortDebounces()
	t.Cleanup(restore)

	jobs := make(chan []string, 1)
	manager := NewManager(nil, nil, nil)
	manager.sidecarRescanInsert = func(_ context.Context, _ int64, paths []string) error {
		jobs <- paths
		return nil
	}
	watcher, ctx := registerTestWatcher(t, manager, 72, 1)
	t.Cleanup(manager.StopAll)

	path := filepath.Join(watcher.rootPath, "Ado", "artist.nfo")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := []byte("generated artist nfo")
	require.NoError(t, os.WriteFile(path, content, 0o644))
	require.NoError(t, manager.SuppressGeneratedWrite(generatedwrite.FromBytes(path, content)))

	// Atomic replacement can surface as Rename/Remove even though the newly
	// published destination already exists. That exact destination is quiet.
	manager.handleEvent(ctx, watcher, fsnotify.Event{Name: path, Op: fsnotify.Rename})
	select {
	case got := <-jobs:
		t.Fatalf("exact generated rename enqueued a scan: %v", got)
	case <-time.After(4 * sidecarRescanDebounceDelay):
	}

	require.NoError(t, os.WriteFile(path, []byte("user-owned artist nfo"), 0o644))
	manager.handleEvent(ctx, watcher, fsnotify.Event{Name: path, Op: fsnotify.Rename})
	select {
	case got := <-jobs:
		require.Equal(t, []string{path}, got)
	case <-time.After(time.Second):
		t.Fatal("later user edit did not enqueue a sidecar rescan")
	}
}
