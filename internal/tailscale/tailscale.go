// Package tailscale wraps tsnet.Server in a Heya-shaped lifecycle:
// declarative Config, ergonomic listeners, and hot Enable/Disable so the
// node can be brought up and torn down from the UI without restarting
// the whole binary. The Server owns the full listener lifecycle — it
// manages the tsnet node, the HTTP server(s) bound to it, and the
// LocalClient status poller as a single unit.
package tailscale

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
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

type Server struct {
	handler  http.Handler
	logger   zerolog.Logger
	onStatus StatusFn

	mu          sync.Mutex
	cfg         Config
	ts          *tsnet.Server
	httpServers []*http.Server
	listeners   []net.Listener
	watchCancel context.CancelFunc

	status atomic.Pointer[Status]
	closed bool
}

func New(handler http.Handler, logger zerolog.Logger, onStatus StatusFn) *Server {
	s := &Server{handler: handler, logger: logger, onStatus: onStatus}
	s.publish(Status{UpdatedAt: time.Now()})
	return s
}

// Enable brings the tsnet node up under the given config and binds the
// configured listeners. Safe to call when already enabled — it'll restart
// the node only if hostname / state-dir actually changed, otherwise it just
// rebuilds the listener set (cheaper).
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
		s.teardownLocked()
		if err := s.startNodeLocked(ctx); err != nil {
			s.recordErrorLocked(err)
			return err
		}
	} else {
		s.closeListenersLocked()
	}

	s.openListenersLocked()
	return nil
}

// Disable closes the listeners and shuts the tsnet node down. The state dir
// is preserved so the next Enable can resume without re-onboarding.
func (s *Server) Disable() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Enabled = false
	s.teardownLocked()
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

// SetFunnel flips Funnel on/off at runtime and rebinds the :443 listener.
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
	s.closeListenersLocked()
	s.openListenersLocked()
	return nil
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
	s.closeListenersLocked()
	s.openListenersLocked()
	return nil
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
	s.teardownLocked()
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
	s.teardownLocked()
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

	watchCtx, watchCancel := context.WithCancel(context.Background())
	s.watchCancel = watchCancel
	go s.watchStatus(watchCtx)

	s.refreshFromIPN(st)
	return nil
}

// openListenersLocked binds tailnet :80 / :443 / Funnel based on cfg and
// kicks off http.Server goroutines for each. Caller must hold s.mu.
//
// Each addListener call returns whether the listener actually opened.
// The :443 binding decides whether HTTPSActive / FunnelActive end up true.
func (s *Server) openListenersLocked() {
	if s.ts == nil {
		return
	}

	// Reset listener-derived status before re-binding. LastError will be
	// re-populated below if any listener fails; *Active flags start false
	// and flip true only if the corresponding bind succeeded.
	cur := s.snapshotLocked()
	cur.LastError = ""
	cur.HTTPSActive = false
	cur.HTTPSURL = ""
	cur.FunnelActive = false
	cur.FunnelURL = ""
	s.publishLocked(cur)

	addListener := func(label string, makeListener func() (net.Listener, error), handler http.Handler) bool {
		ln, err := makeListener()
		if err != nil {
			s.logger.Warn().Err(err).Str("listener", label).Msg("listener failed to open")
			// Surface to the UI. The most common cause is Funnel not being
			// enabled for the tailnet, or HTTPS not being enabled in the
			// admin console — both need user action in the Tailscale UI.
			cur := s.snapshotLocked()
			if cur.LastError == "" {
				cur.LastError = label + ": " + err.Error()
				cur.UpdatedAt = time.Now()
				s.publishLocked(cur)
			}
			return false
		}
		srv := &http.Server{Handler: handler, ReadHeaderTimeout: 15 * time.Second}
		s.listeners = append(s.listeners, ln)
		s.httpServers = append(s.httpServers, srv)
		go func() {
			s.logger.Info().Str("listener", label).Msg("listener up")
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
				s.logger.Warn().Err(err).Str("listener", label).Msg("listener stopped")
			}
		}()
		return true
	}

	switch {
	case s.cfg.Funnel:
		ok := addListener("tailscale-funnel:443",
			func() (net.Listener, error) { return s.ts.ListenFunnel("tcp", ":443") },
			s.handler)
		addListener("tailscale-redirect:80",
			func() (net.Listener, error) { return s.ts.Listen("tcp", ":80") },
			s.httpRedirectorLocked())
		if ok {
			cur := s.snapshotLocked()
			cur.FunnelActive = true
			cur.FunnelURL = httpsURLFor(cur.CertDomain)
			// The Funnel listener also terminates TLS on :443 for tailnet
			// members, so HTTPS is effectively up too. Don't set HTTPSURL
			// though — FunnelURL is the same URL and covers both audiences.
			cur.HTTPSActive = true
			cur.UpdatedAt = time.Now()
			s.publishLocked(cur)
		}

	case s.cfg.HTTPS:
		ok := addListener("tailscale-https:443",
			func() (net.Listener, error) {
				lc, err := s.ts.LocalClient()
				if err != nil {
					return nil, err
				}
				raw, err := s.ts.Listen("tcp", ":443")
				if err != nil {
					return nil, err
				}
				return tls.NewListener(raw, &tls.Config{GetCertificate: lc.GetCertificate}), nil
			},
			s.handler)
		addListener("tailscale-redirect:80",
			func() (net.Listener, error) { return s.ts.Listen("tcp", ":80") },
			s.httpRedirectorLocked())
		if ok {
			cur := s.snapshotLocked()
			cur.HTTPSActive = true
			cur.HTTPSURL = httpsURLFor(cur.CertDomain)
			cur.UpdatedAt = time.Now()
			s.publishLocked(cur)
		}

	default:
		addListener("tailscale-http:80",
			func() (net.Listener, error) { return s.ts.Listen("tcp", ":80") },
			s.handler)
	}
}

func httpsURLFor(certDomain string) string {
	if certDomain == "" {
		return ""
	}
	return "https://" + certDomain
}

// teardownLocked tears down the listeners AND the tsnet node. Caller must
// hold s.mu.
func (s *Server) teardownLocked() {
	s.closeListenersLocked()
	if s.watchCancel != nil {
		s.watchCancel()
		s.watchCancel = nil
	}
	if s.ts != nil {
		_ = s.ts.Close()
		s.ts = nil
	}
}

// closeListenersLocked closes the listeners + http.Servers but leaves the
// tsnet node up. Caller must hold s.mu.
func (s *Server) closeListenersLocked() {
	for _, srv := range s.httpServers {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = srv.Shutdown(shutdownCtx)
		cancel()
	}
	for _, ln := range s.listeners {
		_ = ln.Close()
	}
	s.httpServers = nil
	s.listeners = nil
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
			s.refreshFromIPN(st)
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
		cur.LastError = ""
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

// httpRedirectorLocked returns a handler that bounces requests to the cert
// domain on HTTPS. Caller must hold s.mu (reads s.ts indirectly via Status).
func (s *Server) httpRedirectorLocked() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := s.Status().CertDomain
		if host == "" {
			host = r.Host
		}
		// Build target from controlled host + path/query (gosec G710).
		u := &url.URL{Scheme: "https", Host: host, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
	})
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
