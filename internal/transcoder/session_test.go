package transcoder

import (
	"context"
	"os"
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

// A running head only encodes forward from its start segment; a request
// behind that start (backward seek) must spawn a new head instead of waiting
// on the running one forever.
func TestNeedsNewHead_BackwardSeek(t *testing.T) {
	s := makeFsSession(t, 200)
	s.head = &Head{StartSeg: 50, CurrentSeg: 60, Done: make(chan struct{})}

	assert.True(t, s.needsNewHead(40), "behind head start → new head")
	assert.False(t, s.needsNewHead(55), "inside the head's run → keep head")
	assert.False(t, s.needsNewHead(65), "shortly ahead → keep head")
	assert.True(t, s.needsNewHead(75), "past seek threshold → new head")

	close(s.head.Done)
	assert.True(t, s.needsNewHead(55), "finished head → new head")
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
