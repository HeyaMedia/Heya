package server

import (
	"io"
	"net/http"

	"github.com/karbowiak/heya/internal/playbackgrant"
	"github.com/karbowiak/heya/internal/service"
)

type nativePlaybackResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *nativePlaybackResponseWriter) prepare() {
	w.Header().Set("Cache-Control", "private, no-store")
}

func (w *nativePlaybackResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.prepare()
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *nativePlaybackResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *nativePlaybackResponseWriter) ReadFrom(src io.Reader) (int64, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return readerFrom.ReadFrom(src)
	}
	return io.Copy(struct{ io.Writer }{w.ResponseWriter}, src)
}

// Unwrap preserves optional streaming interfaces through http.ResponseController.
func (w *nativePlaybackResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func validateNativePlaybackRequest(w http.ResponseWriter, r *http.Request, app *service.App) bool {
	w.Header().Set("Cache-Control", "private, no-store")
	grant := r.Header.Get(playbackgrant.HeaderName)
	if _, err := app.ValidateNativePlaybackGrant(r.Context(), grant, r.URL.Path); err != nil {
		writeError(w, http.StatusForbidden, "invalid or expired native playback grant")
		return false
	}
	// The delegated browser handlers do not need or forward this credential.
	r.Header.Del(playbackgrant.HeaderName)
	q := r.URL.Query()
	q.Del("token")
	q.Del("cast_token")
	r.URL.RawQuery = q.Encode()
	return true
}

func handleNativePlaybackStream(app *service.App, normal http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !validateNativePlaybackRequest(w, r, app) {
			return
		}
		normal(&nativePlaybackResponseWriter{ResponseWriter: w}, r)
	}
}
