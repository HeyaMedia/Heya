package server

import (
	"context"
	"net/http"

	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/service"
	tsnetwrap "github.com/karbowiak/heya/internal/tailscale"
	"github.com/rs/zerolog/log"
)

type tailscaleConfigPayload struct {
	Enabled  bool   `json:"enabled"`
	Hostname string `json:"hostname"`
	HTTPS    bool   `json:"https"`
	Funnel   bool   `json:"funnel"`
}

type tailscaleStatusResponse struct {
	Enabled bool                    `json:"enabled"`
	Config  *config.TailscaleConfig `json:"config,omitempty"`
	Status  *tsnetwrap.Status       `json:"status,omitempty"`
	Message string                  `json:"message,omitempty"`
}

func handleTailscaleStatus(app *service.App, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cur := app.ConfigSnapshot().Tailscale
		resp := tailscaleStatusResponse{
			Enabled: cur.Enabled,
			Config:  &cur,
		}
		if ts := app.Tailscale(); ts != nil {
			st := ts.Status()
			resp.Status = &st
		} else {
			resp.Message = "Tailscale manager not initialized — restart the server."
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// handleTailscaleConfig accepts a full TailscaleConfig from the UI: toggles
// the node on/off, applies hostname / HTTPS / Funnel changes, and persists
// the result to heya.yaml.
func handleTailscaleConfig(app *service.App, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var p tailscaleConfigPayload
		if err := readJSON(r, &p); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}

		newCfg := config.TailscaleConfig{
			Enabled:  p.Enabled,
			Hostname: p.Hostname,
			AuthKey:  app.ConfigSnapshot().Tailscale.AuthKey, // never overwrite from API
			StateDir: app.ConfigSnapshot().Tailscale.StateDir,
			HTTPS:    p.HTTPS,
			Funnel:   p.Funnel,
		}
		if newCfg.Hostname == "" {
			newCfg.Hostname = "heya"
		}
		if newCfg.StateDir == "" {
			newCfg.StateDir = app.ConfigSnapshot().DataDir + "/tailscale"
		}

		app.UpdateTailscaleConfig(newCfg)

		if err := config.SaveTailscale(newCfg); err != nil {
			log.Error().Err(err).Msg("failed to save tailscale config to heya.yaml")
			writeError(w, http.StatusInternalServerError, "save heya.yaml: "+err.Error())
			return
		}
		log.Info().
			Bool("enabled", newCfg.Enabled).
			Bool("https", newCfg.HTTPS).
			Bool("funnel", newCfg.Funnel).
			Str("hostname", newCfg.Hostname).
			Msg("tailscale config saved")

		ts := app.Tailscale()
		if ts == nil {
			writeError(w, http.StatusInternalServerError, "tailscale manager not initialized")
			return
		}

		if !newCfg.Enabled {
			// Disable closes tsnet listeners. If this request itself came
			// in over a tsnet listener, doing the close synchronously
			// would deadlock on http.Server.Shutdown (active connection
			// = us). Background the work and reply immediately.
			go func() { _ = ts.Disable() }()
			writeJSON(w, http.StatusOK, map[string]any{"status": "disabling"})
			return
		}

		// Enable is potentially long-running (90s timeout on first auth).
		// Kick it off in the background and return immediately — the UI
		// will pick up the login URL via the tailscale.status event.
		go func() {
			if err := ts.Enable(context.Background(), tsnetwrap.Config{
				Enabled:  true,
				Hostname: newCfg.Hostname,
				AuthKey:  newCfg.AuthKey,
				StateDir: newCfg.StateDir,
				HTTPS:    newCfg.HTTPS,
				Funnel:   newCfg.Funnel,
			}); err != nil {
				// surfaced via Status.LastError
				_ = err
			}
		}()

		writeJSON(w, http.StatusOK, map[string]any{"status": "enabling"})
	}
}

func handleTailscaleFunnel(app *service.App, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ts := app.Tailscale()
		if ts == nil {
			writeError(w, http.StatusBadRequest, "Tailscale is not running")
			return
		}
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}

		// Persist the preference immediately — the toggle should be sticky
		// regardless of how the listener rebind goes.
		cur := app.ConfigSnapshot().Tailscale
		cur.Funnel = req.Enabled
		app.UpdateTailscaleConfig(cur)
		if err := config.SaveTailscale(cur); err != nil {
			log.Warn().Err(err).Msg("failed to persist tailscale funnel preference to heya.yaml")
		} else {
			log.Info().Bool("funnel", req.Enabled).Msg("tailscale funnel preference saved")
		}

		// SetFunnel closes the current :443 listener and opens a new one
		// in the new mode. If THIS request was served on the tsnet listener
		// we're about to close, doing the rebind synchronously deadlocks
		// on http.Server.Shutdown (the only active connection on that
		// server is us, and we're blocked inside SetFunnel). Background
		// the work — UI picks up the new state via the WS status event.
		go func() {
			_ = ts.SetFunnel(context.Background(), req.Enabled)
		}()

		writeJSON(w, http.StatusOK, map[string]any{"funnel": req.Enabled})
	}
}

// handleTailscaleRaw dumps the live ipnstate.Status from tsnet's LocalClient.
// Same content as `tailscale status --json` against a system tailscaled.
// Mounted at /api/tailscale/raw — admin-only.
func handleTailscaleRaw(app *service.App, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ts := app.Tailscale()
		if ts == nil {
			writeError(w, http.StatusBadRequest, "Tailscale is not running")
			return
		}
		st, err := ts.RawStatus(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, st)
	}
}

func handleTailscaleLogout(app *service.App, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ts := app.Tailscale()
		if ts == nil {
			writeError(w, http.StatusBadRequest, "Tailscale is not running")
			return
		}
		// Same deadlock concern as Funnel toggle: Logout tears down the
		// listeners after talking to tailscale.com. Background it.
		cur := app.ConfigSnapshot().Tailscale
		cur.Enabled = false
		app.UpdateTailscaleConfig(cur)
		_ = config.SaveTailscale(cur)
		go func() { _ = ts.Logout(context.Background()) }()
		writeJSON(w, http.StatusOK, map[string]string{"status": "logging out"})
	}
}
