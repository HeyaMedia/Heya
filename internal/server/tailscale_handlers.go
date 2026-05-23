package server

import (
	"net/http"

	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/service"
)

type tailscaleStatusResponse struct {
	Enabled bool   `json:"enabled"`
	Status  any    `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

func handleTailscaleStatus(app *service.App, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.Tailscale.Enabled {
			writeJSON(w, http.StatusOK, tailscaleStatusResponse{
				Enabled: false,
				Message: "Tailscale is disabled in heya.yaml — set tailscale.enabled: true to onboard.",
			})
			return
		}
		ts := app.Tailscale()
		if ts == nil {
			writeJSON(w, http.StatusOK, tailscaleStatusResponse{
				Enabled: true,
				Message: "Tailscale is enabled but not yet started — restart the server.",
			})
			return
		}
		writeJSON(w, http.StatusOK, tailscaleStatusResponse{
			Enabled: true,
			Status:  ts.Status(),
		})
	}
}

func handleTailscaleFunnel(app *service.App, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.Tailscale.Enabled || app.Tailscale() == nil {
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
		app.Tailscale().SetFunnel(req.Enabled)
		writeJSON(w, http.StatusOK, map[string]any{
			"funnel": req.Enabled,
			"note":   "Funnel preference recorded. The current listener keeps serving until restart — restart serve to bind the new mode.",
		})
	}
}

func handleTailscaleLogout(app *service.App, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.Tailscale.Enabled || app.Tailscale() == nil {
			writeError(w, http.StatusBadRequest, "Tailscale is not running")
			return
		}
		if err := app.Tailscale().Logout(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
	}
}
