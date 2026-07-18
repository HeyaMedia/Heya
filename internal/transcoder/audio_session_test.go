package transcoder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsAllowedAudioBitrate(t *testing.T) {
	for _, kbps := range []int{320, 256, 192, 128} {
		assert.True(t, IsAllowedAudioBitrate(kbps), "kbps=%d", kbps)
	}
	for _, kbps := range []int{0, 64, 96, 160, 224, 288, 384, -1} {
		assert.False(t, IsAllowedAudioBitrate(kbps), "kbps=%d", kbps)
	}
}

func TestAudioProfile(t *testing.T) {
	assert.Equal(t, "aac-256", audioProfile(256))
	assert.Equal(t, "aac-128", audioProfile(128))
}

// TestShouldTranscodeForTier locks in the direct-vs-transcode rule for an
// explicit "quality" tier request:
//   - lossless sources (flac/alac/wav) always transcode — there's headroom
//     worth shaping into the requested bitrate.
//   - lossy sources only transcode when their on-disk bitrate meaningfully
//     exceeds the tier (more than +16kbps of slack); otherwise re-encoding
//     "up" gains nothing and the caller should serve the file direct.
func TestShouldTranscodeForTier(t *testing.T) {
	tests := []struct {
		name        string
		format      string
		bitrateKbps int
		tierKbps    int
		want        bool
	}{
		{"flac always transcodes even far under tier", "flac", 50, 320, true},
		{"alac always transcodes", "alac", 1000, 128, true},
		{"wav always transcodes", "wav", 1411, 320, true},
		{"format case-insensitive lossless", "FLAC", 50, 320, true},

		{"mp3 well under tier serves direct (upsampling gains nothing)", "mp3", 128, 320, false},
		{"mp3 exactly at tier serves direct", "mp3", 192, 192, false},
		{"mp3 within +16 margin serves direct", "mp3", 192, 176, false},
		{"mp3 just over +16 margin transcodes", "mp3", 192, 175, true},
		{"mp3 far over tier transcodes", "mp3", 320, 128, true},
		{"aac at tier serves direct", "aac", 256, 256, false},
		{"m4a at tier serves direct", "m4a", 256, 256, false},
		{"unknown format treated as lossy", "wma", 128, 320, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ShouldTranscodeForTier(tt.format, tt.bitrateKbps, tt.tierKbps))
		})
	}
}

func TestStderrTailBoundsMemory(t *testing.T) {
	tw := newStderrTail(16)
	_, _ = tw.Write([]byte("0123456789"))
	_, _ = tw.Write([]byte("abcdefghij"))
	// 20 bytes written, only the last 16 should survive.
	assert.Equal(t, "456789abcdefghij", tw.String())
	assert.LessOrEqual(t, len(tw.buf), 16)
}

func TestAudioSessionManagerSharesEncodeButWaitersCancelIndependently(t *testing.T) {
	manager := NewAudioSessionManager(NewCacheManager(t.TempDir(), 0))
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	manager.encodeAAC = func(_ context.Context, _, out string, _ int) error {
		calls.Add(1)
		close(started)
		<-release
		return os.WriteFile(out, []byte("aac"), 0o640)
	}

	type result struct {
		path string
		err  error
	}
	leaderDone := make(chan result, 1)
	go func() {
		path, err := manager.EnsureAAC(context.Background(), 7, "source.flac", 256)
		leaderDone <- result{path: path, err: err}
	}()
	<-started

	waiterCtx, cancelWaiter := context.WithCancel(context.Background())
	cancelWaiter()
	_, err := manager.EnsureAAC(waiterCtx, 7, "source.flac", 256)
	assert.ErrorIs(t, err, context.Canceled)
	assert.EqualValues(t, 1, calls.Load(), "a canceled waiter must join, not duplicate, the encode")

	close(release)
	leader := <-leaderDone
	require.NoError(t, leader.err)
	assert.FileExists(t, leader.path)
	assert.EqualValues(t, 1, calls.Load())

	closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
	defer closeCancel()
	require.NoError(t, manager.Close(closeCtx))
}

func TestOpenAACPinsOutputUntilOpenHandleCloses(t *testing.T) {
	cache := NewCacheManager(t.TempDir(), 1)
	cache.maxBytes = 1
	manager := NewAudioSessionManager(cache)
	manager.encodeAAC = func(_ context.Context, _, out string, _ int) error {
		return os.WriteFile(out, []byte("encoded-audio"), 0o640)
	}

	file, err := manager.OpenAAC(context.Background(), 71, "source.flac", 256)
	require.NoError(t, err)
	path := file.Name()

	// An empty/stale SessionManager snapshot must not matter: the open file's
	// own cache lease is the source of truth for its response lifetime.
	require.NoError(t, cache.EvictLRU(nil))
	assert.FileExists(t, path)
	buf := make([]byte, len("encoded-audio"))
	_, err = file.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "encoded-audio", string(buf))

	require.NoError(t, file.Close())
	require.NoError(t, file.Close(), "cached audio handles should be safe in redundant cleanup paths")
	require.NoError(t, cache.EvictLRU(map[string]bool{}))
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "closed audio output should be eligible for eviction")

	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, manager.Close(closeCtx))
}

func TestAudioSessionManagerCloseCancelsAndReapsEncodes(t *testing.T) {
	manager := NewAudioSessionManager(NewCacheManager(t.TempDir(), 0))
	started := make(chan struct{})
	exited := make(chan struct{})
	manager.encodeAAC = func(ctx context.Context, _, _ string, _ int) error {
		close(started)
		<-ctx.Done()
		close(exited)
		return ctx.Err()
	}

	ensureDone := make(chan error, 1)
	go func() {
		_, err := manager.EnsureAAC(context.Background(), 8, "source.flac", 256)
		ensureDone <- err
	}()
	<-started

	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, manager.Close(closeCtx))
	select {
	case <-exited:
	default:
		t.Fatal("Close returned before the encoder exited")
	}
	assert.ErrorIs(t, <-ensureDone, ErrAudioManagerClosed)

	// Close is idempotent and the closed state rejects even cache hits/new work.
	require.NoError(t, manager.Close(closeCtx))
	_, err := manager.EnsureAAC(context.Background(), 9, "source.flac", 256)
	assert.ErrorIs(t, err, ErrAudioManagerClosed)
}

func TestAudioSessionManagerCloseCanTimeOutAndResumeWaiting(t *testing.T) {
	manager := NewAudioSessionManager(NewCacheManager(t.TempDir(), 0))
	started := make(chan struct{})
	release := make(chan struct{})
	manager.encodeAAC = func(ctx context.Context, _, _ string, _ int) error {
		close(started)
		<-release // deliberately delay observing manager cancellation
		return ctx.Err()
	}

	ensureDone := make(chan error, 1)
	go func() {
		_, err := manager.EnsureAAC(context.Background(), 10, "source.flac", 256)
		ensureDone <- err
	}()
	<-started

	shortCtx, shortCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer shortCancel()
	assert.ErrorIs(t, manager.Close(shortCtx), context.DeadlineExceeded)

	close(release)
	longCtx, longCancel := context.WithTimeout(context.Background(), time.Second)
	defer longCancel()
	require.NoError(t, manager.Close(longCtx), "a later Close should resume waiting on the same shutdown")
	assert.ErrorIs(t, <-ensureDone, ErrAudioManagerClosed)
}

func TestReserveAudioOutputIsCollisionSafe(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "42_aac-256.m4a")
	one, err := reserveAtomicOutput(out)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(one) })
	two, err := reserveAtomicOutput(out)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(two) })

	assert.NotEqual(t, one, two)
	assert.DirExists(t, dir)
	assert.FileExists(t, one)
	assert.FileExists(t, two)
}

func TestProduceAtomicOutputPublishesOnlySuccess(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "subtitle.vtt")
	require.NoError(t, os.WriteFile(out, []byte("old"), 0o600))
	wantErr := errors.New("producer failed")

	err := produceAtomicOutput(out, func(tmp string) error {
		require.NoError(t, os.WriteFile(tmp, []byte("partial"), 0o600))
		return wantErr
	})
	require.ErrorIs(t, err, wantErr)
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "old", string(data), "a failed producer must preserve the published artifact")
	matches, err := filepath.Glob(filepath.Join(dir, ".subtitle.vtt.*.tmp"))
	require.NoError(t, err)
	assert.Empty(t, matches, "failed producers must not leak temporary files")

	require.NoError(t, produceAtomicOutput(out, func(tmp string) error {
		return os.WriteFile(tmp, []byte("complete"), 0o600)
	}))
	data, err = os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "complete", string(data))
}

func TestAudioSessionManagerPublishesEncoderFailureToAllWaiters(t *testing.T) {
	manager := NewAudioSessionManager(NewCacheManager(t.TempDir(), 0))
	want := errors.New("encode exploded")
	enc := &audioEncode{done: make(chan struct{})}
	results := make(chan error, 2)
	go func() {
		_, err := manager.waitForEncode(context.Background(), "unused", enc)
		results <- err
	}()
	go func() {
		_, err := manager.waitForEncode(context.Background(), "unused", enc)
		results <- err
	}()

	enc.err = want
	close(enc.done)

	assert.ErrorIs(t, <-results, want)
	assert.ErrorIs(t, <-results, want)
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, manager.Close(closeCtx))
}
