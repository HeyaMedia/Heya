package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/karbowiak/heya/internal/cast"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/rs/zerolog/log"
)

// system_settings key for server-side casting. Same live-toggle
// semantics as Jellyfin/Subsonic: UI-editable unless env-locked.
const castKeyEnabled = "cast.enabled"

func (a *App) SaveCastSettings(ctx context.Context, enabled bool) error {
	cur := a.config.Cast
	if err := errIfEnvLockedChanged(castKeyEnabled, cur.Enabled, enabled); err != nil {
		return err
	}
	if err := persistFieldSetting(a, ctx, castKeyEnabled, cur.Enabled, enabled); err != nil {
		return err
	}
	if a.config.Cast.Enabled.Source != config.SourceEnv {
		a.config.Cast.Enabled = config.Field[bool]{Value: enabled, Source: config.SourceDB}
	}
	// Flips take effect immediately: enable starts discovery, disable
	// tears down every active session (receivers see a clean TEARDOWN).
	if enabled {
		a.StartCast(ctx)
	} else if a.castMgr != nil {
		a.castMgr.Stop()
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
