package transcoder

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// restartManager simulates a process boundary: a fresh SessionManager over the
// same cache directory, exactly as a restarted server sees it.
func restartManager(t *testing.T, cacheDir string) *SessionManager {
	t.Helper()
	m := NewSessionManager(NewCacheManager(cacheDir, 1), nil, nil)
	t.Cleanup(m.Close)
	return m
}

func writeSegments(t *testing.T, dir string, indices ...int) {
	t.Helper()
	for _, i := range indices {
		require.NoError(t, os.WriteFile(filepath.Join(dir, segName(i)), []byte("segment"), 0o600))
	}
}

func segName(i int) string {
	return "seg_" + itoa(i) + ".m4s"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf []byte
	for i > 0 {
		buf = append([]byte{byte('0' + i%10)}, buf...)
		i /= 10
	}
	return string(buf)
}

func resumeOpts() TranscodeOpts {
	return TranscodeOpts{UseFMP4: true, Profile: Profile{Name: "1080p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 22}}
}

// The headline behaviour: a player whose server restarted mid-stream gets its
// already-encoded segments back instead of a session that thinks it has
// nothing and re-encodes ground the player already covered.
func TestSessionSurvivesRestartAndAdoptsSegments(t *testing.T) {
	cacheDir := t.TempDir()
	opts := resumeOpts()

	first := restartManager(t, cacheDir)
	session := first.GetOrCreate(context.Background(), 42, "/library/movie.mkv", opts, "viewer", 60, nil)
	require.NotEmpty(t, session.OutputDir)
	writeSegments(t, session.OutputDir, 0, 1, 2, 3)
	outputDir := session.OutputDir
	first.Close()

	assert.FileExists(t, filepath.Join(outputDir, "seg_3.m4s"),
		"shutdown must not delete a live session's segments")

	second := restartManager(t, cacheDir)
	resumed := second.GetOrCreate(context.Background(), 42, "/library/movie.mkv", opts, "viewer", 60, nil)
	require.Equal(t, outputDir, resumed.OutputDir, "same session key must map to the same cache directory")

	for _, idx := range []int{0, 1, 2, 3} {
		assert.True(t, resumed.IsSegmentReady(idx), "segment %d should have been adopted from disk", idx)
	}
	assert.False(t, resumed.IsSegmentReady(4), "an unencoded segment must not be marked ready")
	assert.Equal(t, 4, resumed.ReadySegmentCount())
}

// Adoption is only safe when the new plan matches the one that produced the
// bytes. A quality change reuses the same session key, so the fingerprint is
// the only thing standing between the player and a stream that switches
// resolution mid-segment.
func TestSessionDiscardsSegmentsFromADifferentPlan(t *testing.T) {
	cacheDir := t.TempDir()

	first := restartManager(t, cacheDir)
	session := first.GetOrCreate(context.Background(), 42, "/library/movie.mkv", resumeOpts(), "viewer", 60, nil)
	writeSegments(t, session.OutputDir, 0, 1, 2)
	outputDir := session.OutputDir
	first.Close()

	changed := resumeOpts()
	changed.Profile = Profile{Name: "480p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 24, MaxHeight: 480}

	second := restartManager(t, cacheDir)
	resumed := second.GetOrCreate(context.Background(), 42, "/library/movie.mkv", changed, "viewer", 60, nil)
	require.Equal(t, outputDir, resumed.OutputDir)

	assert.Equal(t, 0, resumed.ReadySegmentCount(), "segments from another profile must not be adopted")
	assert.NoFileExists(t, filepath.Join(outputDir, "seg_0.m4s"), "mismatched output must be purged, not left to confuse a head")
}

// A directory left by a build that predates the sidecar (or one whose sidecar
// was lost) has no proof of provenance, so it gets cleared rather than trusted.
func TestSessionDiscardsSegmentsWithoutAManifest(t *testing.T) {
	cacheDir := t.TempDir()

	first := restartManager(t, cacheDir)
	session := first.GetOrCreate(context.Background(), 7, "/library/movie.mkv", resumeOpts(), "viewer", 60, nil)
	writeSegments(t, session.OutputDir, 0, 1)
	outputDir := session.OutputDir
	require.NoError(t, os.Remove(filepath.Join(outputDir, resumeManifestName)))
	first.Close()

	second := restartManager(t, cacheDir)
	resumed := second.GetOrCreate(context.Background(), 7, "/library/movie.mkv", resumeOpts(), "viewer", 60, nil)

	assert.Equal(t, 0, resumed.ReadySegmentCount())
	assert.NoFileExists(t, filepath.Join(outputDir, "seg_0.m4s"))
}

// Idle cleanup and eviction still reclaim disk — restart-resume must not turn
// every abandoned session into a permanent directory.
func TestIdleDisposalStillRemovesOutput(t *testing.T) {
	cacheDir := t.TempDir()
	manager := restartManager(t, cacheDir)

	session := manager.GetOrCreate(context.Background(), 5, "/library/movie.mkv", resumeOpts(), "viewer", 60, nil)
	writeSegments(t, session.OutputDir, 0)
	outputDir := session.OutputDir

	manager.mu.Lock()
	delete(manager.sessions, session.Key)
	manager.mu.Unlock()
	manager.disposeDetachedSessions([]*TranscodeSession{session})

	assert.NoDirExists(t, outputDir, "an idle/evicted session's output is dead weight and must be removed")
}

func TestResumeFingerprintTracksPlanChanges(t *testing.T) {
	ends := []float64{6, 12, 18}
	base := resumeOpts()
	fp := resumeFingerprint("/library/movie.mkv", ".m4s", ends, base)

	assert.Equal(t, fp, resumeFingerprint("/library/movie.mkv", ".m4s", ends, resumeOpts()),
		"identical plans must fingerprint identically across processes")

	audio := resumeOpts()
	audio.AudioTrack = 1
	assert.NotEqual(t, fp, resumeFingerprint("/library/movie.mkv", ".m4s", ends, audio))

	tone := resumeOpts()
	tone.ToneMap = true
	assert.NotEqual(t, fp, resumeFingerprint("/library/movie.mkv", ".m4s", ends, tone))

	hw := resumeOpts()
	hw.HWAccel = HwAccelConfig{Type: HwAccelType("nvenc"), EncoderH264: "h264_nvenc"}
	assert.NotEqual(t, fp, resumeFingerprint("/library/movie.mkv", ".m4s", ends, hw),
		"segments from a different encoder carry incompatible parameter sets")

	assert.NotEqual(t, fp, resumeFingerprint("/library/movie.mkv", ".m4s", []float64{5, 10, 15}, base),
		"shifted boundaries change what segment N means")

	assert.NotEqual(t, fp, resumeFingerprint("/library/other.mkv", ".m4s", ends, base))
}
