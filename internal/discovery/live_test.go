package discovery

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// TestLiveAdvertiseRoundTrip publishes a real service on the real multicast
// group and browses for it from the same host — the only check that proves
// the wire format is right end to end (a unit test can validate the TXT
// strings but not that a responder answers a query with them).
//
// Skipped by default: it needs a multicast-capable interface and, on macOS,
// triggers the local-network permission prompt, neither of which belongs in
// a plain `go test ./...` run.
//
//	HEYA_DISCOVERY_LIVE_TEST=1 go test ./internal/discovery/ -run TestLive -v -timeout 60s
func TestLiveAdvertiseRoundTrip(t *testing.T) {
	if os.Getenv("HEYA_DISCOVERY_LIVE_TEST") == "" {
		t.Skip("set HEYA_DISCOVERY_LIVE_TEST=1 to run the live mDNS round-trip test")
	}

	const (
		instance = "heya-live-test"
		serverID = "0123456789abcdef0123456789abcdef"
	)
	manager := NewManager(zerolog.New(zerolog.NewTestWriter(t)), nil)
	if err := manager.Advertise(Config{
		Instance: instance,
		Port:     18080,
		HTTPS:    true,
		ServerID: serverID,
		Version:  "live-test",
	}); err != nil {
		t.Fatalf("Advertise: %v", err)
	}
	defer func() {
		if err := manager.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	if st := manager.Status(); !st.Advertising || st.Instance != instance {
		t.Fatalf("status after Advertise = %+v", st)
	}

	// Responders answer the first query almost immediately, but give the
	// multicast group a generous window before calling it a failure.
	found, err := Browse(context.Background(), ServiceType, 6*time.Second)
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}

	matched := false
	for _, entry := range found {
		if entry.Instance != instance {
			continue
		}
		matched = true
		t.Logf("found %s at %s (txt %v)", entry.Instance, entry.URL(), entry.RawTXT)
		if entry.Port != 18080 {
			t.Errorf("port = %d, want 18080", entry.Port)
		}
		if entry.ID() != serverID {
			t.Errorf("id = %q, want %q", entry.ID(), serverID)
		}
		if entry.TXT["scheme"] != "https" {
			t.Errorf("scheme = %q, want https", entry.TXT["scheme"])
		}
		if entry.TXT["v"] != TXTVersion {
			t.Errorf("v = %q, want %q", entry.TXT["v"], TXTVersion)
		}
		// The advertised host must be a single label under `.local.`: a
		// doubled `host.local.local.` or a name missing its trailing dot are
		// both real failure modes of the underlying library.
		if want := hostLabel(entry.Hostname) + ".local"; entry.Hostname != want {
			t.Errorf("advertised host = %q, want %q", entry.Hostname, want)
		}
	}
	if !matched {
		t.Fatalf("advertised instance %q did not answer a browse; saw %d other instances", instance, len(found))
	}

	// Withdrawal: disabling must take the entry off the wire promptly via
	// goodbye packets, not leave clients offering a dead server until the TTL
	// expires.
	if err := manager.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if st := manager.Status(); st.Advertising {
		t.Error("status still reports advertising after Stop")
	}
	after, err := Browse(context.Background(), ServiceType, 4*time.Second)
	if err != nil {
		t.Fatalf("Browse after Stop: %v", err)
	}
	for _, entry := range after {
		if entry.Instance == instance {
			t.Errorf("instance %q still answered %s after Stop", instance, entry.URL())
		}
	}
}

// We rely on zeroconf answering with the addresses of the interface a query
// ARRIVED on rather than the full set — that is what keeps docker/CNI/VM
// addresses away from LAN clients without any name-based guessing. It only
// happens when the registered address list is empty, and it depends on the
// interface index coming through in a socket control message. If that ever
// stops working the failure is silent (a client is handed addresses it
// cannot route to, or none at all), so assert it explicitly.
func TestLivePerInterfaceAddressScoping(t *testing.T) {
	if os.Getenv("HEYA_DISCOVERY_LIVE_TEST") == "" {
		t.Skip("set HEYA_DISCOVERY_LIVE_TEST=1 to run the live mDNS scoping test")
	}

	const instance = "heya-scoping-test"
	manager := NewManager(zerolog.New(zerolog.NewTestWriter(t)), nil)
	if err := manager.Advertise(Config{Instance: instance, Port: 18082}); err != nil {
		t.Fatalf("Advertise: %v", err)
	}
	defer func() { _ = manager.Close() }()

	st := manager.Status()
	if !st.PerInterface {
		t.Fatal("expected per-interface mode when no addresses are configured")
	}

	found, err := Browse(context.Background(), ServiceType, 6*time.Second)
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	for _, entry := range found {
		if entry.Instance != instance {
			continue
		}
		if len(entry.IPv4) == 0 && len(entry.IPv6) == 0 {
			t.Fatal("no addresses answered — the interface index is not reaching appendAddrs, " +
				"so clients cannot resolve this server; set HEYA_DISCOVERY_ADDRESSES on this platform")
		}
		// The browse arrives over one interface, so one address is the whole
		// point. More means the scoping silently stopped working and dead
		// bridge addresses are reaching clients again.
		if len(entry.IPv4) > 1 {
			t.Errorf("answer carried %d IPv4 addresses (%v), want 1 scoped to the receiving interface",
				len(entry.IPv4), entry.IPv4)
		}
		if len(st.Addresses) > 1 && len(entry.IPv4) == 1 {
			t.Logf("scoping works: host has %d candidate addresses, client was told %s",
				len(st.Addresses), entry.IPv4[0])
		}
		return
	}
	t.Fatalf("instance %q never answered", instance)
}
