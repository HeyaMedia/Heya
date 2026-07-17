// Package tailscale wraps tsnet.Server in a Heya-shaped lifecycle. It owns
// tailnet identity and supplies raw network listeners/certificates to Heya's
// embedded Caddy ingress; it deliberately does not run an HTTP server.
package tailscale

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"
)

type Config struct {
	Enabled  bool
	Hostname string
	AuthKey  string
	StateDir string
	HTTPS    bool
	Funnel   bool
}

// Status is the snapshot the UI / CLI read. The fields below are all that
// the rest of Heya cares about; everything richer comes from the LocalClient.
//
// HTTPS / Funnel reflect *intent* (the user-saved preference). HTTPSActive /
// FunnelActive reflect *reality* — whether the corresponding listener
// actually bound. Tailscale itself can refuse Funnel (tailnet ACLs / admin
// console settings); the toggle should stay on so the user knows what
// they asked for, but the "active" flag stays off and LastError carries
// the reason.
type Status struct {
	Enabled      bool      `json:"enabled"`
	Running      bool      `json:"running"`
	Hostname     string    `json:"hostname"`
	BackendState string    `json:"backend_state"`
	MagicDNS     string    `json:"magic_dns,omitempty"`
	IPv4         string    `json:"ipv4,omitempty"`
	IPv6         string    `json:"ipv6,omitempty"`
	CertDomain   string    `json:"cert_domain,omitempty"`
	HTTPS        bool      `json:"https"`
	HTTPSActive  bool      `json:"https_active"`
	HTTPSURL     string    `json:"https_url,omitempty"`
	Funnel       bool      `json:"funnel"`
	FunnelActive bool      `json:"funnel_active"`
	FunnelURL    string    `json:"funnel_url,omitempty"`
	LoginURL     string    `json:"login_url,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// RawStatus returns the live ipnstate.Status from tsnet's LocalClient.
// This is the same blob `tailscale status --json` would print — useful
// for the debug panel and for diagnosing tailnet-side problems
// (auth, ACL tag mismatches, Funnel allowlist, etc.).
func (s *Server) RawStatus(ctx context.Context) (*ipnstate.Status, error) {
	s.mu.Lock()
	ts := s.ts
	s.mu.Unlock()
	if ts == nil {
		return nil, errors.New("tailscale: node not running")
	}
	lc, err := ts.LocalClient()
	if err != nil {
		return nil, err
	}
	return lc.Status(ctx)
}

// StatusFn is called whenever the status snapshot changes — wire this to the
// event hub so the UI updates in real time without polling.
type StatusFn func(Status)

// IngressSource is the stable capability object handed to embedded Caddy.
// It intentionally contains no Server mutex: Caddy provisions listeners
// synchronously while Server lifecycle methods hold that lock.
type IngressSource struct {
	ts *tsnet.Server
	lc *local.Client
}

func (s *IngressSource) ListenTCP(address string) (net.Listener, error) {
	listenAddress, err := unspecifiedAddress(address)
	if err != nil {
		return nil, err
	}
	return s.ts.Listen("tcp", listenAddress)
}

func (s *IngressSource) ListenPacket(address string) (net.PacketConn, error) {
	return s.ts.ListenPacket("udp", address)
}

func (s *IngressSource) ListenFunnel(address string) (net.Listener, error) {
	listenAddress, err := unspecifiedAddress(address)
	if err != nil {
		return nil, err
	}
	return s.ts.ListenFunnel("tcp", listenAddress)
}

func (s *IngressSource) GetCertificate(_ context.Context, hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return s.lc.GetCertificate(hello)
}

func unspecifiedAddress(address string) (string, error) {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("tailscale listener address %q: %w", address, err)
	}
	return net.JoinHostPort("", port), nil
}

type IngressConfig struct {
	Address    string
	CertDomain string
	HTTPS      bool
	Funnel     bool
	Source     *IngressSource
}

type ApplyIngressFn func(context.Context, IngressConfig) error
type RemoveIngressFn func(context.Context) error

type Server struct {
	logger        zerolog.Logger
	onStatus      StatusFn
	applyIngress  ApplyIngressFn
	removeIngress RemoveIngressFn

	mu            sync.Mutex
	cfg           Config
	ts            *tsnet.Server
	ingressSource *IngressSource
	ingressActive bool
	ingressError  string
	watchCancel   context.CancelFunc

	status atomic.Pointer[Status]
	closed bool
}

func New(logger zerolog.Logger, onStatus StatusFn, applyIngress ApplyIngressFn, removeIngress RemoveIngressFn) *Server {
	s := &Server{
		logger: logger, onStatus: onStatus,
		applyIngress: applyIngress, removeIngress: removeIngress,
	}
	s.publish(Status{UpdatedAt: time.Now()})
	return s
}

// Enable brings the tsnet node up under the given config and attaches it to
// Caddy. Safe to call when already enabled — it restarts the node only if
// identity settings changed; HTTPS/Funnel become atomic Caddy reloads.
//
// Returns nil immediately if cfg.Enabled is false (use Disable instead).
func (s *Server) Enable(ctx context.Context, cfg Config) error {
	if !cfg.Enabled {
		return errors.New("tailscale: Enable called with Enabled=false")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("tailscale: server has been closed")
	}

	prev := s.cfg
	s.cfg = cfg
	s.publishLocked(s.snapshotLocked())

	// Hostname / state-dir change requires a node restart. Funnel + HTTPS
	// are listener-level — they don't need a fresh tsnet node.
	needNodeRestart := s.ts == nil ||
		prev.Hostname != cfg.Hostname ||
		prev.StateDir != cfg.StateDir ||
		(cfg.AuthKey != "" && prev.AuthKey != cfg.AuthKey)

	if needNodeRestart {
		s.teardownLocked(ctx)
		if err := s.startNodeLocked(ctx); err != nil {
			s.recordErrorLocked(err)
			return err
		}
	}

	return s.applyIngressLocked(ctx)
}

// Disable closes the listeners and shuts the tsnet node down. The state dir
// is preserved so the next Enable can resume without re-onboarding.
func (s *Server) Disable() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Enabled = false
	s.teardownLocked(context.Background())
	st := s.snapshotLocked()
	st.Running = false
	st.BackendState = ""
	st.HTTPSActive = false
	st.HTTPSURL = ""
	st.FunnelActive = false
	st.FunnelURL = ""
	st.LoginURL = ""
	s.publishLocked(st)
	return nil
}

// SetFunnel flips Funnel on/off at runtime and reloads Caddy's tailnet edge.
// No-op if the node isn't currently enabled.
func (s *Server) SetFunnel(ctx context.Context, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ts == nil {
		s.cfg.Funnel = on
		s.publishLocked(s.snapshotLocked())
		return nil
	}
	s.cfg.Funnel = on
	return s.applyIngressLocked(ctx)
}

// SetHTTPS flips HTTPS on/off and rebinds listeners.
func (s *Server) SetHTTPS(ctx context.Context, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ts == nil {
		s.cfg.HTTPS = on
		s.publishLocked(s.snapshotLocked())
		return nil
	}
	s.cfg.HTTPS = on
	return s.applyIngressLocked(ctx)
}

// Status returns the most recent snapshot.
func (s *Server) Status() Status {
	if p := s.status.Load(); p != nil {
		return *p
	}
	return Status{}
}

// Logout clears the local tailnet identity (useful for re-onboarding under a
// different account). Implicitly disables the node — the next Enable will
// require a fresh auth flow.
func (s *Server) Logout(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ts == nil {
		return errors.New("tailscale: node not running")
	}
	lc, err := s.ts.LocalClient()
	if err != nil {
		return err
	}
	if err := lc.Logout(ctx); err != nil {
		return err
	}
	s.teardownLocked(ctx)
	s.cfg.Enabled = false
	s.publishLocked(s.snapshotLocked())
	return nil
}

// Close permanently shuts down the server. After Close, Enable returns
// an error.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.teardownLocked(context.Background())
	return nil
}

// startNodeLocked is the actual tsnet up. Caller must hold s.mu.
func (s *Server) startNodeLocked(ctx context.Context) error {
	if err := os.MkdirAll(s.cfg.StateDir, 0o700); err != nil {
		return fmt.Errorf("tailscale state dir: %w", err)
	}

	s.ts = &tsnet.Server{
		Hostname: s.cfg.Hostname,
		Dir:      s.cfg.StateDir,
		AuthKey:  s.cfg.AuthKey,
		Logf:     func(string, ...any) {},
		UserLogf: s.userLog,
	}

	upCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	st, err := s.ts.Up(upCtx)
	if err != nil {
		_ = s.ts.Close()
		s.ts = nil
		return fmt.Errorf("tailscale Up: %w", err)
	}
	lc, err := s.ts.LocalClient()
	if err != nil {
		_ = s.ts.Close()
		s.ts = nil
		return fmt.Errorf("tailscale LocalClient: %w", err)
	}
	s.ingressSource = &IngressSource{ts: s.ts, lc: lc}

	watchCtx, watchCancel := context.WithCancel(context.Background())
	s.watchCancel = watchCancel
	go s.watchStatus(watchCtx)

	s.refreshFromIPN(st)
	return nil
}

// applyIngressLocked gives Caddy the current tailnet capability and publishes
// listener reality only after Caddy has accepted the complete config.
func (s *Server) applyIngressLocked(ctx context.Context) error {
	if s.ts == nil || s.ingressSource == nil {
		return nil
	}
	cur := s.snapshotLocked()

	// The one-shot CLI intentionally has no ingress runtime attached.
	if s.applyIngress == nil {
		s.publishLocked(cur)
		return nil
	}
	address := cur.IPv4
	if address == "" {
		address = cur.IPv6
	}
	if address == "" {
		err := errors.New("tailscale: node has no tailnet address")
		s.recordIngressErrorLocked(cur, err)
		return err
	}
	if (s.cfg.HTTPS || s.cfg.Funnel) && cur.CertDomain == "" {
		err := errors.New("tailscale: HTTPS or Funnel is enabled but this tailnet has no certificate domain")
		s.recordIngressErrorLocked(cur, err)
		return err
	}

	err := s.applyIngress(ctx, IngressConfig{
		Address: address, CertDomain: cur.CertDomain, HTTPS: s.cfg.HTTPS,
		Funnel: s.cfg.Funnel, Source: s.ingressSource,
	})
	if err != nil {
		s.recordIngressErrorLocked(cur, err)
		return err
	}
	s.ingressActive = true
	s.ingressError = ""
	cur.LastError = ""
	cur.HTTPSActive = false
	cur.HTTPSURL = ""
	cur.FunnelActive = false
	cur.FunnelURL = ""
	cur.HTTPSActive = s.cfg.HTTPS || s.cfg.Funnel
	if s.cfg.Funnel {
		cur.FunnelActive = true
		cur.FunnelURL = httpsURLFor(cur.CertDomain)
	} else if s.cfg.HTTPS {
		cur.HTTPSURL = httpsURLFor(cur.CertDomain)
	}
	cur.UpdatedAt = time.Now()
	s.publishLocked(cur)
	return nil
}

func (s *Server) recordIngressErrorLocked(cur Status, err error) {
	s.logger.Warn().Err(err).Msg("Caddy tailnet ingress update failed")
	s.ingressError = "ingress: " + err.Error()
	cur.LastError = s.ingressError
	cur.UpdatedAt = time.Now()
	s.publishLocked(cur)
}

func httpsURLFor(certDomain string) string {
	if certDomain == "" {
		return ""
	}
	return "https://" + certDomain
}

// teardownLocked detaches Caddy before closing the tsnet node, so no active
// Caddy listener can retain a dead virtual-network socket.
func (s *Server) teardownLocked(ctx context.Context) {
	if s.ingressActive && s.removeIngress != nil {
		removeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := s.removeIngress(removeCtx); err != nil {
			s.logger.Warn().Err(err).Msg("Caddy tailnet ingress removal failed")
		}
		cancel()
	}
	s.ingressActive = false
	s.ingressError = ""
	s.ingressSource = nil
	if s.watchCancel != nil {
		s.watchCancel()
		s.watchCancel = nil
	}
	if s.ts != nil {
		_ = s.ts.Close()
		s.ts = nil
	}
}

// userLog catches lines from tsnet (e.g. "To authenticate, visit: https://...")
// and pulls login URLs out so we can surface them in the UI.
func (s *Server) userLog(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	s.logger.Info().Msg(msg)

	if idx := indexOfLoginURL(msg); idx >= 0 {
		login := extractURL(msg[idx:])
		if login != "" {
			cur := s.Status()
			cur.LoginURL = login
			cur.UpdatedAt = time.Now()
			s.publish(cur)
		}
	}
}

// watchStatus polls the LocalClient on a 5s tick so IP/cert/backend-state
// changes propagate to the UI without restart.
func (s *Server) watchStatus(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			ts := s.ts
			s.mu.Unlock()
			if ts == nil {
				return
			}
			lc, err := ts.LocalClient()
			if err != nil {
				continue
			}
			st, err := lc.Status(ctx)
			if err != nil {
				continue
			}
			s.mu.Lock()
			before := s.snapshotLocked()
			s.refreshFromIPN(st)
			after := s.snapshotLocked()
			addressChanged := before.IPv4 != after.IPv4 || before.IPv6 != after.IPv6 || before.CertDomain != after.CertDomain
			if s.cfg.Enabled && addressChanged {
				_ = s.applyIngressLocked(ctx)
			}
			s.mu.Unlock()
		}
	}
}

// refreshFromIPN updates the published Status from a tailscale ipnstate.Status
// reading. Caller must hold s.mu.
func (s *Server) refreshFromIPN(st *ipnstate.Status) {
	cur := s.snapshotLocked()
	cur.UpdatedAt = time.Now()
	cur.Running = st != nil && st.BackendState == ipn.Running.String()
	if st != nil {
		cur.BackendState = st.BackendState
		if st.Self != nil {
			cur.MagicDNS = stripTrailingDot(st.Self.DNSName)
			cur.IPv4 = ""
			cur.IPv6 = ""
			for _, ip := range st.Self.TailscaleIPs {
				if ip.Is4() && cur.IPv4 == "" {
					cur.IPv4 = ip.String()
				}
				if ip.Is6() && cur.IPv6 == "" {
					cur.IPv6 = ip.String()
				}
			}
		}
		if st.AuthURL != "" {
			cur.LoginURL = st.AuthURL
		} else if cur.Running {
			cur.LoginURL = ""
		}
	}
	if cur.Running {
		cur.LastError = s.ingressError
		if s.ts != nil {
			if domains := s.ts.CertDomains(); len(domains) > 0 {
				cur.CertDomain = domains[0]
			}
		}
	}
	s.publishLocked(cur)
}

// snapshotLocked builds a Status from the current cfg. Caller must hold s.mu.
func (s *Server) snapshotLocked() Status {
	if p := s.status.Load(); p != nil {
		cp := *p
		cp.Enabled = s.cfg.Enabled
		cp.Hostname = s.cfg.Hostname
		cp.HTTPS = s.cfg.HTTPS
		cp.Funnel = s.cfg.Funnel
		return cp
	}
	return Status{
		Enabled:   s.cfg.Enabled,
		Hostname:  s.cfg.Hostname,
		HTTPS:     s.cfg.HTTPS,
		Funnel:    s.cfg.Funnel,
		UpdatedAt: time.Now(),
	}
}

func (s *Server) recordErrorLocked(err error) {
	cur := s.snapshotLocked()
	cur.Running = false
	cur.LastError = err.Error()
	cur.UpdatedAt = time.Now()
	s.publishLocked(cur)
}

func (s *Server) publish(st Status) {
	cp := st
	s.status.Store(&cp)
	if s.onStatus != nil {
		s.onStatus(cp)
	}
}

// publishLocked is publish() called from inside the lock — same behavior, just
// named to make the locking discipline explicit at the call site.
func (s *Server) publishLocked(st Status) {
	s.publish(st)
}

// LocalClient exposes the underlying tailnet LocalAPI for advanced callers
// (whois, ping, etc.). Returns an error if the server isn't enabled.
func (s *Server) LocalClient() (*local.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ts == nil {
		return nil, errors.New("tailscale: node not running")
	}
	return s.ts.LocalClient()
}

func indexOfLoginURL(s string) int {
	for i := 0; i+8 <= len(s); i++ {
		if s[i:i+8] == "https://" {
			return i
		}
	}
	return -1
}

func extractURL(s string) string {
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return s[:i]
		}
	}
	return s
}

func stripTrailingDot(s string) string {
	if len(s) > 0 && s[len(s)-1] == '.' {
		return s[:len(s)-1]
	}
	return s
}
