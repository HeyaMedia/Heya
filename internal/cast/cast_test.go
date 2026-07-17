package cast

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/eventhub"
)

type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }
func (fakeProvider) Browse(ctx context.Context, _ func(Device)) error {
	<-ctx.Done()
	return ctx.Err()
}
func (fakeProvider) NewTransport(Device) (Transport, error) {
	return &fakeTransport{events: make(chan TransportEvent)}, nil
}

type fakeTransport struct {
	events chan TransportEvent
	once   sync.Once
}

func (t *fakeTransport) Start(context.Context, TrackInfo, int) error { return nil }
func (t *fakeTransport) Pause() error                                { return nil }
func (t *fakeTransport) Resume() error                               { return nil }
func (t *fakeTransport) SetVolume(int) error                         { return nil }
func (t *fakeTransport) Stop() error {
	t.once.Do(func() { close(t.events) })
	return nil
}
func (t *fakeTransport) Events() <-chan TransportEvent { return t.events }

type fakeNativeTransport struct {
	fakeTransport
	pauses  int
	resumes int
	seeks   []int
}

func (t *fakeNativeTransport) Pause() error {
	t.pauses++
	return nil
}
func (t *fakeNativeTransport) Resume() error {
	t.resumes++
	return nil
}
func (t *fakeNativeTransport) Seek(seconds int) error {
	t.seeks = append(t.seeks, seconds)
	return nil
}

func newFakeManager(t *testing.T, devices ...Device) *Manager {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	m := New(t.TempDir())
	m.started = true
	m.runCtx = ctx
	m.cancel = cancel
	m.providers["fake"] = fakeProvider{}
	for _, device := range devices {
		m.devices[device.ID] = device
	}
	t.Cleanup(m.Stop)
	return m
}

// The Settings toggle disables and re-enables casting live. A re-enable
// must fully rebuild the manager: a Stop that leaves `started` set (or a
// canceled runCtx behind) makes every later transport spawn inherit a
// dead context and fail — the exact regression this test pins.
func TestManagerStopStartCycle(t *testing.T) {
	m := New(t.TempDir())

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("first start: %v", err)
	}
	ctx1, err := m.transportCtx()
	if err != nil {
		t.Fatalf("transportCtx after start: %v", err)
	}
	if ctx1.Err() != nil {
		t.Fatal("fresh runCtx is already canceled")
	}

	m.Stop()
	if _, err := m.transportCtx(); err == nil {
		t.Fatal("transportCtx should error while stopped")
	}
	if _, err := m.Play("airplay:nope", 1, TrackInfo{}, 30); err == nil {
		t.Fatal("Play should error while stopped")
	}

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("restart: %v", err)
	}
	ctx2, err := m.transportCtx()
	if err != nil {
		t.Fatalf("transportCtx after restart: %v", err)
	}
	if ctx2.Err() != nil {
		t.Fatal("runCtx after restart is canceled — Stop did not reset the lifecycle")
	}

	m.Stop()
}

// A transport retry (or in-flight seek) that lost the race against
// Session.Stop must not respawn — across a disable→enable cycle the new
// runCtx would otherwise host a ghost transport with no registry entry.
func TestStoppedSessionCannotRespawn(t *testing.T) {
	m := New(t.TempDir())
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer m.Stop()

	s := &Session{
		ID:     newSessionID(),
		Device: Device{ID: "airplay:te:st", Provider: "airplay", Name: "test"},
		mgr:    m,
		state:  StateStarting,
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := s.spawnTransport(TrackInfo{Path: "/dev/null"}, 30); err == nil {
		t.Fatal("spawnTransport succeeded on a stopped session")
	}
	if err := s.Resume(); err == nil {
		t.Fatal("Resume succeeded on a stopped session")
	}
}

func TestManagerSupportsIndependentUsersOnDifferentDevices(t *testing.T) {
	m := newFakeManager(t,
		Device{ID: "fake:kitchen", Provider: "fake", Name: "Kitchen"},
		Device{ID: "fake:office", Provider: "fake", Name: "Office"},
	)

	first, err := m.Play("fake:kitchen", 11, TrackInfo{TrackID: 101, Path: "/dev/null"}, 25)
	if err != nil {
		t.Fatalf("first user play: %v", err)
	}
	second, err := m.Play("fake:office", 22, TrackInfo{TrackID: 202, Path: "/dev/null"}, 30)
	if err != nil {
		t.Fatalf("second user play: %v", err)
	}
	if first.ID == second.ID {
		t.Fatal("different receivers unexpectedly shared one session")
	}
	if got := len(m.Sessions()); got != 2 {
		t.Fatalf("active sessions = %d, want 2", got)
	}
	if got := m.SessionsForUser(11); len(got) != 1 || got[0].DeviceID != "fake:kitchen" {
		t.Fatalf("user 11 sessions = %#v", got)
	}
	if got := m.SessionsForUser(22); len(got) != 1 || got[0].DeviceID != "fake:office" {
		t.Fatalf("user 22 sessions = %#v", got)
	}
}

func TestManagerRejectsCrossUserTakeoverOfSameDevice(t *testing.T) {
	m := newFakeManager(t, Device{ID: "fake:lounge", Provider: "fake", Name: "Lounge"})
	first, err := m.Play("fake:lounge", 11, TrackInfo{TrackID: 101, Path: "/dev/null"}, 25)
	if err != nil {
		t.Fatalf("first user play: %v", err)
	}

	_, err = m.Play("fake:lounge", 22, TrackInfo{TrackID: 202, Path: "/dev/null"}, 30)
	if !errors.Is(err, ErrDeviceInUse) {
		t.Fatalf("cross-user takeover error = %v, want ErrDeviceInUse", err)
	}
	if got := first.Snapshot().TrackID; got != 101 {
		t.Fatalf("existing session was retargeted to track %d", got)
	}
}

func TestManagerRejectsSecondProtocolOnSamePhysicalReceiver(t *testing.T) {
	m := newFakeManager(t,
		Device{ID: "fake:airplay", Provider: "fake", Name: "Living Room AirPlay", Addr: "192.168.20.50"},
		Device{ID: "fake:cast", Provider: "fake", Name: "Living Room Chromecast", Addr: "192.168.20.50"},
	)
	if _, err := m.Play("fake:airplay", 11, TrackInfo{TrackID: 101, Path: "/dev/null"}, 25); err != nil {
		t.Fatalf("first protocol play: %v", err)
	}
	if _, err := m.Play("fake:cast", 22, TrackInfo{TrackID: 202, Path: "/dev/null"}, 30); !errors.Is(err, ErrDeviceInUse) {
		t.Fatalf("second protocol error = %v, want ErrDeviceInUse", err)
	}
}

func TestSessionEventsOnlyReachOwner(t *testing.T) {
	hub := eventhub.New()
	owner := hub.SubscribeUser(11)
	other := hub.SubscribeUser(22)
	t.Cleanup(func() {
		hub.Unsubscribe(owner)
		hub.Unsubscribe(other)
	})

	m := New(t.TempDir())
	m.SetHub(hub)
	s := &Session{
		ID:     "cs-private",
		Device: Device{ID: "fake:lounge", Name: "Lounge"},
		UserID: 11,
		mgr:    m,
		state:  StatePlaying,
		track:  TrackInfo{TrackID: 101},
	}
	m.emitSession(s)

	select {
	case ev := <-owner:
		if ev.Type != eventhub.EventCastState {
			t.Fatalf("owner event type = %q", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("owner did not receive cast event")
	}
	select {
	case ev := <-other:
		t.Fatalf("other user received private cast event: %#v", ev)
	default:
	}
}

func TestSessionUsesNativePullControlsWithoutRespawn(t *testing.T) {
	m := New(t.TempDir())
	tr := &fakeNativeTransport{fakeTransport: fakeTransport{events: make(chan TransportEvent)}}
	s := &Session{
		ID:        "cs-native",
		Device:    Device{ID: "chromecast:test", Provider: "chromecast", Name: "Test Cast"},
		UserID:    11,
		mgr:       m,
		state:     StatePlaying,
		track:     TrackInfo{TrackID: 101, Duration: 180},
		transport: tr,
		resumedAt: time.Now().Add(-2 * time.Second),
	}
	if err := s.Pause(); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if tr.pauses != 1 || s.transport != tr || s.Snapshot().State != StatePaused {
		t.Fatalf("native pause did not preserve transport: pauses=%d state=%s", tr.pauses, s.Snapshot().State)
	}
	if err := s.Resume(); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if tr.resumes != 1 || s.transport != tr || s.Snapshot().State != StatePlaying {
		t.Fatalf("native resume did not preserve transport: resumes=%d state=%s", tr.resumes, s.Snapshot().State)
	}
	if err := s.Seek(75); err != nil {
		t.Fatalf("seek: %v", err)
	}
	if len(tr.seeks) != 1 || tr.seeks[0] != 75 || s.transport != tr {
		t.Fatalf("native seek = %v, transport preserved=%v", tr.seeks, s.transport == tr)
	}
	if got := s.Snapshot().PositionSec; got < 75 || got > 76 {
		t.Fatalf("position after native seek = %.3f", got)
	}
}

func TestAudioSessionReportsStartOnceAndNaturalCompletion(t *testing.T) {
	m := New(t.TempDir())
	var completed []bool
	m.SetPlaybackSink(func(_ context.Context, _ int64, _ TrackInfo, _, _ int, done bool) {
		completed = append(completed, done)
	})
	tr := &fakeTransport{events: make(chan TransportEvent, 4)}
	s := &Session{
		ID:        "cs-audio-lifecycle",
		Device:    Device{ID: "fake:speaker", Name: "Speaker"},
		UserID:    11,
		mgr:       m,
		state:     StateStarting,
		track:     TrackInfo{TrackID: 101, MediaKind: "audio", Duration: 180},
		transport: tr,
	}
	done := make(chan struct{})
	go func() {
		s.consume(tr)
		close(done)
	}()

	tr.events <- TransportEvent{Kind: TransportPlaying}
	tr.events <- TransportEvent{Kind: TransportPaused}
	tr.events <- TransportEvent{Kind: TransportResumed}
	tr.events <- TransportEvent{Kind: TransportEnded}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session did not consume natural end")
	}
	if len(completed) != 2 || completed[0] || !completed[1] {
		t.Fatalf("playback lifecycle = %v, want [start completion]", completed)
	}
}

func TestAudioSessionStopDoesNotCreateHistory(t *testing.T) {
	m := New(t.TempDir())
	called := false
	m.SetPlaybackSink(func(context.Context, int64, TrackInfo, int, int, bool) { called = true })
	tr := &fakeTransport{events: make(chan TransportEvent)}
	s := &Session{
		ID:              "cs-audio-stop",
		UserID:          11,
		mgr:             m,
		state:           StatePlaying,
		track:           TrackInfo{TrackID: 101, MediaKind: "audio", Duration: 180},
		transport:       tr,
		resumedAt:       time.Now(),
		playbackStarted: true,
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if called {
		t.Fatal("manual audio stop submitted a playback event")
	}
}

func TestVideoSessionSnapshotAndProgressSink(t *testing.T) {
	m := New(t.TempDir())
	var gotItem TrackInfo
	var gotPosition int
	var gotCompleted bool
	m.SetPlaybackSink(func(_ context.Context, userID int64, item TrackInfo, positionSec, totalSec int, completed bool) {
		if userID != 11 || totalSec != 120 {
			t.Fatalf("sink identity = user %d total %d", userID, totalSec)
		}
		gotItem = item
		gotPosition = positionSec
		gotCompleted = completed
	})
	s := &Session{
		ID:     "cs-video",
		Device: Device{ID: "chromecast:tv", Name: "TV"},
		UserID: 11,
		mgr:    m,
		state:  StatePlaying,
		track: TrackInfo{
			FileID: "file-public-id", MediaKind: "video",
			MediaItemID: 44, EntityType: "episode", EntityID: 77,
			Title: "Episode", Duration: 120, AudioTrack: 1, Quality: "1080p",
			TextTrack: &TextTrackInfo{SelectionIndex: 2, StreamIndex: 7},
		},
		resumedAt: time.Now().Add(-3 * time.Second),
	}
	snap := s.Snapshot()
	if snap.MediaKind != "video" || snap.FileID != "file-public-id" || snap.EntityType != "episode" || snap.EntityID != 77 {
		t.Fatalf("video snapshot = %#v", snap)
	}
	if snap.MediaItemID != 44 || snap.AudioTrack != 1 || snap.SubtitleTrack == nil || *snap.SubtitleTrack != 2 || snap.Quality != "1080p" {
		t.Fatalf("video control state = %#v", snap)
	}
	s.recordPlayback(false)
	if gotItem.EntityID != 77 || gotPosition < 2 || gotPosition > 4 || gotCompleted {
		t.Fatalf("progress sink = item %#v position %d completed=%v", gotItem, gotPosition, gotCompleted)
	}
	s.recordPlayback(true)
	if gotPosition != 120 || !gotCompleted {
		t.Fatalf("completed sink = position %d completed=%v", gotPosition, gotCompleted)
	}
}
