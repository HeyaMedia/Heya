package transcoder

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSegmentDirUsesHLSNamespace(t *testing.T) {
	base := t.TempDir()
	cache := NewCacheManager(base, 0)

	dir := cache.SegmentDir("session-key")
	assert.Equal(t, filepath.Join(base, hlsCacheNamespace, safeCacheKey("session-key")), dir)
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestNewSessionManagerDoesNotClearCache(t *testing.T) {
	base := t.TempDir()
	cache := NewCacheManager(base, 0)
	audio := filepath.Join(base, audioCacheNamespace, "42_aac-256.m4a")
	sentinel := filepath.Join(base, "owned-by-someone-else")
	require.NoError(t, os.MkdirAll(filepath.Dir(audio), 0o755))
	require.NoError(t, os.WriteFile(audio, []byte("aac"), 0o644))
	require.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0o644))

	manager := NewSessionManager(cache, nil, nil)
	manager.Close()

	_, err := os.Stat(audio)
	assert.NoError(t, err, "constructing a live-session manager must preserve cached audio")
	_, err = os.Stat(sentinel)
	assert.NoError(t, err, "constructing a manager must have no unrelated filesystem side effects")
}

func TestEvictLRUEvictsIndividualAudioItems(t *testing.T) {
	base := t.TempDir()
	cache := NewCacheManager(base, 1)
	cache.maxBytes = 150

	audioDir := filepath.Join(base, audioCacheNamespace)
	require.NoError(t, os.MkdirAll(audioDir, 0o755))
	oldTrack := filepath.Join(audioDir, "1_aac-256.m4a")
	newTrack := filepath.Join(audioDir, "2_aac-256.m4a")
	require.NoError(t, os.WriteFile(oldTrack, make([]byte, 100), 0o644))
	require.NoError(t, os.WriteFile(newTrack, make([]byte, 100), 0o644))
	now := time.Now()
	require.NoError(t, os.Chtimes(oldTrack, now.Add(-time.Hour), now.Add(-time.Hour)))
	require.NoError(t, os.Chtimes(newTrack, now, now))

	require.NoError(t, cache.EvictLRU(nil))

	_, err := os.Stat(oldTrack)
	assert.True(t, os.IsNotExist(err), "only the least-recently-used audio item should be evicted")
	_, err = os.Stat(newTrack)
	assert.NoError(t, err, "a newer sibling in the audio namespace must survive")
	info, err := os.Stat(audioDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "LRU must not evict a namespace root as one item")
}

func TestEvictLRUProtectsPinnedProducerOutput(t *testing.T) {
	base := t.TempDir()
	cache := NewCacheManager(base, 1)
	cache.maxBytes = 10

	audioDir := filepath.Join(base, audioCacheNamespace)
	require.NoError(t, os.MkdirAll(audioDir, 0o755))
	pinned := filepath.Join(audioDir, "1_aac-256.m4a.tmp")
	idle := filepath.Join(audioDir, "2_aac-256.m4a")
	require.NoError(t, os.WriteFile(pinned, make([]byte, 100), 0o644))
	require.NoError(t, os.WriteFile(idle, make([]byte, 100), 0o644))
	now := time.Now()
	require.NoError(t, os.Chtimes(pinned, now.Add(-time.Hour), now.Add(-time.Hour)))
	require.NoError(t, os.Chtimes(idle, now, now))

	release := cache.pin(pinned)
	defer release()
	require.NoError(t, cache.EvictLRU(nil))

	_, err := os.Stat(pinned)
	assert.NoError(t, err, "active producer output must remain pinned")
	_, err = os.Stat(idle)
	assert.True(t, os.IsNotExist(err), "an unpinned item should be evicted instead")
}

func TestProducerReservationAndPinAreAtomicWithLRU(t *testing.T) {
	base := t.TempDir()
	cache := NewCacheManager(base, 1)
	cache.maxBytes = 1
	audioDir := filepath.Join(base, audioCacheNamespace)
	require.NoError(t, os.MkdirAll(audioDir, 0o755))
	out := filepath.Join(audioDir, "1_aac-256.m4a")

	cache.mu.Lock()
	tmp, err := reserveAtomicOutput(out)
	require.NoError(t, err)
	pinned := cache.pinLocked(out, tmp)
	cache.mu.Unlock()
	release := cache.releasePins(pinned)
	defer release()
	defer func() { _ = os.Remove(tmp) }()

	require.NoError(t, cache.EvictLRU(nil))
	assert.FileExists(t, tmp)
}

func TestCacheLeaseReferenceCountsSameKeyAndReleasesIdempotently(t *testing.T) {
	base := t.TempDir()
	cache := NewCacheManager(base, 1)
	cache.maxBytes = 1

	first, err := cache.reserveSegmentDir("same-session")
	require.NoError(t, err)
	second, err := cache.reserveSegmentDir("same-session")
	require.NoError(t, err)
	require.Equal(t, first.Path(), second.Path())
	require.NoError(t, os.WriteFile(filepath.Join(first.Path(), "seg_0.m4s"), []byte("payload"), 0o644))

	first.Release()
	first.Release() // an old cleanup path must not decrement the replacement
	require.NoError(t, cache.EvictLRU(nil))
	assert.DirExists(t, second.Path(), "the replacement session's lease must survive old-session cleanup")

	second.Release()
	require.NoError(t, cache.EvictLRU(nil))
	_, err = os.Stat(second.Path())
	assert.True(t, os.IsNotExist(err), "the cache item becomes evictable after the final lease")
}

func TestReserveSegmentFileProtectsExtractionAndResponseWithEmptyLiveSnapshot(t *testing.T) {
	cache := NewCacheManager(t.TempDir(), 1)
	cache.maxBytes = 1
	lease, err := cache.ReserveSegmentFile("subtitle-42", "subtitle.vtt")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(lease.Path(), []byte("WEBVTT\n"), 0o644))

	require.NoError(t, cache.EvictLRU(nil))
	assert.FileExists(t, lease.Path(), "a subtitle must remain present while its response lease is held")

	lease.Release()
	require.NoError(t, cache.EvictLRU(map[string]bool{}))
	_, err = os.Stat(lease.Path())
	assert.True(t, os.IsNotExist(err))
}

func TestReadCacheStatsDoesNotCreateMissingDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-created")
	stats := ReadCacheStats(missing, 7)
	assert.Equal(t, CacheStats{MaxSizeGB: 7}, stats)
	_, err := os.Stat(missing)
	assert.True(t, os.IsNotExist(err), "read-only diagnostics must not create the configured cache path")

	existing := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(existing, "one"), []byte("123"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(existing, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(existing, "nested", "two"), []byte("4567"), 0o644))
	stats = ReadCacheStats(existing, 3)
	assert.Equal(t, int64(7), stats.TotalSize)
	assert.Equal(t, 2, stats.ItemCount)
	assert.Equal(t, 3, stats.MaxSizeGB)
}

func TestClearRemainsExplicitClearAll(t *testing.T) {
	base := t.TempDir()
	cache := NewCacheManager(base, 0)
	hlsFile := filepath.Join(cache.SegmentDir("session"), "seg_0.m4s")
	audioFile := filepath.Join(base, audioCacheNamespace, "1_aac-256.m4a")
	require.NoError(t, os.WriteFile(hlsFile, []byte("hls"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Dir(audioFile), 0o755))
	require.NoError(t, os.WriteFile(audioFile, []byte("aac"), 0o644))

	require.NoError(t, cache.Clear())
	entries, err := os.ReadDir(base)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestCacheManagerSetMaxSizeGB(t *testing.T) {
	cache := NewCacheManager(t.TempDir(), 1)
	assert.Equal(t, 1, cache.Stats().MaxSizeGB)

	cache.SetMaxSizeGB(0)

	assert.Equal(t, 0, cache.Stats().MaxSizeGB, "zero is the live unlimited setting")
}
