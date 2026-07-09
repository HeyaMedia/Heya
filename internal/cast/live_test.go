package cast

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveAirplay exercises the full transport stack against a real
// AirPlay receiver: discovery → spawn → PCM feed → metadata → volume →
// seek → graceful stop. Skipped unless explicitly requested — it makes
// actual sound in an actual room.
//
//	HEYA_CAST_LIVE_TEST=1 \
//	HEYA_CAST_LIVE_FILE=/path/to/track.m4a \
//	HEYA_CAST_LIVE_DEVICE=Anlæg \
//	go test ./internal/cast/ -run TestLiveAirplay -v -timeout 180s
func TestLiveAirplay(t *testing.T) {
	if os.Getenv("HEYA_CAST_LIVE_TEST") == "" {
		t.Skip("set HEYA_CAST_LIVE_TEST=1 to run the live receiver test (plays audio!)")
	}
	file := os.Getenv("HEYA_CAST_LIVE_FILE")
	if file == "" {
		t.Fatal("HEYA_CAST_LIVE_FILE must point at a local audio file")
	}
	wantDev := os.Getenv("HEYA_CAST_LIVE_DEVICE")
	if wantDev == "" {
		wantDev = "Anlæg"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := New(t.TempDir())
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("manager start: %v", err)
	}
	defer mgr.Stop()

	// Discovery: first browse window needs a few seconds on a quiet LAN.
	var dev Device
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for _, d := range mgr.Devices() {
			if d.Name == wantDev {
				dev = d
			}
		}
		if dev.ID != "" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if dev.ID == "" {
		t.Fatalf("device %q not discovered within 15s (found: %v)", wantDev, mgr.Devices())
	}
	t.Logf("discovered %s (%s %s) at %s:%d", dev.Name, dev.Manufacturer, dev.Model, dev.Addr, dev.Port)

	track := TrackInfo{
		TrackID: 0, // no DB in this test; scrobble sink is nil anyway
		Path:    file,
		Title:   "Heya cast live test",
		Artist:  "Heya",
		Album:   "internal/cast",
		// Duration unknown for arbitrary files; leave 0 (no clamp).
	}
	sess, err := mgr.Play(dev.ID, 1, track, 22)
	if err != nil {
		t.Fatalf("play: %v", err)
	}

	waitState := func(want SessionState, within time.Duration) {
		t.Helper()
		end := time.Now().Add(within)
		for time.Now().Before(end) {
			if sess.Snapshot().State == want {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
		t.Fatalf("session never reached %s (now %s)", want, sess.Snapshot().State)
	}

	// Commence: lead(7s) + establishment + margin.
	waitState(StatePlaying, 20*time.Second)
	t.Log("playing — letting it run 8s")
	time.Sleep(8 * time.Second)

	if err := sess.SetVolume(18); err != nil {
		t.Errorf("volume: %v", err)
	}

	// Pause = instant transport teardown with frozen position; resume =
	// respawn at that position (see Session.Pause for why not the FIFO).
	t.Log("pausing")
	if err := sess.Pause(); err != nil {
		t.Fatalf("pause: %v", err)
	}
	waitState(StatePaused, 5*time.Second)
	pausedAt := sess.Snapshot().PositionSec
	time.Sleep(3 * time.Second)
	if pos := sess.Snapshot().PositionSec; pos != pausedAt {
		t.Errorf("position moved while paused: %.1f → %.1f", pausedAt, pos)
	}
	t.Log("resuming")
	if err := sess.Resume(); err != nil {
		t.Fatalf("resume: %v", err)
	}
	waitState(StatePlaying, 20*time.Second)
	if pos := sess.Snapshot().PositionSec; pos < pausedAt-1 || pos > pausedAt+15 {
		t.Errorf("position after resume = %.1fs, want ≈%.1fs", pos, pausedAt)
	}

	t.Log("seeking to 30s")
	if err := sess.Seek(30); err != nil {
		t.Fatalf("seek: %v", err)
	}
	waitState(StatePlaying, 20*time.Second)
	if pos := sess.Snapshot().PositionSec; pos < 30 || pos > 45 {
		t.Errorf("position after seek = %.1fs, want ~30s", pos)
	}
	time.Sleep(5 * time.Second)

	if err := sess.Stop(); err != nil {
		t.Errorf("stop: %v", err)
	}
	if got := sess.Snapshot().State; got != StateStopped {
		t.Errorf("final state = %s, want stopped", got)
	}
}
