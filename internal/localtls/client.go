// Package localtls builds HTTP clients that trust Heya's embedded Caddy CA in
// addition to the operating system trust store. It is used only by Heya's own
// CLI/TUI clients; browsers and third-party clients remain in full control of
// whether they install the local root.
package localtls

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func RootPath(dataDir string) string {
	return filepath.Join(dataDir, "caddy", "pki", "authorities", "local", "root.crt")
}

// Client returns an isolated transport cloned from http.DefaultTransport. If
// the local root does not exist yet, the client still has the ordinary system
// roots and can connect to public ACME/Tailscale certificates normally.
func Client(dataDir string, timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if pemBytes, err := os.ReadFile(RootPath(dataDir)); err == nil {
		pool.AppendCertsFromPEM(pemBytes)
	}
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return &http.Client{Transport: transport, Timeout: timeout}
}
