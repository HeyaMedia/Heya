package service

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/karbowiak/heya/internal/cast"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/rs/zerolog/log"
)

// system_settings keys for server-side casting. Same live-toggle
// semantics as Jellyfin/Subsonic: UI-editable unless env-locked.
const (
	castKeyEnabled = "cast.enabled"
	castKeyDevices = "cast.devices"
)

func (a *App) SaveCastSettings(ctx context.Context, enabled bool, devices string) error {
	cur := a.config.Cast
	if err := errIfEnvLockedChanged(castKeyEnabled, cur.Enabled, enabled); err != nil {
		return err
	}
	if err := errIfEnvLockedChanged(castKeyDevices, cur.Devices, devices); err != nil {
		return err
	}
	if err := persistFieldSetting(a, ctx, castKeyEnabled, cur.Enabled, enabled); err != nil {
		return err
	}
	if err := persistFieldSetting(a, ctx, castKeyDevices, cur.Devices, devices); err != nil {
		return err
	}
	if a.config.Cast.Enabled.Source != config.SourceEnv {
		a.config.Cast.Enabled = config.Field[bool]{Value: enabled, Source: config.SourceDB}
	}
	if a.config.Cast.Devices.Source != config.SourceEnv {
		a.config.Cast.Devices = config.Field[string]{Value: devices, Source: config.SourceDB}
	}
	// Flips take effect immediately: enable starts discovery, disable
	// tears down every active session (receivers see a clean TEARDOWN).
	// A device-list change while running restarts the manager so the
	// resolve loop picks it up — active sessions stop (rare admin action).
	devicesChanged := cur.Devices.Value != a.config.Cast.Devices.Value
	if a.castMgr != nil && (!a.CastEnabled() || devicesChanged) {
		a.castMgr.Stop()
	}
	if a.CastEnabled() {
		a.StartCast(ctx)
	}
	return nil
}

// LoadCastFromDB seeds the in-memory snapshot from system_settings at
// boot; env-sourced fields keep their env provenance.
func (a *App) LoadCastFromDB(ctx context.Context) {
	if a.db == nil {
		return
	}
	overlayFieldFromDB(a, ctx, &a.config.Cast.Enabled, castKeyEnabled, nil)
	overlayFieldFromDB(a, ctx, &a.config.Cast.Devices, castKeyDevices, nil)
}

func (a *App) CastEnabled() bool { return a.config.Cast.Enabled.Value }

// StartCast launches discovery when casting is enabled. Idempotent —
// called from serve at boot and again on settings flips.
func (a *App) StartCast(ctx context.Context) {
	if a.castMgr == nil || !a.CastEnabled() {
		return
	}
	a.castMgr.SetStaticDevices(splitCastDevices(a.config.Cast.Devices.Value))
	if err := a.castMgr.Start(a.lifetimeCtx); err != nil {
		// Extraction/spawn problems shouldn't kill the server; devices
		// simply won't appear and the API reports the empty list.
		log.Error().Err(err).Msg("cast: manager start failed")
	}
}

// splitCastDevices parses the HEYA_CAST_DEVICES comma list into clean
// addresses (IP or ip:port), dropping empties from trailing commas.
func splitCastDevices(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if addr := strings.TrimSpace(part); addr != "" {
			out = append(out, addr)
		}
	}
	return out
}

// Cast exposes the manager for handlers/CLI. Nil when the App was built
// without a data dir (spec-dump / humatest fixtures).
func (a *App) Cast() *cast.Manager { return a.castMgr }

// CastConfigView is the Settings payload: values + provenance so the UI
// can grey out env-locked fields.
type CastConfigView struct {
	Enabled       bool   `json:"enabled"`
	EnabledSource string `json:"enabled_source"`
	Devices       string `json:"devices"`
	DevicesSource string `json:"devices_source"`
}

func (a *App) CastConfig() CastConfigView {
	return CastConfigView{
		Enabled:       a.config.Cast.Enabled.Value,
		EnabledSource: string(a.config.Cast.Enabled.Source),
		Devices:       a.config.Cast.Devices.Value,
		DevicesSource: string(a.config.Cast.Devices.Source),
	}
}

// CastInterface is one of the server's own network legs — the debug page
// renders these against discovered devices so a subnet mismatch (the #1
// "no devices" cause: containers, VLANs) is visible at a glance.
type CastInterface struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

type CastNetworkStatus struct {
	Enabled    bool                      `json:"enabled"`
	Running    bool                      `json:"running"`
	Interfaces []CastInterface           `json:"interfaces"`
	Devices    []cast.Device             `json:"devices"`
	Static     []cast.StaticTargetStatus `json:"static"`
	Sessions   []cast.SessionSnapshot    `json:"sessions"`
}

// CastStatus assembles the Settings → Casting diagnostics view.
func (a *App) CastStatus() CastNetworkStatus {
	st := CastNetworkStatus{
		Enabled:    a.CastEnabled(),
		Interfaces: castLocalInterfaces(),
		Devices:    []cast.Device{},
		Static:     []cast.StaticTargetStatus{},
		Sessions:   []cast.SessionSnapshot{},
	}
	if a.castMgr != nil {
		st.Running = a.castMgr.Running()
		st.Devices = a.castMgr.Devices()
		st.Static = a.castMgr.StaticStatuses()
		st.Sessions = a.castMgr.Sessions()
	}
	return st
}

// castLocalInterfaces lists the server's up, non-loopback IPv4 legs.
// mDNS discovery can only hear receivers sharing an L2 with one of these.
func castLocalInterfaces() []CastInterface {
	out := []CastInterface{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipn, ok := addr.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			out = append(out, CastInterface{Name: iface.Name, Addr: ipn.String()})
		}
	}
	return out
}

// CastPlayTrack resolves a track to its primary file and starts (or
// retargets) a session on the device. The primary (highest
// quality_score) file feeds the PCM decoder — the receiver gets full
// quality regardless of what browsers would direct-play.
func (a *App) CastPlayTrack(ctx context.Context, userID int64, deviceID string, trackID int64, volume, startSeconds int) (cast.SessionSnapshot, error) {
	if a.castMgr == nil {
		return cast.SessionSnapshot{}, fmt.Errorf("casting unavailable")
	}
	if !a.CastEnabled() {
		return cast.SessionSnapshot{}, fmt.Errorf("casting is disabled")
	}
	track, err := a.castTrackInfo(ctx, trackID)
	if err != nil {
		return cast.SessionSnapshot{}, err
	}
	// Mid-track handoff: a client transferring local playback passes its
	// position. Same clamp as Session.Seek — never start past the end.
	if startSeconds > 0 {
		if track.Duration > 0 && startSeconds >= track.Duration {
			startSeconds = track.Duration - 1
		}
		track.StartAt = startSeconds
	}
	// Note: the request ctx bounds only the DB reads above. The session
	// itself runs on the cast manager's lifetime.
	s, err := a.castMgr.Play(deviceID, userID, track, volume)
	if err != nil {
		return cast.SessionSnapshot{}, err
	}
	return s.Snapshot(), nil
}

func (a *App) castTrackInfo(ctx context.Context, trackID int64) (cast.TrackInfo, error) {
	detail, err := a.GetMusicTrackDetail(ctx, trackID)
	if err != nil {
		return cast.TrackInfo{}, fmt.Errorf("track %d not found", trackID)
	}
	files := detail.Files
	if len(files) == 0 {
		return cast.TrackInfo{}, fmt.Errorf("track %d has no playable file", trackID)
	}
	lf, err := a.GetLibraryFile(ctx, files[0].LibraryFileID)
	if err != nil {
		return cast.TrackInfo{}, fmt.Errorf("track %d: library file missing", trackID)
	}
	if vfs.IsSMBPath(lf.Path) {
		// Same restriction as the AAC transcode path: the PCM feeder
		// needs a locally-readable file. Revisit with vfs streaming.
		return cast.TrackInfo{}, fmt.Errorf("casting from remote (SMB) sources is not supported yet")
	}
	return cast.TrackInfo{
		TrackID:  detail.ID,
		Path:     lf.Path,
		Title:    detail.Title,
		Artist:   detail.ArtistName,
		Album:    detail.AlbumTitle,
		Duration: int(detail.Duration),
	}, nil
}

// castPlaybackSink records cast listens through the same dispatch the
// HTTP playback endpoint uses, with a distinguishing source label.
func (a *App) castPlaybackSink(ctx context.Context, userID, trackID int64, positionSec, totalSec int, completed bool) {
	_ = a.RecordPlayback(ctx, userID, PlaybackEvent{
		EntityType:      "track",
		EntityID:        trackID,
		PositionSeconds: int32(positionSec), //nolint:gosec // bounded by track duration
		TotalSeconds:    int32(totalSec),    //nolint:gosec // bounded by track duration
		Completed:       completed,
		Source:          "cast",
	})
}
