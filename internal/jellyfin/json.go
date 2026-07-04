package jellyfin

import (
	"net/http"

	json "github.com/goccy/go-json"
)

// writeJSON emits a Jellyfin-shaped JSON response. Same goccy codec the huma
// surface uses. Content type matches Jellyfin's (clients don't care, but the
// point of this package is to be indistinguishable).
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decodeJSON parses a request body. encoding/json (and goccy) match struct
// fields case-insensitively, which mirrors ASP.NET model binding — clients
// sending "username" instead of "Username" keep working.
func decodeJSON(r *http.Request, v any) error {
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(v)
}
