package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativePlaybackResponseWriterOverridesCacheableMediaHeaders(t *testing.T) {
	for name, write := range map[string]func(http.ResponseWriter){
		"write": func(w http.ResponseWriter) {
			_, _ = w.Write([]byte("segment"))
		},
		"read_from": func(w http.ResponseWriter) {
			_, _ = io.Copy(w, struct{ io.Reader }{strings.NewReader("segment")})
		},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			wrapped := &nativePlaybackResponseWriter{ResponseWriter: recorder}
			wrapped.Header().Set("Cache-Control", "public, max-age=3600")
			write(wrapped)

			if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
				t.Fatalf("Cache-Control = %q", got)
			}
			if recorder.Code != http.StatusOK || recorder.Body.String() != "segment" {
				t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
			}
		})
	}
}
