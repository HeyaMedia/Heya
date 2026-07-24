package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/discovery"
	"github.com/karbowiak/heya/internal/ui"
)

// system_settings keys for LAN discovery. Only enabled + name are UI-editable
// — port/host/addresses/interfaces are container-shaped escape hatches that
// stay env-only, and server.id is an internal persisted identity.
const (
	discoveryKeyEnabled = "discovery.enabled"
	discoveryKeyName    = "discovery.name"
	serverKeyID         = "server.id"
)

// DiscoveryUpdate is the DTO accepted by the API for runtime changes.
type DiscoveryUpdate struct {
	Enabled bool
	Name    string
}

// Discovery returns the mDNS advertiser (nil until serve.go wires it — the
// worker and CLI processes never advertise).
func (a *App) Discovery() *discovery.Manager {
	a.networkMu.RLock()
	defer a.networkMu.RUnlock()
	return a.discovery
}

func (a *App) SetDiscovery(m *discovery.Manager) {
	a.networkMu.Lock()
	a.discovery = m
	a.networkMu.Unlock()
}

// SaveDiscoverySettings persists the UI-editable fields, refusing any whose
// effective value is env-locked (validate-all-then-write-all).
func (a *App) SaveDiscoverySettings(ctx context.Context, u DiscoveryUpdate) error {
	u.Name = strings.Join(strings.Fields(u.Name), " ")
	if len(u.Name) > 63 {
		return fmt.Errorf("discovery name must be 63 characters or fewer")
	}

	a.configMu.Lock()
	defer a.configMu.Unlock()

	cur := a.config.Discovery
	if err := errIfEnvLockedChanged(discoveryKeyEnabled, cur.Enabled, u.Enabled); err != nil {
		return err
	}
	if err := errIfEnvLockedChanged(discoveryKeyName, cur.Name, u.Name); err != nil {
		return err
	}

	if err := persistAndOverlayField(a, ctx, discoveryKeyEnabled, &a.config.Discovery.Enabled, u.Enabled); err != nil {
		return err
	}
	return persistAndOverlayField(a, ctx, discoveryKeyName, &a.config.Discovery.Name, u.Name)
}

// SaveAndApplyDiscoverySettings persists then re-publishes, keeping the order
// identical across concurrent admin requests.
func (a *App) SaveAndApplyDiscoverySettings(ctx context.Context, u DiscoveryUpdate) (config.DiscoveryConfig, error) {
	a.discoverySettingsMu.Lock()
	defer a.discoverySettingsMu.Unlock()
	if err := a.SaveDiscoverySettings(ctx, u); err != nil {
		return config.DiscoveryConfig{}, err
	}
	if err := a.applyDiscoveryRuntimeLocked(ctx); err != nil {
		return config.DiscoveryConfig{}, err
	}
	return a.ConfigSnapshot().Discovery, nil
}

// ApplyDiscoveryRuntime publishes (or withdraws) the advertisement for the
// config already loaded. Called once at serve boot and after every save.
//
// Unlike remote/tailscale this runs inline: zeroconf.Register binds two UDP
// sockets and returns — there is no bring-up to stream progress for, so a
// background transition would only add moving parts.
func (a *App) ApplyDiscoveryRuntime(ctx context.Context) error {
	a.discoverySettingsMu.Lock()
	defer a.discoverySettingsMu.Unlock()
	return a.applyDiscoveryRuntimeLocked(ctx)
}

func (a *App) applyDiscoveryRuntimeLocked(ctx context.Context) error {
	manager := a.Discovery()
	if manager == nil {
		return fmt.Errorf("discovery manager is unavailable")
	}
	cur := a.ConfigSnapshot().Discovery
	if !cur.Enabled.Value {
		return manager.Stop()
	}
	runtimeConfig, err := a.DiscoveryRuntimeConfig(ctx)
	if err != nil {
		return err
	}
	return manager.Advertise(runtimeConfig)
}

// LoadDiscoveryFromDB seeds the in-memory snapshot from system_settings.
// Skipped in passive mode by the caller for the same reason tailscale is: a
// borrowed production DB carries the *production* server's discovery name,
// and republishing it from a dev box puts two servers on the LAN claiming one
// DNS-SD instance name.
func (a *App) LoadDiscoveryFromDB(ctx context.Context) {
	if a.db == nil {
		return // spec-dump / test construction without a database
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()

	overlayFieldFromDB(a, ctx, &a.config.Discovery.Enabled, discoveryKeyEnabled, nil)
	overlayFieldFromDB(a, ctx, &a.config.Discovery.Name, discoveryKeyName, func(v string) bool { return len(v) <= 63 })
}

// DiscoveryRuntimeConfig materializes the advertiser-facing config: resolves
// the empty name to the hostname, port 0 to the port this server binds, and
// folds in the current remote-access URL so a client provisioned on the LAN
// also learns how to reach the server from outside.
func (a *App) DiscoveryRuntimeConfig(ctx context.Context) (discovery.Config, error) {
	snapshot := a.ConfigSnapshot()
	if snapshot == nil {
		return discovery.Config{}, fmt.Errorf("discovery config is unavailable")
	}
	cur := snapshot.Discovery

	port := cur.Port.Value
	if port == 0 {
		parsed, err := strconv.Atoi(strings.TrimSpace(snapshot.Port.Value))
		if err != nil || parsed < 1 || parsed > 65535 {
			return discovery.Config{}, fmt.Errorf("cannot advertise: HEYA_PORT %q is not a usable port (set HEYA_DISCOVERY_PORT)", snapshot.Port.Value)
		}
		port = parsed
	}

	return discovery.Config{
		Instance:   a.ServerDisplayName(),
		Port:       port,
		HTTPS:      a.publicSchemeIsHTTPS(),
		ServerID:   a.ServerID(ctx),
		Version:    ui.Version,
		RemoteURL:  a.remoteURL(),
		Host:       cur.Host.Value,
		Addresses:  discovery.SplitList(cur.Addresses.Value),
		Interfaces: discovery.SplitList(cur.Interfaces.Value),
	}, nil
}

// ServerDisplayName is the human-facing name for this install — the DNS-SD
// instance name, and what a client shows in its server list. Configured name
// wins; otherwise the system hostname; "Heya" only if the OS won't say.
func (a *App) ServerDisplayName() string {
	if cfg := a.ConfigSnapshot(); cfg != nil {
		if name := strings.TrimSpace(cfg.Discovery.Name.Value); name != "" {
			return name
		}
	}
	if host, err := os.Hostname(); err == nil {
		// macOS hands back "knas.local" / "laptop.lan"; the bare label reads
		// better in a picker and stays a valid instance name.
		if host = strings.TrimSpace(host); host != "" {
			return strings.SplitN(host, ".", 2)[0]
		}
	}
	return "Heya"
}

// publicSchemeIsHTTPS reports the scheme clients should use for the
// advertised port, read from the live host listener rather than guessed:
// production Caddy terminates TLS, `--dev-backend` serves plaintext.
func (a *App) publicSchemeIsHTTPS() bool {
	manager := a.Ingress()
	if manager == nil {
		return false
	}
	for _, listener := range manager.Status().Listeners {
		if listener.Kind == "host" {
			return listener.TLS
		}
	}
	return false
}

// remoteURL returns the public origin when direct remote access is up, "" otherwise.
func (a *App) remoteURL() string {
	manager := a.Remote()
	if manager == nil {
		return ""
	}
	return manager.Status().RemoteURL
}

// Process-lifetime cache for the persisted server id. Package-level for the
// same reason jfServerID is: App is a boot-time singleton.
var (
	serverIDOnce sync.Once
	serverID     string
)

// ServerID returns the stable identity of this Heya install — advertised in
// the mDNS TXT record and returned by the public /api/server/info endpoint.
// Clients key their saved-server entries on it, so a rename, a new IP or a
// switch between LAN and remote URLs must not change it: minted once (32 hex
// chars), persisted to system_settings, cached for the process lifetime.
//
// Deliberately NOT the Jellyfin server id — that one belongs to the
// compatibility protocol and a user could reasonably want to reset it without
// every first-party client forgetting the server.
//
// Passive mode neither reads nor mints: the DB is borrowed (usually
// production's), so the id in it belongs to THAT server. Adopting it would
// make a client treat this process and the real server as one installation,
// and writing one would let a dev box decide who the real server is. It uses
// a value derived from this host instead — stable across restarts, distinct
// per machine, persisted nowhere.
func (a *App) ServerID(ctx context.Context) string {
	serverIDOnce.Do(func() {
		cfg := a.ConfigSnapshot()
		if a.db == nil || (cfg != nil && cfg.PassiveMode.Value) {
			serverID = derivedServerID()
			return
		}
		if v, ok := readSetting[string](a, ctx, serverKeyID); ok && len(v) == 32 {
			serverID = v
			return
		}
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			// Degenerate fallback — only reachable if the kernel CSPRNG is broken.
			copy(buf, []byte("heya-server-id!!"))
		}
		id := hex.EncodeToString(buf)
		_ = writeSetting(a, ctx, serverKeyID, id)
		serverID = id
	})
	return serverID
}

// derivedServerID is the non-persisting identity used when this process must
// not write to its database. Hashing the hostname keeps it stable across
// restarts and distinct per machine, which is all a client needs to tell two
// servers apart.
func derivedServerID() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "heya"
	}
	sum := sha256.Sum256([]byte("heya-server-id:" + host))
	return hex.EncodeToString(sum[:16])
}
