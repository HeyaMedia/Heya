package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/cast"
	"github.com/karbowiak/heya/internal/service"
)

// registerCastRoutes mounts server-side casting: device discovery plus
// the playback sessions that stream to network receivers. Admins always have
// access; regular users must be explicitly allowed in Settings → Casting.
// Sessions and their live events are private to the user that started them.
func registerCastRoutes(api huma.API, app *service.App) {
	castUser := func(ctx context.Context) (int64, error) {
		user := userFrom(ctx)
		if !app.CastAccessAllowed(user.ID, user.IsAdmin) {
			return 0, huma.Error403Forbidden(service.ErrCastAccessDenied.Error())
		}
		return user.ID, nil
	}

	type devicesBody struct {
		Items []cast.Device `json:"items"`
	}
	huma.Register(api, secured(op(http.MethodGet, "/api/cast/devices", "cast-devices", "Discovered cast devices", "Cast")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[devicesBody], error) {
			if _, err := castUser(ctx); err != nil {
				return nil, err
			}
			return noStoreJSON(devicesBody{Items: app.Cast().Devices()}), nil
		})

	// Settings surface: values + provenance (env-locked fields grey out in
	// the UI) and the network diagnostics behind Settings → Casting.
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/cast/config", "cast-config", "Casting config", "Cast")),
		func(_ context.Context, _ *struct{}) (*JSONOutput[service.CastConfigView], error) {
			return noStoreJSON(app.CastConfig()), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/cast/config", "set-cast-config", "Apply casting config", "Cast")),
		func(ctx context.Context, in *struct {
			Body struct {
				Enabled        bool    `json:"enabled"`
				BaseURL        string  `json:"base_url" doc:"Optional receiver-facing Heya origin for Chromecast/DLNA URL pulls; empty derives the routed LAN address"`
				Devices        string  `json:"devices" doc:"Comma-separated receiver addresses resolved by unicast mDNS (same-subnet only)"`
				AllowedUserIDs []int64 `json:"allowed_user_ids" doc:"Regular users allowed to discover and control server-side cast receivers; admins are always allowed"`
			}
		}) (*JSONOutput[service.CastConfigView], error) {
			if err := app.SaveCastSettings(ctx, in.Body.Enabled, in.Body.BaseURL, in.Body.Devices, in.Body.AllowedUserIDs); err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(app.CastConfig()), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/cast/status", "cast-status", "Casting network diagnostics", "Cast")),
		func(_ context.Context, _ *struct{}) (*JSONOutput[service.CastNetworkStatus], error) {
			return noStoreJSON(app.CastStatus()), nil
		})

	type sessionsBody struct {
		Items []cast.SessionSnapshot `json:"items"`
	}
	huma.Register(api, secured(op(http.MethodGet, "/api/cast/sessions", "cast-sessions", "Active cast sessions", "Cast")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[sessionsBody], error) {
			userID, err := castUser(ctx)
			if err != nil {
				return nil, err
			}
			return noStoreJSON(sessionsBody{Items: app.Cast().SessionsForUser(userID)}), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/cast/sessions", "cast-play", "Start (or retarget) a cast session", "Cast")),
		func(ctx context.Context, in *struct {
			Body struct {
				DeviceID      string `json:"device_id" minLength:"1" doc:"Target device (from /api/cast/devices)"`
				TrackID       int64  `json:"track_id,omitempty" minimum:"1" doc:"Music track to play; mutually exclusive with file_id"`
				FileID        string `json:"file_id,omitempty" maxLength:"64" doc:"Video library-file reference; mutually exclusive with track_id"`
				EntityType    string `json:"entity_type,omitempty" enum:"movie,episode" doc:"Watch-progress entity type for video"`
				EntityID      int64  `json:"entity_id,omitempty" minimum:"1" doc:"Movie media-item ID or TV episode ID for video progress"`
				Title         string `json:"title,omitempty" maxLength:"500" doc:"Display title for video playback"`
				AudioTrack    int    `json:"audio_track,omitempty" minimum:"0" doc:"Zero-based audio-stream selection for video"`
				SubtitleTrack *int   `json:"subtitle_track,omitempty" minimum:"0" doc:"Zero-based text-subtitle selection for video; omit for subtitles off"`
				Quality       string `json:"quality,omitempty" maxLength:"24" doc:"Optional HLS quality profile for video; auto uses the source-compatible plan"`
				Volume        int    `json:"volume" minimum:"0" maximum:"100" default:"30" doc:"Initial device volume (ignored when retargeting an existing session)"`
				StartSeconds  int    `json:"start_seconds,omitempty" minimum:"0" doc:"Start position in the media item — lets a client hand off mid-playback"`
				StartPaused   bool   `json:"start_paused,omitempty" doc:"Load video paused; used when changing remote track options while paused"`
			}
		}) (*JSONOutput[cast.SessionSnapshot], error) {
			var snap cast.SessionSnapshot
			var err error
			switch {
			case in.Body.TrackID > 0 && in.Body.FileID == "":
				snap, err = app.CastPlayTrack(ctx, userFrom(ctx).ID, in.Body.DeviceID, in.Body.TrackID, in.Body.Volume, in.Body.StartSeconds)
			case in.Body.FileID != "" && in.Body.TrackID == 0:
				snap, err = app.CastPlayVideo(ctx, userFrom(ctx).ID, in.Body.DeviceID, in.Body.FileID, in.Body.EntityType, in.Body.EntityID, in.Body.Title, in.Body.AudioTrack, in.Body.SubtitleTrack, in.Body.Quality, in.Body.Volume, in.Body.StartSeconds, in.Body.StartPaused)
			default:
				return nil, huma.Error422UnprocessableEntity("provide exactly one of track_id or file_id")
			}
			if err != nil {
				if errors.Is(err, cast.ErrDeviceInUse) {
					return nil, huma.Error409Conflict(err.Error())
				}
				return nil, humaServiceErrorStatus(err, http.StatusUnprocessableEntity)
			}
			return noStoreJSON(snap), nil
		})

	sessionByID := func(ctx context.Context, id string) (*cast.Session, error) {
		userID, err := castUser(ctx)
		if err != nil {
			return nil, err
		}
		s, ok := app.Cast().Session(id)
		if !ok || s.UserID != userID {
			return nil, huma.Error404NotFound("no such cast session")
		}
		return s, nil
	}

	huma.Register(api, secured(op(http.MethodGet, "/api/cast/sessions/{id}", "cast-session", "One cast session", "Cast")),
		func(ctx context.Context, in *struct {
			ID string `path:"id"`
		}) (*JSONOutput[cast.SessionSnapshot], error) {
			s, err := sessionByID(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			return noStoreJSON(s.Snapshot()), nil
		})

	// Control verbs. Pause/resume are optimistic: state flips when the
	// receiver-side confirmation lands on the transport's stderr, so a
	// follow-up GET (or the WS event) reflects the truth.
	control := func(path, opID, summary string, do func(*cast.Session) error) {
		huma.Register(api, secured(op(http.MethodPost, path, opID, summary, "Cast")),
			func(ctx context.Context, in *struct {
				ID string `path:"id"`
			}) (*JSONOutput[cast.SessionSnapshot], error) {
				s, err := sessionByID(ctx, in.ID)
				if err != nil {
					return nil, err
				}
				if err := do(s); err != nil {
					return nil, huma.Error422UnprocessableEntity(err.Error())
				}
				return noStoreJSON(s.Snapshot()), nil
			})
	}
	control("/api/cast/sessions/{id}/pause", "cast-pause", "Pause a cast session", (*cast.Session).Pause)
	control("/api/cast/sessions/{id}/resume", "cast-resume", "Resume a cast session", (*cast.Session).Resume)
	control("/api/cast/sessions/{id}/stop", "cast-stop", "Stop a cast session", (*cast.Session).Stop)

	huma.Register(api, secured(op(http.MethodPost, "/api/cast/sessions/{id}/seek", "cast-seek", "Seek within the current track", "Cast")),
		func(ctx context.Context, in *struct {
			ID   string `path:"id"`
			Body struct {
				Seconds int `json:"seconds" minimum:"0" doc:"Absolute position in the track"`
			}
		}) (*JSONOutput[cast.SessionSnapshot], error) {
			s, err := sessionByID(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			if err := s.Seek(in.Body.Seconds); err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return noStoreJSON(s.Snapshot()), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/cast/sessions/{id}/volume", "cast-volume", "Set cast session volume", "Cast")),
		func(ctx context.Context, in *struct {
			ID   string `path:"id"`
			Body struct {
				Level int `json:"level" minimum:"0" maximum:"100" doc:"Device stream volume"`
			}
		}) (*JSONOutput[cast.SessionSnapshot], error) {
			s, err := sessionByID(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			if err := s.SetVolume(in.Body.Level); err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return noStoreJSON(s.Snapshot()), nil
		})
}
