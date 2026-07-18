package transcoder

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeFsSession builds an fMP4 session over a temp output dir, sized for
// exercising segment bookkeeping without spinning up ffmpeg.
func makeFsSession(t *testing.T, totalSegs int) *TranscodeSession {
	t.Helper()
	ends := make([]float64, totalSegs)
	for i := range ends {
		ends[i] = float64(i+1) * 6.0
	}
	segments := make([]*segReady, totalSegs)
	for i := range segments {
		segments[i] = newSegReady()
	}
	return &TranscodeSession{
		Key:         "test",
		FilePath:    filepath.Join(t.TempDir(), "input.mkv"),
		OutputDir:   t.TempDir(),
		SegExt:      ".m4s",
		segPathFmt:  "seg_%d.m4s",
		TotalSegs:   totalSegs,
		SegmentEnds: ends,
		segments:    segments,
		LastAccess:  time.Now(),
	}
}

func touchSeg(t *testing.T, s *TranscodeSession, idx int) {
	t.Helper()
	require.NoError(t, os.WriteFile(s.SegmentPath(idx), []byte("x"), 0o644))
}

func TestComputeCopyVideoSegmentEnds_UsesPersistedExactBoundaries(t *testing.T) {
	kf := &Keyframes{
		IFrames:            []float64{0, 2, 6, 8, 12},
		Duration:           18,
		HLSBoundaryVersion: HLSBoundaryVersion,
		HLSSegmentDuration: SegmentDuration,
		HLSSegmentEnds:     []float64{6, 14, 18},
	}
	assert.Equal(t, []float64{6, 14, 18}, computeCopyVideoSegmentEnds(18, kf))
}

func TestComputeCopyVideoSegmentEnds_LegacyArtifactFallsBackImmediately(t *testing.T) {
	kf := &Keyframes{IFrames: []float64{0, 6, 12}, Duration: 18}
	assert.Equal(t, []float64{6, 12, 18}, computeCopyVideoSegmentEnds(18, kf))

	// A stale algorithm version must not be trusted.
	kf.HLSBoundaryVersion = HLSBoundaryVersion + 1
	kf.HLSSegmentDuration = SegmentDuration
	kf.HLSSegmentEnds = []float64{9, 18}
	assert.Equal(t, []float64{6, 12, 18}, computeCopyVideoSegmentEnds(18, kf))
}

// The output dir accumulates disjoint segment ranges from previous heads
// (earlier seek targets). Reconcile must mark exactly the files present —
// range-filling to the highest index on disk marked never-encoded gap
// segments ready, which dead-ended playback in permanent 404s after a
// backward seek.
func TestReconcileSegments_GapsStayUnready(t *testing.T) {
	s := makeFsSession(t, 200)
	for _, idx := range []int{5, 6, 100, 101} {
		touchSeg(t, s, idx)
	}
	// In-progress temp file and init segment must be ignored.
	require.NoError(t, os.WriteFile(filepath.Join(s.OutputDir, "seg_7.m4s.tmp"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(s.OutputDir, "init.mp4"), []byte("x"), 0o644))

	head := &Head{StartSeg: 5, CurrentSeg: 5}
	s.reconcileSegmentsFromFS(head)

	for _, idx := range []int{5, 6, 100, 101} {
		assert.True(t, s.IsSegmentReady(idx), "seg %d should be ready", idx)
	}
	for _, idx := range []int{4, 7, 50, 99, 102} {
		assert.False(t, s.IsSegmentReady(idx), "seg %d must NOT be ready", idx)
	}
}

// The head cursor must follow the head's own contiguous run, not the global
// max on disk: adopting an older head's far-ahead index fakes forward
// progress and trips the lead cap on a head that just spawned.
func TestReconcileSegments_CursorStopsAtGap(t *testing.T) {
	s := makeFsSession(t, 200)
	for _, idx := range []int{5, 6, 100, 101} {
		touchSeg(t, s, idx)
	}
	head := &Head{StartSeg: 5, CurrentSeg: 5}
	s.reconcileSegmentsFromFS(head)
	assert.Equal(t, 6, head.CurrentSeg)
}

func TestReconcileSegments_NoOwnOutputYet(t *testing.T) {
	s := makeFsSession(t, 200)
	touchSeg(t, s, 100) // older head's output only
	head := &Head{StartSeg: 5, CurrentSeg: 5}
	s.reconcileSegmentsFromFS(head)
	assert.Equal(t, 5, head.CurrentSeg)
	assert.True(t, s.IsSegmentReady(100))
	assert.False(t, s.IsSegmentReady(5))
}

// A running head only encodes forward from its cursor (last FLUSHED segment).
// needsNewHead is only consulted for segments that are NOT ready, so anything
// at or behind the cursor — a backward seek, or a passed segment whose file
// vanished and had its latch reset — will never arrive from this head and
// needs a new one.
func TestNeedsNewHead_BackwardSeek(t *testing.T) {
	s := makeFsSession(t, 200)
	s.head = &Head{StartSeg: 50, CurrentSeg: 60, Done: make(chan struct{})}

	assert.True(t, s.needsNewHead(40), "behind head start → new head")
	assert.True(t, s.needsNewHead(55), "unready behind the cursor (vanished file) → new head")
	assert.True(t, s.needsNewHead(60), "unready at the cursor → new head")
	assert.False(t, s.needsNewHead(65), "shortly ahead → keep head")
	assert.True(t, s.needsNewHead(75), "past seek threshold → new head")

	close(s.head.Done)
	assert.True(t, s.needsNewHead(65), "finished head → new head")
}

// A freshly spawned head sits at CurrentSeg = StartSeg-1 (nothing flushed).
// Its own start segment must NOT read as "already passed", or the request
// that spawned it would kill/spawn heads in an infinite loop.
func TestNeedsNewHead_FreshHeadServesItsStartSegment(t *testing.T) {
	s := makeFsSession(t, 200)
	s.head = &Head{StartSeg: 50, CurrentSeg: 49, Done: make(chan struct{})}

	assert.False(t, s.needsNewHead(50), "fresh head's own start segment → keep head")
	assert.False(t, s.needsNewHead(51), "just ahead of a fresh head → keep head")
	assert.True(t, s.needsNewHead(49), "behind the fresh head's start → new head")
}

// fakeBuilder produces a command that runs long but never writes segments,
// standing in for ffmpeg in livelock/timeout tests.
type fakeBuilder struct{}

func (fakeBuilder) BuildHLSCommand(ctx context.Context, opts TranscodeOpts) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, "sleep", "60"), nil
}
func (fakeBuilder) IsAvailable() bool                  { return true }
func (fakeBuilder) FormatCommand(cmd *exec.Cmd) string { return "fake" }

type testCommandBuilder struct {
	build func(context.Context, TranscodeOpts) (*exec.Cmd, error)
}

func (b testCommandBuilder) BuildHLSCommand(ctx context.Context, opts TranscodeOpts) (*exec.Cmd, error) {
	return b.build(ctx, opts)
}
func (testCommandBuilder) IsAvailable() bool { return true }
func (testCommandBuilder) FormatCommand(cmd *exec.Cmd) string {
	if cmd == nil {
		return "<nil>"
	}
	return "test-command"
}

// Livelock canary: the first request of a fresh session spawns a head for
// exactly that segment. RequestSegment must settle into WaitForSegment and
// time out cleanly — not kill/spawn the head it just created forever.
func TestRequestSegment_FreshSpawnDoesNotLivelock(t *testing.T) {
	s := makeFsSession(t, 200)
	s.builder = fakeBuilder{}

	done := make(chan bool, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		done <- s.RequestSegment(ctx, 8)
	}()

	select {
	case ok := <-done:
		assert.False(t, ok, "no segments were produced, request must time out")
	case <-time.After(10 * time.Second):
		t.Fatal("RequestSegment did not return — kill/spawn livelock")
	}
	s.Kill()
}

// A ready-marked segment whose file vanished (cache eviction, manual delete)
// must be resettable so a head can re-encode it — otherwise every request
// serves a 404 forever.
func TestResetSegment_ReopensLatch(t *testing.T) {
	s := makeFsSession(t, 10)
	s.markSegmentReady(3)
	require.True(t, s.IsSegmentReady(3))
	assert.False(t, s.segmentFileExists(3))

	s.resetSegment(3)
	assert.False(t, s.IsSegmentReady(3))

	s.markSegmentReady(3)
	assert.True(t, s.IsSegmentReady(3), "fresh latch must be markable again")
}

func TestRequestSegment_ReadyWithFileServesImmediately(t *testing.T) {
	s := makeFsSession(t, 10)
	touchSeg(t, s, 2)
	s.markSegmentReady(2)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assert.True(t, s.RequestSegment(ctx, 2))
	assert.Equal(t, 2, s.LastRequestedSegment())
}

func TestEnsureSegmentRejectsInvalidIndex(t *testing.T) {
	s := makeFsSession(t, 3)
	assert.ErrorIs(t, s.EnsureSegment(context.Background(), -1), ErrInvalidSegment)
	assert.ErrorIs(t, s.EnsureSegment(context.Background(), 3), ErrInvalidSegment)
}

func TestEnsureSegmentReturnsBuilderFailurePromptly(t *testing.T) {
	want := errors.New("cannot construct encoder")
	s := makeFsSession(t, 10)
	s.builder = testCommandBuilder{build: func(context.Context, TranscodeOpts) (*exec.Cmd, error) {
		return nil, want
	}}
	defer s.Kill()

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.EnsureSegment(ctx, 2)

	assert.ErrorIs(t, err, ErrTranscodeFailed)
	assert.ErrorIs(t, err, want)
	assert.Less(t, time.Since(started), time.Second, "build failure must wake the request, not wait for its deadline")
	assert.False(t, s.RequestSegment(context.Background(), 2), "compatibility API must preserve failure as false")
}

func TestEnsureSegmentReturnsStartFailurePromptly(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-encoder")
	s := makeFsSession(t, 10)
	s.builder = testCommandBuilder{build: func(ctx context.Context, _ TranscodeOpts) (*exec.Cmd, error) {
		return exec.CommandContext(ctx, missing), nil
	}}
	defer s.Kill()

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.EnsureSegment(ctx, 2)

	assert.ErrorIs(t, err, ErrTranscodeFailed)
	assert.Contains(t, err.Error(), "start encoder")
	assert.Less(t, time.Since(started), time.Second, "start failure must wake the request, not wait for its deadline")
}

func TestEnsureSegmentReturnsEarlyExitPromptly(t *testing.T) {
	s := makeFsSession(t, 10)
	s.builder = testCommandBuilder{build: func(ctx context.Context, _ TranscodeOpts) (*exec.Cmd, error) {
		return exec.CommandContext(ctx, "sh", "-c", "exit 17"), nil
	}}
	defer s.Kill()

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.EnsureSegment(ctx, 2)

	assert.ErrorIs(t, err, ErrTranscodeFailed)
	assert.Contains(t, err.Error(), "encoder exited")
	assert.Less(t, time.Since(started), time.Second, "early exit must wake the request, not wait for its deadline")
}

func TestEnsureSegmentAcceptsFileFlushedImmediatelyBeforeExit(t *testing.T) {
	s := makeFsSession(t, 10)
	s.builder = testCommandBuilder{build: func(ctx context.Context, opts TranscodeOpts) (*exec.Cmd, error) {
		return exec.CommandContext(ctx, "sh", "-c", `touch "$1/seg_2.m4s"`, "sh", opts.OutputDir), nil
	}}
	defer s.Kill()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, s.EnsureSegment(ctx, 2))
	assert.True(t, s.IsSegmentReady(2))
	assert.True(t, s.segmentFileExists(2))
}

func TestEnsureSegmentDoesNotRespawnAfterSessionKill(t *testing.T) {
	built := make(chan struct{})
	s := makeFsSession(t, 10)
	s.builder = testCommandBuilder{build: func(ctx context.Context, _ TranscodeOpts) (*exec.Cmd, error) {
		close(built)
		return exec.CommandContext(ctx, "sleep", "60"), nil
	}}

	done := make(chan error, 1)
	go func() {
		done <- s.EnsureSegment(context.Background(), 2)
	}()
	<-built
	s.Kill()
	assert.ErrorIs(t, <-done, ErrTranscodeSessionClosed)

	s.mu.Lock()
	defer s.mu.Unlock()
	assert.Nil(t, s.head, "terminal session cleanup must not be mistaken for a seek replacement")
}

func TestSessionManagerEvictsPreviousSessionForSameFile(t *testing.T) {
	cache := NewCacheManager(t.TempDir(), 0)
	manager := NewSessionManager(cache, nil, nil)
	defer manager.Close()

	opts := TranscodeOpts{AudioTrack: 1, UseFMP4: true}
	first := manager.GetOrCreate(context.Background(), 42, "/library/movie.mkv", opts, "viewer-a", 12, nil)
	second := manager.GetOrCreate(context.Background(), 42, "/library/movie.mkv", opts, "viewer-b", 12, nil)

	require.NotSame(t, first, second)
	assert.NotEqual(t, first.Key, second.Key)
	assert.NotEqual(t, first.OutputDir, second.OutputDir)
	assert.Nil(t, manager.GetExistingSession(42, 1, "viewer-a"))
	assert.Same(t, second, manager.GetExistingSession(42, 1, "viewer-b"))
	assert.ErrorIs(t, first.EnsureSegment(context.Background(), 0), ErrTranscodeSessionClosed)
	_, err := os.Stat(first.OutputDir)
	assert.True(t, os.IsNotExist(err))
	assert.DirExists(t, second.OutputDir)
}

func TestDetachedSessionCleanupPreservesRecreatedSameKeyDirectory(t *testing.T) {
	cache := NewCacheManager(t.TempDir(), 1)
	manager := NewSessionManager(cache, nil, nil)
	defer manager.Close()

	const key = "42:a1:viewer-a"
	oldLease, err := cache.reserveSegmentDir(key)
	require.NoError(t, err)
	old := &TranscodeSession{Key: key, OutputDir: oldLease.Path(), cacheLease: oldLease}
	newLease, err := cache.reserveSegmentDir(key)
	require.NoError(t, err)
	replacement := &TranscodeSession{Key: key, OutputDir: newLease.Path(), cacheLease: newLease}
	manager.mu.Lock()
	manager.sessions[key] = replacement
	manager.mu.Unlock()

	marker := filepath.Join(replacement.OutputDir, "replacement.m4s")
	require.NoError(t, os.WriteFile(marker, []byte("new"), 0o644))
	manager.disposeDetachedSessions([]*TranscodeSession{old})

	assert.FileExists(t, marker)
	cache.mu.RLock()
	assert.Equal(t, 1, cache.pins[replacement.OutputDir], "old lease release must retain the replacement pin")
	cache.mu.RUnlock()
}

func TestSessionManagerCloseJoinsInFlightEvictionTeardown(t *testing.T) {
	cache := NewCacheManager(t.TempDir(), 1)
	manager := NewSessionManager(cache, nil, nil)

	oldLease, err := cache.reserveSegmentDir("42:a1:old")
	require.NoError(t, err)
	cancelled := make(chan struct{})
	headDone := make(chan struct{})
	old := &TranscodeSession{
		Key:        "42:a1:old",
		OutputDir:  oldLease.Path(),
		cacheLease: oldLease,
		head: &Head{
			Cancel: func() { close(cancelled) },
			Done:   headDone,
		},
	}
	manager.mu.Lock()
	manager.sessions[old.Key] = old
	manager.mu.Unlock()

	createDone := make(chan struct{})
	go func() {
		manager.GetOrCreate(context.Background(), 42, "/library/movie.mkv", TranscodeOpts{AudioTrack: 1, UseFMP4: true}, "new", 12, nil)
		close(createDone)
	}()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("replacement did not begin reaping the evicted head")
	}

	closeDone := make(chan struct{})
	go func() {
		manager.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		t.Fatal("SessionManager.Close returned while eviction teardown was still in flight")
	case <-time.After(20 * time.Millisecond):
	}

	close(headDone)
	select {
	case <-createDone:
	case <-time.After(time.Second):
		t.Fatal("in-flight creation did not finish after the evicted head exited")
	}
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("SessionManager.Close did not finish after joined creation teardown")
	}
}

func TestSessionManagerLeaseProtectsLiveOutputWithEmptySnapshot(t *testing.T) {
	cache := NewCacheManager(t.TempDir(), 1)
	cache.maxBytes = 1
	manager := NewSessionManager(cache, nil, nil)

	session := manager.GetOrCreate(context.Background(), 88, "/library/movie.mkv", TranscodeOpts{UseFMP4: true}, "viewer", 12, nil)
	require.NoError(t, os.WriteFile(filepath.Join(session.OutputDir, "seg_0.m4s"), []byte("segment"), 0o644))

	// The old cleanup code relied only on a separately-captured live map. A
	// stale/empty snapshot must no longer be able to evict a registered session.
	require.NoError(t, cache.EvictLRU(nil))
	assert.FileExists(t, filepath.Join(session.OutputDir, "seg_0.m4s"))

	manager.Close()
	_, err := os.Stat(session.OutputDir)
	assert.True(t, os.IsNotExist(err), "Close stops/removes the session before releasing its lease")
	cache.mu.RLock()
	assert.Empty(t, cache.pins)
	cache.mu.RUnlock()
}

func TestSessionManagerCloseIsTerminalForNewSessions(t *testing.T) {
	cache := NewCacheManager(t.TempDir(), 1)
	manager := NewSessionManager(cache, nil, testCommandBuilder{build: func(context.Context, TranscodeOpts) (*exec.Cmd, error) {
		t.Fatal("closed manager must not build an ffmpeg command")
		return nil, nil
	}})
	manager.Close()

	session := manager.GetOrCreate(context.Background(), 89, "/library/movie.mkv", TranscodeOpts{UseFMP4: true}, "viewer", 12, nil)
	require.NotNil(t, session)
	assert.Empty(t, session.OutputDir, "closed manager must not reserve a cache directory")
	assert.ErrorIs(t, session.EnsureSegment(context.Background(), 0), ErrTranscodeSessionClosed)
	assert.Nil(t, manager.GetExistingSession(89, 0, "viewer"))

	cache.mu.RLock()
	assert.Empty(t, cache.pins)
	cache.mu.RUnlock()
}

func TestTranscodeHeadRecreatesLeasedOutputAfterExplicitClear(t *testing.T) {
	cache := NewCacheManager(t.TempDir(), 1)
	input := filepath.Join(t.TempDir(), "input.mkv")
	require.NoError(t, os.WriteFile(input, []byte("input"), 0o644))
	builder := testCommandBuilder{build: func(ctx context.Context, opts TranscodeOpts) (*exec.Cmd, error) {
		return exec.CommandContext(ctx, "sh", "-c", `touch "$1/seg_0.m4s"`, "sh", opts.OutputDir), nil
	}}
	manager := NewSessionManager(cache, nil, builder)
	defer manager.Close()

	session := manager.GetOrCreate(context.Background(), 99, input, TranscodeOpts{UseFMP4: true}, "viewer", 6, nil)
	require.DirExists(t, session.OutputDir)
	require.NoError(t, cache.Clear())
	_, err := os.Stat(session.OutputDir)
	require.True(t, os.IsNotExist(err))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, session.EnsureSegment(ctx, 0))
	assert.FileExists(t, session.SegmentPath(0))
}

func TestEvictLRU_SkipsLiveSessionDirs(t *testing.T) {
	base := t.TempDir()
	c := NewCacheManager(base, 1)
	c.maxBytes = 10 // force eviction with tiny payloads

	oldDir := filepath.Join(base, "old-live")
	newDir := filepath.Join(base, "new-idle")
	require.NoError(t, os.MkdirAll(oldDir, 0o755))
	require.NoError(t, os.MkdirAll(newDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldDir, "seg_0.m4s"), make([]byte, 100), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(newDir, "seg_0.m4s"), make([]byte, 100), 0o644))
	// Make oldDir the LRU candidate.
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(oldDir, past, past))

	require.NoError(t, c.EvictLRU(map[string]bool{oldDir: true}))

	_, err := os.Stat(oldDir)
	assert.NoError(t, err, "live session dir must survive eviction")
	_, err = os.Stat(newDir)
	assert.True(t, os.IsNotExist(err), "idle dir should be evicted")
}

func TestParseSegIdx(t *testing.T) {
	cases := map[string]int{
		"seg_0.m4s":      0,
		"seg_334.m4s":    334,
		"seg_0012.ts":    12,
		"init.mp4":       -1,
		"_ffmpeg.m3u8":   -1,
		"seg_x.m4s":      -1,
		"head_5_cmd.txt": -1,
	}
	for name, want := range cases {
		assert.Equal(t, want, parseSegIdx(name), "parseSegIdx(%q)", name)
	}
}
