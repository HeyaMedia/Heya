package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProtocolRouter(t *testing.T) {
	marker := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Heya-Test-Handler", name)
			w.WriteHeader(http.StatusNoContent)
		})
	}
	handler := protocolRouter(marker("jellyfin"), marker("subsonic"), marker("spa"))

	tests := []struct {
		name   string
		path   string
		header string
		want   string
	}{
		{name: "Jellyfin discovery", path: "/System/Info/Public", want: "jellyfin"},
		{name: "lowercase Jellyfin login", path: "/users/authenticatebyname", want: "jellyfin"},
		{name: "OpenSubsonic endpoint", path: "/rest/ping.view", want: "subsonic"},
		{name: "unknown OpenSubsonic endpoint", path: "/rest/futureEndpoint", want: "subsonic"},
		{name: "Heya recommendation page", path: "/movies/recommendations", want: "spa"},
		{name: "credentialed collision", path: "/movies/recommendations", header: `MediaBrowser Client="Infuse"`, want: "jellyfin"},
		{name: "removed Jellyfin prefix", path: "/jellyfin/System/Info/Public", want: "spa"},
		{name: "removed Subsonic prefix", path: "/subsonic/rest/ping.view", want: "spa"},
		{name: "ordinary Heya page", path: "/search", want: "spa"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, r)
			if got := recorder.Header().Get("X-Heya-Test-Handler"); got != tt.want {
				t.Fatalf("handler for %s = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
