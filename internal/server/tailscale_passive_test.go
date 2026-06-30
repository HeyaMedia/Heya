package server

import (
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/config"
)

// The mutating tailscale endpoints must refuse to run in passive mode: they
// would persist to the borrowed (prod) DB and bring up a tsnet node under the
// source server's identity. tailscaleReadOnly is the gate at the top of each.
func TestTailscaleReadOnly(t *testing.T) {
	passive := &config.Config{PassiveMode: config.Field[bool]{Value: true}}
	if err := tailscaleReadOnly(passive); err == nil {
		t.Fatal("passive mode must block tailscale mutations")
	} else if se, ok := err.(huma.StatusError); !ok || se.GetStatus() != http.StatusForbidden {
		t.Fatalf("want 403 StatusError, got %v", err)
	}

	for _, tc := range []struct {
		name string
		cfg  *config.Config
	}{
		{"nil config", nil},
		{"not passive", &config.Config{PassiveMode: config.Field[bool]{Value: false}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tailscaleReadOnly(tc.cfg); err != nil {
				t.Fatalf("want nil (not blocked), got %v", err)
			}
		})
	}
}
