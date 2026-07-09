package cast

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// scrobbleMinSeconds mirrors the FE threshold in usePlayer.ts — a track
// counts as listened after 30s of actual play time.
const scrobbleMinSeconds = 30

// Session is one server-owned playback session against one device. The
// server is the player: clients (web UI, CLI) only send commands here
// and mirror state from the WS events the session emits. Phase 1 plays
// a single track; the queue moves in here in Phase 3.
type Session struct {
	ID     string
	Device Device
	UserID int64

	mgr *Manager

	mu        sync.Mutex
	state     SessionState
	track     TrackInfo
	volume    int
	transport Transport

	// Position clock: track position = track.StartAt + playedBase
	// (+ time since resumedAt while playing). Server-derived — cliap2's
	// own position lines are log noise, not an API.
	playedBase time.Duration
	resumedAt  time.Time

	// listened accumulates actual play time across seeks/replacements
	// for the scrobble threshold.
	listened time.Duration

	retried bool // one respawn attempt per track on pre-commence failure
	closed  bool
}

func newSessionID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "cs-" + hex.EncodeToString(b[:])
}

// snapshot is the API/WS view of a session.
type SessionSnapshot struct {
	ID          string       `json:"id"`
	DeviceID    string       `json:"device_id"`
	DeviceName  string       `json:"device_name"`
	UserID      int64        `json:"user_id"`
	State       SessionState `json:"state"`
	TrackID     int64        `json:"track_id,omitempty"`
	Title       string       `json:"title,omitempty"`
	Artist      string       `json:"artist,omitempty"`
	Album       string       `json:"album,omitempty"`
	DurationSec int          `json:"duration_sec,omitempty"`
	PositionSec float64      `json:"position_sec"`
	Volume      int          `json:"volume"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

func (s *Session) Snapshot() SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SessionSnapshot{
		ID:          s.ID,
		DeviceID:    s.Device.ID,
		DeviceName:  s.Device.Name,
		UserID:      s.UserID,
		State:       s.state,
		TrackID:     s.track.TrackID,
		Title:       s.track.Title,
		Artist:      s.track.Artist,
		Album:       s.track.Album,
		DurationSec: s.track.Duration,
		PositionSec: s.positionLocked().Seconds(),
		Volume:      s.volume,
		UpdatedAt:   time.Now(),
	}
}

func (s *Session) positionLocked() time.Duration {
	pos := time.Duration(s.track.StartAt)*time.Second + s.playedBase
	if s.state == StatePlaying && !s.resumedAt.IsZero() {
		pos += time.Since(s.resumedAt)
	}
	if max := time.Duration(s.track.Duration) * time.Second; s.track.Duration > 0 && pos > max {
		pos = max
	}
	return pos
}

// start spawns the first transport for the session's track.
func (s *Session) start() error {
	s.mu.Lock()
	track, volume := s.track, s.volume
	s.mu.Unlock()
	return s.spawnTransport(track, volume)
}

// spawnTransport binds the sender processes to the manager's lifetime
// context — never a request context, which would SIGTERM playback the
// moment the HTTP call that started it returns.
func (s *Session) spawnTransport(track TrackInfo, volume int) error {
	ctx, err := s.mgr.transportCtx()
	if err != nil {
		return err
	}
	tr, err := s.mgr.providerFor(s.Device).NewTransport(s.Device)
	if err != nil {
		return err
	}
	if err := tr.Start(ctx, track, volume); err != nil {
		return err
	}
	s.mu.Lock()
	s.transport = tr
	s.track = track
	s.state = StateStarting
	s.playedBase = 0
	s.resumedAt = time.Time{}
	s.mu.Unlock()
	s.mgr.emitSession(s)
	go s.consume(tr) //nolint:gosec // session outlives any request; retry path deliberately uses Background
	return nil
}

// consume applies one transport's events to the session. The pointer
// guard makes stale loops (from a replaced transport) harmless: they
// drain their closed channel without touching state.
func (s *Session) consume(tr Transport) {
	for ev := range tr.Events() {
		s.mu.Lock()
		if s.transport != tr {
			s.mu.Unlock()
			continue
		}
		switch ev.Kind {
		case TransportConnected:
			// still buffering; no state edge worth broadcasting
			s.mu.Unlock()
			continue
		case TransportPlaying, TransportResumed:
			s.state = StatePlaying
			s.resumedAt = time.Now()
		case TransportPaused:
			s.accumulateLocked()
			s.state = StatePaused
		case TransportEnded:
			s.accumulateLocked()
			s.state = StateStopped
			s.mu.Unlock()
			s.recordPlayback(true)
			s.mgr.emitSession(s)
			s.mgr.removeSession(s)
			return
		case TransportFailed:
			retry := !s.retried && s.playedBase == 0 && s.state == StateStarting && !s.closed
			s.retried = true
			track, volume := s.track, s.volume
			s.mu.Unlock()
			if retry {
				log.Warn().Err(ev.Err).Str("device", s.Device.Name).Msg("cast: transport failed pre-commence, retrying once")
				time.Sleep(time.Second)
				if err := s.spawnTransport(track, volume); err == nil {
					return
				}
			}
			log.Error().Err(ev.Err).Str("device", s.Device.Name).Msg("cast: session failed")
			s.mu.Lock()
			s.state = StateFailed
			s.mu.Unlock()
			s.mgr.emitSession(s)
			s.mgr.removeSession(s)
			return
		}
		s.mu.Unlock()
		s.mgr.emitSession(s)
	}
}

// accumulateLocked folds the running play interval into playedBase and
// the scrobble counter. Caller holds s.mu.
func (s *Session) accumulateLocked() {
	if s.state == StatePlaying && !s.resumedAt.IsZero() {
		d := time.Since(s.resumedAt)
		s.playedBase += d
		s.listened += d
		s.resumedAt = time.Time{}
	}
}

func (s *Session) recordPlayback(completed bool) {
	s.mu.Lock()
	listened := s.listened
	track := s.track
	pos := s.positionLocked()
	s.listened = 0
	s.mu.Unlock()
	if s.mgr.playbackSink == nil || track.TrackID == 0 {
		return
	}
	if !completed && listened < scrobbleMinSeconds*time.Second {
		return
	}
	s.mgr.playbackSink(context.Background(), s.UserID, track.TrackID, int(pos.Seconds()), track.Duration, completed)
}

// Pause freezes the position and tears the transport down. Not the
// transport's FIFO ACTION=PAUSE: cliap2's pause only stops *intake*, so
// the ~4-5s of pre-roll-primed audio keeps playing after the command —
// a stop is instantly silent and Resume respawns at the frozen position
// (the same live-validated path as Seek). Pause latency traded for
// resume latency (~2-3s session re-establishment), which is the
// predictable half of that trade.
func (s *Session) Pause() error {
	s.mu.Lock()
	if s.state != StatePlaying || s.transport == nil {
		s.mu.Unlock()
		return fmt.Errorf("cast: session is not playing")
	}
	s.accumulateLocked()
	s.state = StatePaused
	old := s.transport
	s.transport = nil
	s.mu.Unlock()

	err := old.Stop()
	s.mgr.emitSession(s)
	return err
}

// Resume respawns the transport at the paused position.
func (s *Session) Resume() error {
	s.mu.Lock()
	if s.state != StatePaused {
		s.mu.Unlock()
		return fmt.Errorf("cast: session is not paused")
	}
	track := s.track
	track.StartAt = int(s.positionLocked().Seconds())
	volume := s.volume
	s.retried = false
	s.mu.Unlock()
	return s.spawnTransport(track, volume)
}

// SetVolume applies live when a transport is up; while paused it just
// records the level for the resume respawn.
func (s *Session) SetVolume(level int) error {
	s.mu.Lock()
	s.volume = clampVolume(level)
	tr := s.transport
	s.mu.Unlock()
	if tr != nil {
		if err := tr.SetVolume(level); err != nil {
			return err
		}
	}
	s.mgr.emitSession(s)
	return nil
}

// Seek replaces the transport with the same track at a new offset — the
// live-validated seek path (in-band FLUSH+refeed is a future
// refinement). While paused it only moves the frozen position.
func (s *Session) Seek(seconds int) error {
	s.mu.Lock()
	track := s.track
	volume := s.volume
	old := s.transport
	paused := s.state == StatePaused
	s.accumulateLocked()
	s.retried = false

	if seconds < 0 {
		seconds = 0
	}
	if track.Duration > 0 && seconds >= track.Duration {
		seconds = track.Duration - 1
	}
	track.StartAt = seconds

	if paused {
		s.track = track
		s.playedBase = 0
		s.mu.Unlock()
		s.mgr.emitSession(s)
		return nil
	}
	s.mu.Unlock()

	if old != nil {
		_ = old.Stop()
	}
	return s.spawnTransport(track, volume)
}

// PlayTrack switches the session to a new track (records the previous
// one first if it crossed the scrobble threshold).
func (s *Session) PlayTrack(track TrackInfo) error {
	s.mu.Lock()
	old := s.transport
	s.accumulateLocked()
	s.mu.Unlock()
	s.recordPlayback(false)

	if old != nil {
		_ = old.Stop()
	}
	s.mu.Lock()
	s.retried = false
	s.mu.Unlock()
	return s.spawnTransport(track, s.volume)
}

// Stop ends the session: scrobble-if-earned, graceful transport
// teardown, removal from the manager.
func (s *Session) Stop() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.accumulateLocked()
	s.state = StateStopped
	tr := s.transport
	s.mu.Unlock()

	s.recordPlayback(false)
	var err error
	if tr != nil {
		err = tr.Stop()
	}
	s.mgr.emitSession(s)
	s.mgr.removeSession(s)
	return err
}
