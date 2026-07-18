package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/remote"
	"github.com/rs/zerolog/log"
)

// RemoteUpdate is the DTO accepted by the API for runtime remote-access
// changes. Token semantics: empty means "keep whatever is stored" (the API
// never echoes the token back, so the FE can't round-trip it); to replace a
// token the user pastes a new one. Clearing happens implicitly when the
// provider is set to "".
type RemoteUpdate struct {
	Enabled     bool
	Port        int
	ACMEEmail   string
	DNSProvider string
	DNSToken    string
	Domain      string
	Subdomain   string
}

// system_settings keys for the UI-editable remote-access fields. CheckURL,
// CertDir and ACMECA intentionally are NOT here — they're env-only.
const (
	remoteKeyEnabled     = "remote.enabled"
	remoteKeyPort        = "remote.port"
	remoteKeyACMEEmail   = "remote.acme_email"
	remoteKeyDNSProvider = "remote.dns_provider"
	remoteKeyDNSToken    = "remote.dns_token"
	remoteKeyDomain      = "remote.domain"
	remoteKeySubdomain   = "remote.subdomain"
)

// Remote returns the remote-access manager (nil until serve.go wires it).
func (a *App) Remote() *remote.Manager {
	a.networkMu.RLock()
	defer a.networkMu.RUnlock()
	return a.remote
}
func (a *App) SetRemote(m *remote.Manager) {
	a.networkMu.Lock()
	a.remote = m
	a.networkMu.Unlock()
}

// SaveRemoteSettings persists the UI-editable remote fields, refusing any
// field whose effective value is env-locked (validate-all-then-write-all).
// The caller (handler) triggers the manager Enable/Disable after this
// returns — this method only handles persistence + the in-memory snapshot.
func (a *App) SaveRemoteSettings(ctx context.Context, u RemoteUpdate) error {
	u.DNSProvider = strings.ToLower(strings.TrimSpace(u.DNSProvider))
	u.Domain = strings.TrimSpace(strings.Trim(u.Domain, "."))
	u.Subdomain = strings.TrimSpace(strings.Trim(u.Subdomain, "."))

	switch u.DNSProvider {
	case "", "desec", "duckdns", "cloudflare":
	default:
		return fmt.Errorf("unknown DNS provider %q (want desec, duckdns or cloudflare)", u.DNSProvider)
	}
	if u.DNSProvider != "" && u.Domain == "" {
		return fmt.Errorf("DNS provider %q requires a domain", u.DNSProvider)
	}
	if u.Port != 0 && (u.Port < 1024 || u.Port > 65535) {
		return fmt.Errorf("remote port must be 0 (auto) or 1024-65535, got %d", u.Port)
	}

	// Serialize the DB overlay with readers and other settings saves so the
	// in-memory snapshot always describes the last completed persistence pass.
	a.configMu.Lock()
	defer a.configMu.Unlock()

	cur := a.config.Remote
	if err := errIfEnvLockedChanged(remoteKeyEnabled, cur.Enabled, u.Enabled); err != nil {
		return err
	}
	if u.Port != 0 {
		if err := errIfEnvLockedChanged(remoteKeyPort, cur.Port, u.Port); err != nil {
			return err
		}
	}
	if err := errIfEnvLockedChanged(remoteKeyACMEEmail, cur.ACMEEmail, u.ACMEEmail); err != nil {
		return err
	}
	if err := errIfEnvLockedChanged(remoteKeyDNSProvider, cur.DNSProvider, u.DNSProvider); err != nil {
		return err
	}
	if u.DNSToken != "" {
		if err := errIfEnvLockedChanged(remoteKeyDNSToken, cur.DNSToken, u.DNSToken); err != nil {
			return err
		}
	}
	if err := errIfEnvLockedChanged(remoteKeyDomain, cur.Domain, u.Domain); err != nil {
		return err
	}
	if err := errIfEnvLockedChanged(remoteKeySubdomain, cur.Subdomain, u.Subdomain); err != nil {
		return err
	}

	if err := persistAndOverlayField(a, ctx, remoteKeyEnabled, &a.config.Remote.Enabled, u.Enabled); err != nil {
		return err
	}
	if u.Port != 0 {
		if err := persistAndOverlayField(a, ctx, remoteKeyPort, &a.config.Remote.Port, u.Port); err != nil {
			return err
		}
	}
	if err := persistAndOverlayField(a, ctx, remoteKeyACMEEmail, &a.config.Remote.ACMEEmail, u.ACMEEmail); err != nil {
		return err
	}
	if err := persistAndOverlayField(a, ctx, remoteKeyDNSProvider, &a.config.Remote.DNSProvider, u.DNSProvider); err != nil {
		return err
	}
	if u.DNSToken != "" {
		if err := persistAndOverlayField(a, ctx, remoteKeyDNSToken, &a.config.Remote.DNSToken, u.DNSToken); err != nil {
			return err
		}
	}
	if err := persistAndOverlayField(a, ctx, remoteKeyDomain, &a.config.Remote.Domain, u.Domain); err != nil {
		return err
	}
	if err := persistAndOverlayField(a, ctx, remoteKeySubdomain, &a.config.Remote.Subdomain, u.Subdomain); err != nil {
		return err
	}
	return nil
}

// SaveAndApplyRemoteSettings keeps persistence order and asynchronous manager
// transition order identical across concurrent admin requests.
func (a *App) SaveAndApplyRemoteSettings(ctx context.Context, update RemoteUpdate) (config.RemoteConfig, error) {
	a.remoteSettingsMu.Lock()
	defer a.remoteSettingsMu.Unlock()
	if err := a.SaveRemoteSettings(ctx, update); err != nil {
		return config.RemoteConfig{}, err
	}
	return a.applyRemoteRuntimeLocked(ctx)
}

// ApplyRemoteRuntime applies the effective config already loaded at server
// boot. It returns after admitting the App-owned background transition.
func (a *App) ApplyRemoteRuntime(ctx context.Context) error {
	a.remoteSettingsMu.Lock()
	defer a.remoteSettingsMu.Unlock()
	_, err := a.applyRemoteRuntimeLocked(ctx)
	return err
}

func (a *App) applyRemoteRuntimeLocked(ctx context.Context) (config.RemoteConfig, error) {
	snapshot := a.ConfigSnapshot()
	if snapshot == nil {
		return config.RemoteConfig{}, fmt.Errorf("remote config is unavailable")
	}
	manager := a.Remote()
	if manager == nil {
		return config.RemoteConfig{}, fmt.Errorf("remote access manager is unavailable")
	}
	cur := snapshot.Remote
	var runtimeConfig remote.Config
	if cur.Enabled.Value {
		resolved, err := a.RemoteRuntimeConfig(ctx)
		if err != nil {
			return config.RemoteConfig{}, err
		}
		runtimeConfig = resolved
		// RemoteRuntimeConfig may have minted and persisted the first sticky port.
		cur = a.ConfigSnapshot().Remote
	}
	started := a.remoteTransition.Start(a, func(workCtx context.Context) {
		var err error
		if cur.Enabled.Value {
			err = manager.Enable(workCtx, runtimeConfig)
		} else {
			err = manager.Disable()
		}
		if err != nil && workCtx.Err() == nil {
			log.Warn().Err(err).Msg("remote access live config transition failed")
		}
	})
	if !started {
		return config.RemoteConfig{}, errAppClosing
	}
	return cur, nil
}

// LoadRemoteFromDB seeds the in-memory snapshot from system_settings.
// Called once at boot after config.Load(); env-set fields keep their env
// provenance. Skipped in passive mode alongside LoadTailscaleFromDB — a
// borrowed prod DB's remote.enabled must not open ports on a dev box.
func (a *App) LoadRemoteFromDB(ctx context.Context) {
	a.configMu.Lock()
	defer a.configMu.Unlock()

	overlayFieldFromDB(a, ctx, &a.config.Remote.Enabled, remoteKeyEnabled, nil)
	overlayFieldFromDB(a, ctx, &a.config.Remote.Port, remoteKeyPort, func(v int) bool { return v >= 1024 && v <= 65535 })
	overlayFieldFromDB(a, ctx, &a.config.Remote.ACMEEmail, remoteKeyACMEEmail, nil)
	overlayFieldFromDB(a, ctx, &a.config.Remote.DNSProvider, remoteKeyDNSProvider, nil)
	overlayFieldFromDB(a, ctx, &a.config.Remote.DNSToken, remoteKeyDNSToken, nil)
	overlayFieldFromDB(a, ctx, &a.config.Remote.Domain, remoteKeyDomain, nil)
	overlayFieldFromDB(a, ctx, &a.config.Remote.Subdomain, remoteKeySubdomain, nil)
}

// RemoteRuntimeConfig materializes the manager-facing config from the
// current snapshot, resolving Port==0 to a freshly generated random high
// port that is persisted immediately — the port lands in bookmarks and
// client configs, so it must survive restarts.
func (a *App) RemoteRuntimeConfig(ctx context.Context) (remote.Config, error) {
	a.configMu.Lock()
	defer a.configMu.Unlock()

	cur := a.config.Remote
	port := cur.Port.Value
	if port == 0 {
		p, err := randomHighPort()
		if err != nil {
			return remote.Config{}, fmt.Errorf("generating remote port: %w", err)
		}
		port = p
		if err := persistAndOverlayField(a, ctx, remoteKeyPort, &a.config.Remote.Port, port); err != nil {
			return remote.Config{}, fmt.Errorf("persisting generated remote port: %w", err)
		}
	}
	return remote.Config{
		Port:        port,
		CheckURL:    cur.CheckURL.Value,
		CertDir:     cur.CertDir.Value,
		ACMECA:      cur.ACMECA.Value,
		ACMEEmail:   cur.ACMEEmail.Value,
		DNSProvider: cur.DNSProvider.Value,
		DNSToken:    cur.DNSToken.Value,
		Domain:      cur.Domain.Value,
		Subdomain:   cur.Subdomain.Value,
	}, nil
}

// randomHighPort picks a port in [20000, 60000) — above every well-known
// range (no ISP blocks, no collisions with common services), below the
// ephemeral range some routers refuse to map.
func randomHighPort() (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(40000))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()) + 20000, nil
}
