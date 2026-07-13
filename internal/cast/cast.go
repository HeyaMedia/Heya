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
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/rs/zerolog/log"
)

// PlaybackSink lets the service layer record music listens and video watch
// progress without this package importing service. positionSec is the item
// position when recording; completed marks a natural media end.
type PlaybackSink func(ctx context.Context, userID int64, item TrackInfo, positionSec, totalSec int, completed bool)

// ErrDeviceInUse means another user already owns the one transport a physical
// receiver can accept. Other receivers remain independent and can be used by
// other users concurrently.
var ErrDeviceInUse = errors.New("cast device is already in use by another user")

type Manager struct {
	dataDir string
	hub     *eventhub.Hub // nil in contexts without a live WS hub (CLI)

	playbackSink PlaybackSink

	mediaBaseURL  string
	mediaPort     string
	mediaTokenKey []byte

	// staticAddrs are receiver addresses resolved by unicast mDNS instead
	// of multicast browse — deployments where multicast can't reach us
	// (containers, multicast-filtered networks). Set before Start.
	staticAddrs  []string
	staticStatus map[string]StaticTargetStatus

	mu        sync.RWMutex
	providers map[string]Provider
	devices   map[string]Device
	sessions  map[string]*Session // keyed by physical endpoint — one session per receiver
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
		dataDir:       dataDir,
		mediaTokenKey: newMediaTokenKey(),
		providers:     map[string]Provider{},
		devices:       map[string]Device{},
		sessions:      map[string]*Session{},
		byID:          map[string]*Session{},
	}
}

func (m *Manager) SetHub(hub *eventhub.Hub)        { m.hub = hub }
func (m *Manager) SetPlaybackSink(fn PlaybackSink) { m.playbackSink = fn }

// SetStaticDevices installs the unicast-resolved receiver list (comma
// list from HEYA_CAST_DEVICES, already split). Takes effect on Start.
func (m *Manager) SetStaticDevices(addrs []string) {
	m.mu.Lock()
	m.staticAddrs = addrs
	m.staticStatus = map[string]StaticTargetStatus{}
	m.mu.Unlock()
}

func (m *Manager) setStaticStatus(s StaticTargetStatus) {
	m.mu.Lock()
	if m.staticStatus == nil {
		m.staticStatus = map[string]StaticTargetStatus{}
	}
	m.staticStatus[s.Addr] = s
	m.mu.Unlock()
}

// StaticStatuses reports the last resolve outcome per configured address,
// in config order (untried targets appear with a zero CheckedAt).
func (m *Manager) StaticStatuses() []StaticTargetStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]StaticTargetStatus, 0, len(m.staticAddrs))
	for _, addr := range m.staticAddrs {
		if s, ok := m.staticStatus[addr]; ok {
			out = append(out, s)
		} else {
			out = append(out, StaticTargetStatus{Addr: addr})
		}
	}
	return out
}

// Running reports whether discovery is live (started and not stopped).
func (m *Manager) Running() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

// Start extracts helper binaries, registers providers, and launches the
// discovery loops. Idempotent; ctx should be the app lifetime.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	binPath, airplayErr := ensureCliap2(filepath.Join(m.dataDir, "cast", "bin"))
	browseCtx, cancel := context.WithCancel(ctx)
	m.runCtx = browseCtx
	m.cancel = cancel
	if airplayErr == nil {
		m.providers["airplay"] = &airplayProvider{binPath: binPath}
	} else {
		log.Warn().Err(airplayErr).Msg("cast: AirPlay provider unavailable; continuing with URL-pull providers")
	}
	m.providers["chromecast"] = &chromecastProvider{}
	m.started = true
	providers := make([]Provider, 0, len(m.providers))
	for _, p := range m.providers {
		providers = append(providers, p)
	}
	staticAddrs := m.staticAddrs
	m.mu.Unlock()

	if len(staticAddrs) > 0 {
		go m.resolveStaticLoop(browseCtx, staticAddrs)
	}

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
	out := make([]Device, 0, len(m.devices))
	for _, d := range m.devices {
		out = append(out, d)
	}
	m.mu.RUnlock()
	for i := range out {
		if origin, err := m.mediaOriginFor(out[i]); err == nil {
			out[i].MediaOrigin = origin
		}
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
	if track.PullPath != "" {
		mediaURL, err := m.mediaURLFor(dev, userID, track)
		if err != nil {
			return nil, err
		}
		track.URL = mediaURL
	}

	endpointKey := receiverSessionKey(dev)
	m.mu.Lock()
	if existing, ok := m.sessions[endpointKey]; ok {
		m.mu.Unlock()
		if existing.UserID != userID || existing.Device.ID != dev.ID {
			return nil, fmt.Errorf("%w: %s", ErrDeviceInUse, dev.Name)
		}
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
	m.sessions[endpointKey] = s
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

// SessionsForUser is the non-admin API view. Receiver diagnostics use
// Sessions(), but normal clients must never see another user's playback.
func (m *Manager) SessionsForUser(userID int64) []SessionSnapshot {
	all := m.Sessions()
	out := make([]SessionSnapshot, 0, len(all))
	for _, snap := range all {
		if snap.UserID == userID {
			out = append(out, snap)
		}
	}
	return out
}

func (m *Manager) removeSession(s *Session) {
	m.mu.Lock()
	endpointKey := receiverSessionKey(s.Device)
	if m.sessions[endpointKey] == s {
		delete(m.sessions, endpointKey)
	}
	delete(m.byID, s.ID)
	m.mu.Unlock()
}

// receiverSessionKey collapses duplicate protocol advertisements for the same
// physical network endpoint. Without it, an AirPlay+Cast/DLNA receiver could be
// controlled concurrently through two provider IDs. Future multi-zone devices
// should expose an explicit zone endpoint key instead of sharing one Device.
func receiverSessionKey(dev Device) string {
	if ip := net.ParseIP(dev.Addr); ip != nil {
		return "ip:" + ip.String()
	}
	if dev.Addr != "" {
		return "host:" + strings.ToLower(dev.Addr)
	}
	return "id:" + dev.ID
}

// emitSession delivers session state only to the owning user's clients.
// Network receiver state contains listening activity and control handles;
// broadcasting it household-wide would bypass the casting allowlist.
func (m *Manager) emitSession(s *Session) {
	if m.hub == nil {
		return
	}
	snap := s.Snapshot()
	m.hub.EmitToUser(snap.UserID, eventhub.EventCastState, eventhub.CastStatePayload{
		SessionID:   snap.ID,
		DeviceID:    snap.DeviceID,
		DeviceName:  snap.DeviceName,
		UserID:      snap.UserID,
		State:       string(snap.State),
		MediaKind:   snap.MediaKind,
		TrackID:     snap.TrackID,
		FileID:      snap.FileID,
		EntityType:  snap.EntityType,
		EntityID:    snap.EntityID,
		Title:       snap.Title,
		Artist:      snap.Artist,
		PositionSec: snap.PositionSec,
		DurationSec: snap.DurationSec,
		Volume:      snap.Volume,
		At:          time.Now(),
	})
}
