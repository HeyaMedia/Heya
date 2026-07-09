package subsonic

import "net/http"

// Honest "feature absent" stubs: a probing client must conclude "this
// server has none of these", never "this server is broken". Shares,
// podcasts (Subsonic-managed server-side subscriptions — Heya's podcast
// system is per-user and lives on the native API), internet radio, and
// chat all answer their empty collection shape; their mutation endpoints
// are unregistered and answer error 0 like any unknown view.
func (s *Server) stubEmpty(key string, payload any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		respond(w, r, key, payload)
	}
}
