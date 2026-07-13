package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/cast"
	"github.com/karbowiak/heya/internal/service"
)

// registerCastRoutes mounts server-side casting: device discovery plus
// the playback sessions that stream to network receivers. Sessions are
// household-scoped — any authenticated user sees and controls them,
// matching the global EventCastState broadcasts every client mirrors.
func registerCastRoutes(api huma.API, app *service.App) {
	type devicesBody struct {
		Items []cast.Device `json:"items"`
	}
	huma.Register(api, secured(op(http.MethodGet, "/api/cast/devices", "cast-devices", "Discovered cast devices", "Cast")),
		func(_ context.Context, _ *struct{}) (*JSONOutput[devicesBody], error) {
			return noStoreJSON(devicesBody{Items: app.Cast().Devices()}), nil
		})

	// Settings surface: values + provenance (env-locked fields grey out in
	// the UI) and the network diagnostics behind Settings → Casting.
	huma.Register(api, secured(op(http.MethodGet, "/api/cast/config", "cast-config", "Casting config", "Cast")),
		func(_ context.Context, _ *struct{}) (*JSONOutput[service.CastConfigView], error) {
			return noStoreJSON(app.CastConfig()), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/cast/config", "set-cast-config", "Apply casting config", "Cast")),
		func(ctx context.Context, in *struct {
			Body struct {
				Enabled bool   `json:"enabled"`
				Devices string `json:"devices" doc:"Comma-separated receiver addresses resolved by unicast mDNS (same-subnet only)"`
			}
		}) (*JSONOutput[service.CastConfigView], error) {
			if err := app.SaveCastSettings(ctx, in.Body.Enabled, in.Body.Devices); err != nil {
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
		func(_ context.Context, _ *struct{}) (*JSONOutput[sessionsBody], error) {
			return noStoreJSON(sessionsBody{Items: app.Cast().Sessions()}), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/cast/sessions", "cast-play", "Start (or retarget) a cast session", "Cast")),
		func(ctx context.Context, in *struct {
			Body struct {
				DeviceID     string `json:"device_id" minLength:"1"      doc:"Target device (from /api/cast/devices)"`
				TrackID      int64  `json:"track_id"  minimum:"1"        doc:"Music track to play"`
				Volume       int    `json:"volume"    minimum:"0" maximum:"100" default:"30" doc:"Initial device volume (ignored when retargeting an existing session)"`
				StartSeconds int    `json:"start_seconds,omitempty" minimum:"0" doc:"Start position in the track — lets a client hand off mid-track playback"`
			}
		}) (*JSONOutput[cast.SessionSnapshot], error) {
			snap, err := app.CastPlayTrack(ctx, userFrom(ctx).ID, in.Body.DeviceID, in.Body.TrackID, in.Body.Volume, in.Body.StartSeconds)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return noStoreJSON(snap), nil
		})

	sessionByID := func(id string) (*cast.Session, error) {
		s, ok := app.Cast().Session(id)
		if !ok {
			return nil, huma.Error404NotFound("no such cast session")
		}
		return s, nil
	}

	huma.Register(api, secured(op(http.MethodGet, "/api/cast/sessions/{id}", "cast-session", "One cast session", "Cast")),
		func(_ context.Context, in *struct {
			ID string `path:"id"`
		}) (*JSONOutput[cast.SessionSnapshot], error) {
			s, err := sessionByID(in.ID)
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
			func(_ context.Context, in *struct {
				ID string `path:"id"`
			}) (*JSONOutput[cast.SessionSnapshot], error) {
				s, err := sessionByID(in.ID)
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
		func(_ context.Context, in *struct {
			ID   string `path:"id"`
			Body struct {
				Seconds int `json:"seconds" minimum:"0" doc:"Absolute position in the track"`
			}
		}) (*JSONOutput[cast.SessionSnapshot], error) {
			s, err := sessionByID(in.ID)
			if err != nil {
				return nil, err
			}
			if err := s.Seek(in.Body.Seconds); err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return noStoreJSON(s.Snapshot()), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/cast/sessions/{id}/volume", "cast-volume", "Set cast session volume", "Cast")),
		func(_ context.Context, in *struct {
			ID   string `path:"id"`
			Body struct {
				Level int `json:"level" minimum:"0" maximum:"100" doc:"Device stream volume"`
			}
		}) (*JSONOutput[cast.SessionSnapshot], error) {
			s, err := sessionByID(in.ID)
			if err != nil {
				return nil, err
			}
			if err := s.SetVolume(in.Body.Level); err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return noStoreJSON(s.Snapshot()), nil
		})
}
