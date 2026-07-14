package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"github.com/karbowiak/heya/internal/remote"
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
func (a *App) Remote() *remote.Manager     { return a.remote }
func (a *App) SetRemote(m *remote.Manager) { a.remote = m }

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

// LoadRemoteFromDB seeds the in-memory snapshot from system_settings.
// Called once at boot after config.Load(); env-set fields keep their env
// provenance. Skipped in passive mode alongside LoadTailscaleFromDB — a
// borrowed prod DB's remote.enabled must not open ports on a dev box.
func (a *App) LoadRemoteFromDB(ctx context.Context) {
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
