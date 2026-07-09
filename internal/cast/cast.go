// Package cast implements server-side playback to network receivers:
// Heya discovers devices, streams audio to them, and owns the playback
// session; clients (web UI, CLI) only send control commands and mirror
// session state over the WS event bus.
//
// Layout: cast.go is the entrypoint (Manager: providers, device cache,
// session registry). Each protocol/vendor lives in its own file behind
// the Provider/Transport interfaces — airplay.go today; yamaha.go,
// sony.go, nad.go, castv2.go etc. slot in beside it later.
//
// Research, live-validated invocation recipes, and failure modes:
// docs/casting-research.md. Build plan: docs/cast-plan.md.
package cast

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/rs/zerolog/log"
)

// PlaybackSink lets the service layer record listens (play_events /
// scrobbles) without this package importing service. positionSec is the
// track position when recording; completed marks a natural track end.
type PlaybackSink func(ctx context.Context, userID, trackID int64, positionSec, totalSec int, completed bool)

type Manager struct {
	dataDir string
	hub     *eventhub.Hub // nil in contexts without a live WS hub (CLI)

	playbackSink PlaybackSink

	mu        sync.RWMutex
	providers map[string]Provider
	devices   map[string]Device
	sessions  map[string]*Session // keyed by Device.ID — one session per device
	byID      map[string]*Session
	// runCtx is the manager-lifetime context every transport process is
	// bound to. Sessions must survive the HTTP request that created
	// them — request contexts stop at the service boundary and never
	// reach a transport.
	runCtx  context.Context
	cancel  context.CancelFunc
	started bool
}

func New(dataDir string) *Manager {
	return &Manager{
		dataDir:   dataDir,
		providers: map[string]Provider{},
		devices:   map[string]Device{},
		sessions:  map[string]*Session{},
		byID:      map[string]*Session{},
	}
}

func (m *Manager) SetHub(hub *eventhub.Hub)        { m.hub = hub }
func (m *Manager) SetPlaybackSink(fn PlaybackSink) { m.playbackSink = fn }

// Start extracts helper binaries, registers providers, and launches the
// discovery loops. Idempotent; ctx should be the app lifetime.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	binPath, err := ensureCliap2(filepath.Join(m.dataDir, "cast", "bin"))
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("cast: extracting cliap2: %w", err)
	}
	browseCtx, cancel := context.WithCancel(ctx)
	m.runCtx = browseCtx
	m.cancel = cancel
	m.providers["airplay"] = &airplayProvider{binPath: binPath}
	m.started = true
	providers := make([]Provider, 0, len(m.providers))
	for _, p := range m.providers {
		providers = append(providers, p)
	}
	m.mu.Unlock()

	for _, p := range providers {
		go func(p Provider) {
			if err := p.Browse(browseCtx, m.upsertDevice); err != nil && browseCtx.Err() == nil {
				log.Error().Err(err).Str("provider", p.Name()).Msg("cast: discovery loop exited")
			}
		}(p)
	}
	log.Info().Msg("cast: manager started, browsing for devices")
	return nil
}

// Stop tears down every active session (graceful — receivers must see
// TEARDOWN) and halts discovery. Fully resets the started state so a
// later Start() rebuilds a live runCtx — the Settings toggle disables
// and re-enables casting without a restart, and a re-enable must not
// hand transports a canceled context.
func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.runCtx = nil
	m.started = false
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	for _, s := range sessions {
		_ = s.Stop()
	}
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) upsertDevice(dev Device) {
	m.mu.Lock()
	m.devices[dev.ID] = dev
	m.mu.Unlock()
}

// Devices returns everything seen since boot, freshest name-sorted.
// mDNS absence is weak evidence of device death, so nothing is purged;
// LastSeen lets callers grey out stale entries if they care.
func (m *Manager) Devices() []Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Device, 0, len(m.devices))
	for _, d := range m.devices {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (m *Manager) Device(id string) (Device, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.devices[id]
	return d, ok
}

func (m *Manager) providerFor(dev Device) Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.providers[dev.Provider]
}

// transportCtx snapshots the lifetime context transports bind to.
// Errors when the manager is stopped (or was never started) so a spawn
// racing a disable gets a clean failure instead of a canceled/nil ctx.
func (m *Manager) transportCtx() (context.Context, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.started || m.runCtx == nil {
		return nil, fmt.Errorf("cast: manager not started")
	}
	return m.runCtx, nil
}

// Play starts (or retargets) the session on a device. One session per
// device: an existing session switches tracks in place, preserving its
// volume; a new one starts at the given volume. Deliberately takes no
// context — the session runs on the manager's lifetime, not the
// caller's (an HTTP request ending must not kill playback).
func (m *Manager) Play(deviceID string, userID int64, track TrackInfo, volume int) (*Session, error) {
	m.mu.RLock()
	started := m.started
	m.mu.RUnlock()
	if !started {
		return nil, fmt.Errorf("cast: manager not started")
	}
	dev, ok := m.Device(deviceID)
	if !ok {
		return nil, fmt.Errorf("cast: unknown device %q", deviceID)
	}
	if m.providerFor(dev) == nil {
		return nil, fmt.Errorf("cast: no provider for device %q", deviceID)
	}

	m.mu.Lock()
	if existing, ok := m.sessions[dev.ID]; ok {
		m.mu.Unlock()
		return existing, existing.PlayTrack(track)
	}
	s := &Session{
		ID:     newSessionID(),
		Device: dev,
		UserID: userID,
		mgr:    m,
		state:  StateStarting,
		track:  track,
		volume: clampVolume(volume),
	}
	m.sessions[dev.ID] = s
	m.byID[s.ID] = s
	m.mu.Unlock()

	if err := s.start(); err != nil {
		m.removeSession(s)
		return nil, err
	}
	return s, nil
}

func (m *Manager) Session(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.byID[id]
	return s, ok
}

func (m *Manager) Sessions() []SessionSnapshot {
	m.mu.RLock()
	list := make([]*Session, 0, len(m.byID))
	for _, s := range m.byID {
		list = append(list, s)
	}
	m.mu.RUnlock()
	out := make([]SessionSnapshot, 0, len(list))
	for _, s := range list {
		out = append(out, s.Snapshot())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceName < out[j].DeviceName })
	return out
}

func (m *Manager) removeSession(s *Session) {
	m.mu.Lock()
	if m.sessions[s.Device.ID] == s {
		delete(m.sessions, s.Device.ID)
	}
	delete(m.byID, s.ID)
	m.mu.Unlock()
}

// emitSession broadcasts the session snapshot on the WS bus. Global (not
// per-user) on purpose: cast targets are household devices and every
// client should render the same "playing on Anlæg" state.
func (m *Manager) emitSession(s *Session) {
	if m.hub == nil {
		return
	}
	snap := s.Snapshot()
	m.hub.Emit(eventhub.EventCastState, eventhub.CastStatePayload{
		SessionID:   snap.ID,
		DeviceID:    snap.DeviceID,
		DeviceName:  snap.DeviceName,
		UserID:      snap.UserID,
		State:       string(snap.State),
		TrackID:     snap.TrackID,
		Title:       snap.Title,
		Artist:      snap.Artist,
		PositionSec: snap.PositionSec,
		DurationSec: snap.DurationSec,
		Volume:      snap.Volume,
		At:          time.Now(),
	})
}
