package jellyfin

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
	"net/http"
	"strings"
	"sync"
)

// The vendored Jellyfin 10.11.11 OpenAPI spec — the triage source for the
// coverage manifest (manifest.go, manifest_test.go) AND served verbatim at
// /api-docs/openapi.json like a real server (tooling and API browsers fetch
// it; upstream serves it anonymously).

//go:embed spec/jellyfin-openapi-10.11.11.json.gz
var specGz []byte

var (
	specRawOnce sync.Once
	specRaw     []byte
)

// GET /api-docs/openapi.json — anonymous, like upstream. Clients that accept
// gzip get the vendored bytes as-is; others get them decompressed once and
// cached.
func (s *Server) handleOpenAPISpec(w http.ResponseWriter, r *http.Request, _ Params) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(specGz)
		return
	}
	specRawOnce.Do(func() {
		zr, err := gzip.NewReader(bytes.NewReader(specGz))
		if err != nil {
			return
		}
		specRaw, _ = io.ReadAll(zr)
	})
	if specRaw == nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(specRaw)
}

// GET /robots.txt — upstream redirects to the web client's robots file; we
// serve a deny-all at the target (a personal media server has no business in
// a crawler index). Registered only when the Jellyfin surface is enabled.
func (s *Server) handleRobotsRedirect(w http.ResponseWriter, r *http.Request, _ Params) {
	http.Redirect(w, r, "web/robots.txt", http.StatusFound)
}

func (s *Server) handleRobotsTxt(w http.ResponseWriter, _ *http.Request, _ Params) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}
