// Package tailscale wraps tsnet.Server in a Heya-shaped lifecycle:
// declarative Config, ergonomic listeners, structured status snapshots,
// and a Funnel toggle that can be flipped at runtime via the API.
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
	Hostname string
	AuthKey  string
	StateDir string
	HTTPS    bool
	Funnel   bool
}

// Status is the snapshot the UI / CLI read. The fields below are all that
// the rest of Heya cares about; everything richer comes from the LocalClient.
type Status struct {
	Running      bool      `json:"running"`
	Hostname     string    `json:"hostname"`
	BackendState string    `json:"backend_state"`
	MagicDNS     string    `json:"magic_dns,omitempty"`
	IPv4         string    `json:"ipv4,omitempty"`
	IPv6         string    `json:"ipv6,omitempty"`
	CertDomain   string    `json:"cert_domain,omitempty"`
	HTTPS        bool      `json:"https"`
	Funnel       bool      `json:"funnel"`
	LoginURL     string    `json:"login_url,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// StatusFn is called whenever the status snapshot changes — wire this to the
// event hub so the UI updates in real time without polling.
type StatusFn func(Status)

type Server struct {
	cfg      Config
	logger   zerolog.Logger
	onStatus StatusFn

	ts     *tsnet.Server
	status atomic.Pointer[Status]

	closeMu sync.Mutex
	closed  bool
}

func New(cfg Config, logger zerolog.Logger, onStatus StatusFn) *Server {
	s := &Server{cfg: cfg, logger: logger, onStatus: onStatus}
	s.publish(Status{
		Hostname:  cfg.Hostname,
		HTTPS:     cfg.HTTPS,
		Funnel:    cfg.Funnel,
		UpdatedAt: time.Now(),
	})
	return s
}

// Start brings up the tsnet node and blocks until it has a tailnet address
// (or the context is cancelled / Up errors). After Start returns nil the
// listeners can be opened.
func (s *Server) Start(ctx context.Context) error {
	if err := os.MkdirAll(s.cfg.StateDir, 0o700); err != nil {
		return fmt.Errorf("tailscale state dir: %w", err)
	}

	s.ts = &tsnet.Server{
		Hostname: s.cfg.Hostname,
		Dir:      s.cfg.StateDir,
		AuthKey:  s.cfg.AuthKey,
		Logf:     func(string, ...any) {}, // silence backend chatter
		UserLogf: s.userLog,
	}

	upCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	st, err := s.ts.Up(upCtx)
	if err != nil {
		s.recordError(err)
		return fmt.Errorf("tailscale Up: %w", err)
	}

	s.refreshStatus(st)
	go s.watchStatus(ctx)
	return nil
}

// Listen returns a plain HTTP listener on the tailnet only (port 80).
func (s *Server) Listen() (net.Listener, error) {
	if s.ts == nil {
		return nil, errors.New("tailscale: server not started")
	}
	return s.ts.Listen("tcp", ":80")
}

// ListenTLS returns an HTTPS listener using a Tailscale-issued cert for the
// node's MagicDNS name (port 443). Requires HTTPS to be enabled in the
// tailnet admin console.
func (s *Server) ListenTLS() (net.Listener, error) {
	if s.ts == nil {
		return nil, errors.New("tailscale: server not started")
	}
	lc, err := s.ts.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("tailscale local client: %w", err)
	}
	raw, err := s.ts.Listen("tcp", ":443")
	if err != nil {
		return nil, fmt.Errorf("tailscale :443 listen: %w", err)
	}
	tlsCfg := &tls.Config{GetCertificate: lc.GetCertificate}
	return tls.NewListener(raw, tlsCfg), nil
}

// ListenFunnel returns a listener that accepts both tailnet *and* public
// internet traffic via Tailscale Funnel. Always TLS-terminated by Tailscale.
func (s *Server) ListenFunnel() (net.Listener, error) {
	if s.ts == nil {
		return nil, errors.New("tailscale: server not started")
	}
	return s.ts.ListenFunnel("tcp", ":443")
}

// Status returns the most recent snapshot.
func (s *Server) Status() Status {
	if p := s.status.Load(); p != nil {
		return *p
	}
	return Status{Hostname: s.cfg.Hostname, HTTPS: s.cfg.HTTPS, Funnel: s.cfg.Funnel}
}

// SetFunnel flips Funnel on/off at runtime by re-binding the listener. The
// returned bool reflects the new state; on error the previous state is kept.
//
// Note: callers must restart their listener loop after this — the function
// signals intent and persists config; serve.go is responsible for re-Listen.
func (s *Server) SetFunnel(enabled bool) {
	s.cfg.Funnel = enabled
	cur := s.Status()
	cur.Funnel = enabled
	cur.UpdatedAt = time.Now()
	s.publish(cur)
}

// Logout clears the local tailnet identity (useful for re-onboarding under a
// different account). The next Start will require a fresh auth flow.
func (s *Server) Logout(ctx context.Context) error {
	if s.ts == nil {
		return errors.New("tailscale: server not started")
	}
	lc, err := s.ts.LocalClient()
	if err != nil {
		return err
	}
	return lc.Logout(ctx)
}

func (s *Server) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed || s.ts == nil {
		return nil
	}
	s.closed = true
	return s.ts.Close()
}

// userLog catches lines from tsnet (e.g. "To authenticate, visit: https://...")
// and pulls login URLs out so we can surface them in the UI.
func (s *Server) userLog(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	s.logger.Info().Msg(msg)

	if idx := indexOfLoginURL(msg); idx >= 0 {
		url := extractURL(msg[idx:])
		if url != "" {
			cur := s.Status()
			cur.LoginURL = url
			cur.UpdatedAt = time.Now()
			s.publish(cur)
		}
	}
}

// watchStatus polls the LocalClient on a 10s tick so IP/cert/backend-state
// changes propagate to the UI without restart.
func (s *Server) watchStatus(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.ts == nil {
				return
			}
			lc, err := s.ts.LocalClient()
			if err != nil {
				continue
			}
			st, err := lc.Status(ctx)
			if err != nil {
				continue
			}
			s.refreshStatus(st)
		}
	}
}

func (s *Server) refreshStatus(st *ipnstate.Status) {
	cur := s.Status()
	cur.UpdatedAt = time.Now()
	cur.Running = st != nil && st.BackendState == ipn.Running.String()
	if st != nil {
		cur.BackendState = st.BackendState
		if st.Self != nil {
			cur.MagicDNS = stripTrailingDot(st.Self.DNSName)
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
		if domains := s.ts.CertDomains(); len(domains) > 0 {
			cur.CertDomain = domains[0]
		}
	}
	s.publish(cur)
}

func (s *Server) recordError(err error) {
	cur := s.Status()
	cur.Running = false
	cur.LastError = err.Error()
	cur.UpdatedAt = time.Now()
	s.publish(cur)
}

func (s *Server) publish(st Status) {
	cp := st
	s.status.Store(&cp)
	if s.onStatus != nil {
		s.onStatus(cp)
	}
}

// LocalClient exposes the underlying tailnet LocalAPI for advanced callers
// (whois, ping, etc.). Returns nil-ish error if the server hasn't started.
func (s *Server) LocalClient() (*local.Client, error) {
	if s.ts == nil {
		return nil, errors.New("tailscale: server not started")
	}
	return s.ts.LocalClient()
}

// HTTPRedirector returns a handler suitable for the :80 listener when HTTPS
// is on — bounces every request to the same path on the cert domain.
func (s *Server) HTTPRedirector() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := s.Status().CertDomain
		if host == "" {
			host = r.Host
		}
		// Reconstruct from controlled host + path/query only; never use the
		// raw RequestURI as that's flagged by gosec G710 (open-redirect).
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
