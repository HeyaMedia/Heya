package server

import (
	"net/http"

	"github.com/karbowiak/heya/internal/service"
)

// castVideoCORS permits Google's hosted Default Media Receiver to read Heya's
// LAN media URL. Authorization still comes exclusively from the scoped token.
func castVideoCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers", "Accept-Ranges, Content-Length, Content-Range")
}

func validateCastVideoRequest(w http.ResponseWriter, r *http.Request, app *service.App) bool {
	castVideoCORS(w)
	if app.Cast() == nil {
		writeError(w, http.StatusNotFound, "cast media not found")
		return false
	}
	userID, err := app.Cast().ValidateMediaToken(r.URL.Query().Get("cast_token"), r.URL.Path)
	if err != nil || app.ValidateCastMediaAccess(r.Context(), userID) != nil {
		writeError(w, http.StatusForbidden, "invalid or expired cast media token")
		return false
	}
	return true
}

func handleCastVideoDirect(app *service.App) http.HandlerFunc {
	normal := handleDirectStream(app)
	return func(w http.ResponseWriter, r *http.Request) {
		if !validateCastVideoRequest(w, r, app) {
			return
		}
		q := r.URL.Query()
		q.Del("cast_token")
		r.URL.RawQuery = q.Encode()
		w.Header().Set("Cache-Control", "private, no-store")
		normal(w, r)
	}
}

// handleCastVideoHLS authenticates every manifest and segment request before
// reusing the normal HLS implementation. Keep cast_token in the query: the
// master and variant writers propagate it to their child URLs.
func handleCastVideoHLS(app *service.App, normal http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !validateCastVideoRequest(w, r, app) {
			return
		}
		normal(w, r)
	}
}

func handleCastVideoSubtitle(app *service.App) http.HandlerFunc {
	normal := handleGetSubtitleAs(app, true)
	return func(w http.ResponseWriter, r *http.Request) {
		if !validateCastVideoRequest(w, r, app) {
			return
		}
		q := r.URL.Query()
		q.Del("cast_token")
		r.URL.RawQuery = q.Encode()
		w.Header().Set("Cache-Control", "private, no-store")
		normal(w, r)
	}
}
