package tailscale

import (
	"encoding/json"
	"net/http"
)

// ControlHandler exposes a *Server's control surface over HTTP so the dev
// front-door (`heya dev-proxy`) can be driven by the hot-reloading backend.
// The backend's *RemoteClient is the only caller, over a localhost-only unix
// socket — there is no auth here on purpose: it never binds a TCP port and is
// dev-only. The request/response bodies mirror the Manager method set.
func ControlHandler(s *Server) http.Handler {
	mux := http.NewServeMux()

	// Each mutating route returns the post-action Status snapshot so the
	// RemoteClient can update its cache (and fire its onStatus) without
	// waiting for the next poll tick.
	mux.HandleFunc("POST /enable", func(w http.ResponseWriter, r *http.Request) {
		var cfg Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.Enable(r.Context(), cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, s.Status())
	})

	mux.HandleFunc("POST /disable", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Disable(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, s.Status())
	})

	mux.HandleFunc("POST /funnel", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			On bool `json:"on"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.SetFunnel(r.Context(), body.On); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, s.Status())
	})

	mux.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Logout(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, s.Status())
	})

	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.Status())
	})

	mux.HandleFunc("GET /raw", func(w http.ResponseWriter, r *http.Request) {
		st, err := s.RawStatus(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, st)
	})

	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
