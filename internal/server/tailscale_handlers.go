package server

import (
	"net/http"

	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/service"
	tsnetwrap "github.com/karbowiak/heya/internal/tailscale"
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
			writeError(w, http.StatusInternalServerError, "save heya.yaml: "+err.Error())
			return
		}

		ts := app.Tailscale()
		if ts == nil {
			writeError(w, http.StatusInternalServerError, "tailscale manager not initialized")
			return
		}

		if !newCfg.Enabled {
			if err := ts.Disable(); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"status": "disabled"})
			return
		}

		// Enable is potentially long-running (90s timeout on first auth).
		// Kick it off in the background and return immediately — the UI
		// will pick up the login URL via the tailscale.status event.
		go func() {
			if err := ts.Enable(r.Context(), tsnetwrap.Config{
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
		if app.Tailscale() == nil {
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
		if err := app.Tailscale().SetFunnel(r.Context(), req.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Persist so the choice survives restart.
		cur := app.ConfigSnapshot().Tailscale
		cur.Funnel = req.Enabled
		app.UpdateTailscaleConfig(cur)
		_ = config.SaveTailscale(cur)
		writeJSON(w, http.StatusOK, map[string]any{"funnel": req.Enabled})
	}
}

func handleTailscaleLogout(app *service.App, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if app.Tailscale() == nil {
			writeError(w, http.StatusBadRequest, "Tailscale is not running")
			return
		}
		if err := app.Tailscale().Logout(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		cur := app.ConfigSnapshot().Tailscale
		cur.Enabled = false
		app.UpdateTailscaleConfig(cur)
		_ = config.SaveTailscale(cur)
		writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
	}
}
