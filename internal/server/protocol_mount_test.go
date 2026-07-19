package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProtocolMountLandingRedirects(t *testing.T) {
	tests := []struct {
		prefix  string
		landing string
	}{
		{prefix: "/jellyfin", landing: "/jellyfin/System/Info/Public"},
		{prefix: "/subsonic", landing: "/subsonic/rest/ping.view"},
	}

	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			var downstreamPath string
			handler := protocolMount(tt.prefix, tt.landing, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				downstreamPath = r.URL.Path
				w.WriteHeader(http.StatusNoContent)
			}))

			for _, path := range []string{tt.prefix, tt.prefix + "/"} {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
				if recorder.Code != http.StatusTemporaryRedirect {
					t.Fatalf("GET %s status = %d, want %d", path, recorder.Code, http.StatusTemporaryRedirect)
				}
				if got := recorder.Header().Get("Location"); got != tt.landing {
					t.Fatalf("GET %s Location = %q, want %q", path, got, tt.landing)
				}
			}

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tt.prefix+"/rest/probe", nil))
			if recorder.Code != http.StatusNoContent {
				t.Fatalf("nested request status = %d, want %d", recorder.Code, http.StatusNoContent)
			}
			if downstreamPath != "/rest/probe" {
				t.Fatalf("downstream path = %q, want %q", downstreamPath, "/rest/probe")
			}
		})
	}
}
