package localtls

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestClientUsesIsolatedTransportAndStableRootPath(t *testing.T) {
	dataDir := t.TempDir()
	client := Client(dataDir, 12*time.Second)
	if client.Timeout != 12*time.Second {
		t.Fatalf("timeout = %s", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == http.DefaultTransport || transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
		t.Fatalf("client transport was not isolated: %#v", client.Transport)
	}
	want := filepath.Join(dataDir, "caddy", "pki", "authorities", "local", "root.crt")
	if got := RootPath(dataDir); got != want {
		t.Fatalf("RootPath = %q, want %q", got, want)
	}
}
