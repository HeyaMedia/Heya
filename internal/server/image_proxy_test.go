package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/karbowiak/heya/internal/config"
)

func passiveCfg(passive bool, proxyURL string) *config.Config {
	return &config.Config{
		PassiveMode:   config.Field[bool]{Value: passive},
		ImageProxyURL: config.Field[string]{Value: proxyURL},
	}
}

func TestNewPassiveImageProxy_NilCases(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
	}{
		{"nil config", nil},
		{"not passive", passiveCfg(false, "https://heya.example.ts.net")},
		{"passive but no url", passiveCfg(true, "")},
		{"passive but garbage url", passiveCfg(true, "://not-a-url")},
		{"passive but relative url", passiveCfg(true, "/api/foo")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if p := newPassiveImageProxy(tc.cfg); p != nil {
				t.Fatalf("expected nil proxy, got %v", p)
			}
		})
	}
}

// In passive mode with a valid upstream, the proxy must forward the inbound
// path + query verbatim (the upstream serves identical routes) and stream the
// upstream body back unchanged.
func TestNewPassiveImageProxy_ForwardsPathAndBody(t *testing.T) {
	var gotPath, gotRawQuery, gotHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawQuery = r.URL.RawQuery
		gotHost = r.Host
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("JPEGBYTES"))
	}))
	defer upstream.Close()

	proxy := newPassiveImageProxy(passiveCfg(true, upstream.URL))
	if proxy == nil {
		t.Fatal("expected a non-nil proxy for passive mode + valid url")
	}

	srv := httptest.NewServer(proxiedImage(proxy, func(w http.ResponseWriter, r *http.Request) {
		t.Error("local handler must not run when a proxy is configured")
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/media/42/image/poster?w=300")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if gotPath != "/api/media/42/image/poster" {
		t.Errorf("upstream path = %q, want /api/media/42/image/poster", gotPath)
	}
	if gotRawQuery != "w=300" {
		t.Errorf("upstream query = %q, want w=300", gotRawQuery)
	}
	if gotHost != upstream.Listener.Addr().String() {
		t.Errorf("upstream Host header = %q, want %q (proxy must target the upstream vhost)", gotHost, upstream.Listener.Addr().String())
	}
	if string(body) != "JPEGBYTES" {
		t.Errorf("body = %q, want JPEGBYTES", string(body))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
}

// proxiedImage with a nil proxy is exactly the local handler (the normal,
// non-passive path) — zero behavioural change.
func TestProxiedImage_NilProxyUsesLocal(t *testing.T) {
	called := false
	h := proxiedImage(nil, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	})
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/media/1/image/poster", nil))
	if !called {
		t.Fatal("local handler should have been called when proxy is nil")
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
}
