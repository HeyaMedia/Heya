package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompatibilityProtocolsBypassETag(t *testing.T) {
	handler := withETag(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	tests := []struct {
		name     string
		path     string
		header   string
		wantETag bool
	}{
		{name: "Jellyfin route", path: "/System/Info/Public"},
		{name: "lowercase Jellyfin miss with identity", path: "/future/endpoint", header: `MediaBrowser Client="test"`},
		{name: "OpenSubsonic route", path: "/rest/ping.view"},
		{name: "Heya collision", path: "/movies/recommendations", wantETag: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if got := w.Header().Get("ETag") != ""; got != tt.wantETag {
				t.Fatalf("ETag presence for %s = %v, want %v", tt.path, got, tt.wantETag)
			}
		})
	}
}
