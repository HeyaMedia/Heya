// Package remote implements Plex-style direct remote access for the Heya
// server: UPnP port mapping on the LAN router, a dedicated TLS listener
// serving the same handler as the LAN listener, per-server certificates via
// ACME DNS-01 against a user-supplied DNS provider (deSEC, DuckDNS,
// Cloudflare), and outside-in reachability verification through the
// heya.media connectivity-check service.
//
// The subsystem is production-only (no dev-proxy presence) and everything is
// driven through the Manager: serve.go constructs it, the settings handlers
// Enable/Disable it, and the maintenance loop keeps the port mapping leased,
// the wan. DNS record pointed at the current WAN IP, and the reachability
// verdict fresh.
package remote

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Config is the runtime configuration handed to Enable. It is fully
// materialized (port already resolved) — provenance and persistence live in
// the service layer, not here.
type Config struct {
	Port        int
	CheckURL    string
	CertDir     string
	ACMECA      string // ACME directory URL; "" = Let's Encrypt production
	ACMEEmail   string
	DNSProvider string // "" | "desec" | "duckdns" | "cloudflare"
	DNSToken    string
	Domain      string
	Subdomain   string
}

// Phase is the primary reachability state. DNS and certificate state are
// orthogonal and live in their own status blocks.
type Phase string

const (
	PhaseDisabled    Phase = "disabled"
	PhaseStarting    Phase = "starting"
	PhaseMapping     Phase = "mapping"
	PhaseProbing     Phase = "probing"
	PhaseReachable   Phase = "reachable"
	PhaseUnreachable Phase = "unreachable"
	// PhaseUnverified means the listener + mapping look fine locally but the
	// heya.media check service couldn't be reached to prove reachability
	// from outside.
	PhaseUnverified Phase = "unverified"
	PhaseError      Phase = "error"
)

type UPnPStatus struct {
	Available bool   `json:"available"`
	Gateway   string `json:"gateway,omitempty"`
	Error     string `json:"error,omitempty"`
	MappedAt  string `json:"mapped_at,omitempty"`
}

type DNSStatus struct {
	Provider   string `json:"provider,omitempty"`
	Configured bool   `json:"configured"`
	Zone       string `json:"zone,omitempty"`
	WANHost    string `json:"wan_host,omitempty"`
	LANHost    string `json:"lan_host,omitempty"`
	LastSyncAt string `json:"last_sync_at,omitempty"`
	Error      string `json:"error,omitempty"`
}

type CertStatus struct {
	Mode    string   `json:"mode"` // none | self_signed | acme
	Issuing bool     `json:"issuing"`
	SANs    []string `json:"sans,omitempty"`
	Expiry  string   `json:"expiry,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// CheckResult mirrors the heya.media /v1/check response (docs in the
// connectivity-check spec). Unavailable is client-side: the service itself
// couldn't be reached, so nothing was proven either way.
type CheckResult struct {
	ObservedIP  string      `json:"observed_ip,omitempty"`
	Reachable   bool        `json:"reachable"`
	Verified    bool        `json:"verified"`
	LatencyMS   int         `json:"latency_ms,omitempty"`
	Error       *CheckError `json:"error,omitempty"`
	Unavailable bool        `json:"unavailable,omitempty"`
}

type CheckError struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

type RemoteStatus struct {
	Enabled          bool         `json:"enabled"`
	Phase            Phase        `json:"phase"`
	Detail           string       `json:"detail,omitempty"`
	Port             int          `json:"port,omitempty"`
	LANIP            string       `json:"lan_ip,omitempty"`
	RouterExternalIP string       `json:"router_external_ip,omitempty"`
	ObservedIP       string       `json:"observed_ip,omitempty"`
	CGNAT            bool         `json:"cgnat"`
	UPnP             UPnPStatus   `json:"upnp"`
	DNS              DNSStatus    `json:"dns"`
	Cert             CertStatus   `json:"cert"`
	LastCheck        *CheckResult `json:"last_check,omitempty"`
	LastCheckAt      string       `json:"last_check_at,omitempty"`
	RemoteURL        string       `json:"remote_url,omitempty"`
	LANURL           string       `json:"lan_url,omitempty"`
}

// StatusFn receives a status snapshot on every meaningful transition —
// serve.go wires it to the event hub ("remote.status").
type StatusFn func(RemoteStatus)

// Manager owns the remote-access runtime. All mutating entry points are
// serialized by opMu so a Disable can't interleave with a half-finished
// Enable; status reads take only stateMu.
type Manager struct {
	handler  http.Handler
	log      zerolog.Logger
	onStatus StatusFn

	opMu sync.Mutex // serializes Enable/Disable/Close/Recheck bring-up work

	stateMu  sync.Mutex
	cfg      Config
	status   RemoteStatus
	names    dnsNames
	upnp     *upnpGateway
	certs    *certManager
	records  *recordSyncer
	probe    *probeClient
	listener *tlsListener

	challengeMu  sync.Mutex
	challenge    string
	challengeExp time.Time

	loopCancel  context.CancelFunc
	issueCancel context.CancelFunc
}

// NewManager builds a disabled manager. handler is the same root handler the
// LAN listener serves — remote clients see the identical API + SPA.
func NewManager(handler http.Handler, logger zerolog.Logger, onStatus StatusFn) *Manager {
	return &Manager{
		handler:  handler,
		log:      logger,
		onStatus: onStatus,
	}
}

// RemoteStatus returns a copy of the current status.
func (m *Manager) Status() RemoteStatus {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	return m.status
}

// update mutates status under lock and emits the resulting snapshot.
func (m *Manager) update(fn func(*RemoteStatus)) {
	m.stateMu.Lock()
	fn(&m.status)
	snap := m.status
	m.stateMu.Unlock()
	if m.onStatus != nil {
		m.onStatus(snap)
	}
}

// Enable brings the subsystem up (idempotent: an enabled manager is torn
// down and rebuilt, which is how config changes apply). Blocking — callers
// run it in a goroutine; progress streams via StatusFn.
func (m *Manager) Enable(ctx context.Context, cfg Config) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.stopLocked(false)

	if cfg.Port < 1024 || cfg.Port > 65535 {
		err := fmt.Errorf("invalid remote port %d", cfg.Port)
		m.update(func(s *RemoteStatus) { *s = RemoteStatus{Enabled: true, Phase: PhaseError, Detail: err.Error()} })
		return err
	}

	names := buildDNSNames(cfg)
	m.stateMu.Lock()
	m.cfg = cfg
	m.names = names
	m.status = RemoteStatus{
		Enabled: true,
		Phase:   PhaseStarting,
		Port:    cfg.Port,
		DNS: DNSStatus{
			Provider:   cfg.DNSProvider,
			Configured: names.configured,
			Zone:       names.zone,
			WANHost:    names.wanHost,
			LANHost:    names.lanHost,
		},
		Cert: CertStatus{Mode: "none"},
	}
	m.stateMu.Unlock()
	m.update(func(*RemoteStatus) {})

	lanIP := detectLANIP()
	m.update(func(s *RemoteStatus) { s.LANIP = lanIP })

	// Certificates + TLS listener first: the listener must answer before the
	// probe fires, and it works LAN-only even when UPnP/probing fail below.
	certs, err := newCertManager(cfg, names, m.log)
	if err != nil {
		m.update(func(s *RemoteStatus) { s.Phase = PhaseError; s.Detail = "certificate setup: " + err.Error() })
		return err
	}
	m.stateMu.Lock()
	m.certs = certs
	m.stateMu.Unlock()
	m.update(func(s *RemoteStatus) { s.Cert = certs.snapshotStatus() })

	lst, err := startTLSListener(cfg.Port, m.handler, certs)
	if err != nil {
		m.update(func(s *RemoteStatus) { s.Phase = PhaseError; s.Detail = "listener: " + err.Error() })
		return err
	}
	m.stateMu.Lock()
	m.listener = lst
	m.stateMu.Unlock()

	// DNS provider + managed-cert issuance, fully async: issuance can take
	// minutes on first run (DNS propagation), and remote access must not
	// wait on it — the self-signed fallback serves until the real cert
	// lands in the cache.
	if names.configured {
		syncer, err := newRecordSyncer(cfg, names)
		if err != nil {
			m.update(func(s *RemoteStatus) { s.DNS.Error = err.Error() })
		} else {
			m.stateMu.Lock()
			m.records = syncer
			m.stateMu.Unlock()
			if lanIP != "" && names.lanHost != "" {
				if addr, perr := netip.ParseAddr(lanIP); perr == nil {
					if serr := syncer.syncLAN(ctx, addr); serr != nil {
						m.update(func(s *RemoteStatus) { s.DNS.Error = "lan record: " + serr.Error() })
					} else {
						m.update(func(s *RemoteStatus) { s.DNS.LastSyncAt = nowRFC3339(); s.DNS.Error = "" })
					}
				}
			}
			issueCtx, cancel := context.WithCancel(ctx)
			m.issueCancel = cancel
			go m.issueLoop(issueCtx, certs)
		}
	}

	// UPnP mapping. Failure is not fatal: a manual port forward still makes
	// the probe succeed, so we always continue to the check.
	m.update(func(s *RemoteStatus) { s.Phase = PhaseMapping })
	gw, gwErr := discoverGateway()
	if gwErr != nil {
		m.log.Warn().Err(gwErr).Msg("UPnP gateway discovery failed")
		m.update(func(s *RemoteStatus) {
			s.UPnP = UPnPStatus{Available: false, Error: gwErr.Error()}
		})
	} else {
		m.stateMu.Lock()
		m.upnp = gw
		m.stateMu.Unlock()
		routerIP, _ := gw.externalIP()
		if err := gw.addMapping(cfg.Port, lanIP); err != nil {
			m.log.Warn().Err(err).Int("port", cfg.Port).Msg("UPnP port mapping failed")
			m.update(func(s *RemoteStatus) {
				s.RouterExternalIP = routerIP
				s.UPnP = UPnPStatus{Available: true, Gateway: gw.location(), Error: "mapping failed: " + err.Error()}
			})
		} else {
			m.update(func(s *RemoteStatus) {
				s.RouterExternalIP = routerIP
				s.UPnP = UPnPStatus{Available: true, Gateway: gw.location(), MappedAt: nowRFC3339()}
			})
		}
	}

	m.stateMu.Lock()
	m.probe = newProbeClient(cfg.CheckURL)
	m.stateMu.Unlock()

	m.runCheck(ctx)

	loopCtx, cancel := context.WithCancel(ctx)
	m.loopCancel = cancel
	go m.maintenanceLoop(loopCtx)

	return nil
}

// Disable tears everything down including the router port mapping.
func (m *Manager) Disable() error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.stopLocked(true)
	m.update(func(s *RemoteStatus) { *s = RemoteStatus{Enabled: false, Phase: PhaseDisabled} })
	return nil
}

// Close tears down listeners and loops but leaves the port mapping in place:
// restarts (air, deploys) must not strand remote clients — the mapping is
// re-asserted on the next Enable.
func (m *Manager) Close() error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.stopLocked(false)
	return nil
}

// stopLocked tears down running components. Callers hold opMu.
func (m *Manager) stopLocked(unmap bool) {
	if m.loopCancel != nil {
		m.loopCancel()
		m.loopCancel = nil
	}
	if m.issueCancel != nil {
		m.issueCancel()
		m.issueCancel = nil
	}
	m.stateMu.Lock()
	lst := m.listener
	gw := m.upnp
	certs := m.certs
	port := m.cfg.Port
	m.listener = nil
	m.upnp = nil
	m.certs = nil
	m.records = nil
	m.probe = nil
	m.stateMu.Unlock()

	if lst != nil {
		lst.close()
	}
	if certs != nil {
		certs.close()
	}
	if unmap && gw != nil && port != 0 {
		if err := gw.unmap(port); err != nil {
			m.log.Warn().Err(err).Int("port", port).Msg("UPnP unmap failed")
		}
	}
}

// Recheck re-asserts the port mapping and re-runs the outside-in check.
// Synchronous — the settings UI calls it from a button and shows the result.
func (m *Manager) Recheck(ctx context.Context) (RemoteStatus, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.stateMu.Lock()
	enabled := m.status.Enabled
	gw := m.upnp
	cfg := m.cfg
	lanIP := m.status.LANIP
	m.stateMu.Unlock()

	if !enabled {
		return m.Status(), errors.New("remote access is not enabled")
	}
	if gw != nil {
		if err := gw.addMapping(cfg.Port, lanIP); err != nil {
			m.update(func(s *RemoteStatus) { s.UPnP.Error = "mapping failed: " + err.Error() })
		} else {
			m.update(func(s *RemoteStatus) { s.UPnP.Error = ""; s.UPnP.MappedAt = nowRFC3339() })
		}
		if ip, err := gw.externalIP(); err == nil {
			m.update(func(s *RemoteStatus) { s.RouterExternalIP = ip })
		}
	}
	m.runCheck(ctx)
	return m.Status(), nil
}

// ProbeChallenge returns the in-flight check challenge, if one is current.
// Served by the public GET /api/connectivity/probe endpoint.
func (m *Manager) ProbeChallenge() (string, bool) {
	m.challengeMu.Lock()
	defer m.challengeMu.Unlock()
	if m.challenge == "" || time.Now().After(m.challengeExp) {
		return "", false
	}
	return m.challenge, true
}

// newChallenge mints and stores the nonce the heya.media prober will read
// back through the public probe endpoint. Valid for one check window.
func (m *Manager) newChallenge() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	c := hex.EncodeToString(buf)
	m.challengeMu.Lock()
	m.challenge = c
	m.challengeExp = time.Now().Add(2 * time.Minute)
	m.challengeMu.Unlock()
	return c
}

// clearChallenge drops the nonce as soon as its check round-trip ends —
// the public probe endpoint answers 404 outside an in-flight check.
func (m *Manager) clearChallenge() {
	m.challengeMu.Lock()
	m.challenge = ""
	m.challengeMu.Unlock()
}

// runCheck performs the outside-in verification: observed IP (+ CGNAT
// detection), then the full /v1/check round trip, then wan-record sync.
func (m *Manager) runCheck(ctx context.Context) {
	m.stateMu.Lock()
	probe := m.probe
	cfg := m.cfg
	syncer := m.records
	routerIP := m.status.RouterExternalIP
	m.stateMu.Unlock()
	if probe == nil {
		return
	}

	m.update(func(s *RemoteStatus) { s.Phase = PhaseProbing; s.Detail = "" })

	observed, ipErr := probe.observedIP(ctx)
	if ipErr == nil && observed != "" {
		m.update(func(s *RemoteStatus) {
			s.ObservedIP = observed
			s.CGNAT = isCGNAT(routerIP, observed)
		})
	}

	challenge := m.newChallenge()
	res := probe.check(ctx, cfg.Port, challenge)
	m.clearChallenge()

	now := nowRFC3339()
	m.update(func(s *RemoteStatus) {
		s.LastCheck = &res
		s.LastCheckAt = now
		if res.ObservedIP != "" {
			s.ObservedIP = res.ObservedIP
			s.CGNAT = isCGNAT(s.RouterExternalIP, res.ObservedIP)
		}
		switch {
		case res.Unavailable:
			s.Phase = PhaseUnverified
			s.Detail = "the connectivity check service could not be reached — port mapping looks OK locally but reachability is unproven"
		case res.Error != nil && res.Error.Code == "same_network":
			// The check service egresses behind the same router as this
			// server (hairpin probe) — it cannot see us from outside, so
			// the verdict is inconclusive rather than negative.
			s.Phase = PhaseUnverified
			s.Detail = "the check service is on the same network as this server and can't probe from outside — result inconclusive"
		case res.Reachable && res.Verified:
			s.Phase = PhaseReachable
			s.Detail = ""
		case res.Reachable && !res.Verified:
			s.Phase = PhaseUnreachable
			s.Detail = "a server answered on that port, but it isn't this one — the router forwards the port to a different device"
		default:
			s.Phase = PhaseUnreachable
			s.Detail = checkErrorDetail(res.Error, s.CGNAT)
		}
		s.RemoteURL, s.LANURL = buildURLs(m.names, s, cfg.Port)
	})

	// Keep wan. pointed at whatever the internet actually sees.
	if syncer != nil {
		m.stateMu.Lock()
		obs := m.status.ObservedIP
		m.stateMu.Unlock()
		if obs == "" {
			obs = routerIP
		}
		if addr, err := netip.ParseAddr(obs); err == nil {
			if err := syncer.syncWAN(ctx, addr); err != nil {
				m.update(func(s *RemoteStatus) { s.DNS.Error = "wan record: " + err.Error() })
			} else {
				m.update(func(s *RemoteStatus) { s.DNS.LastSyncAt = nowRFC3339(); s.DNS.Error = "" })
			}
		}
	}
}

// issueLoop runs managed-cert issuance and keeps cert status current. One
// run per Enable; certmagic maintains renewals internally afterwards.
func (m *Manager) issueLoop(ctx context.Context, certs *certManager) {
	m.update(func(s *RemoteStatus) { s.Cert.Issuing = true })
	err := certs.issue(ctx)
	if err != nil && ctx.Err() == nil {
		m.log.Warn().Err(err).Msg("ACME issuance failed")
	}
	m.update(func(s *RemoteStatus) {
		s.Cert = certs.snapshotStatus()
		if err != nil && ctx.Err() == nil {
			s.Cert.Error = err.Error()
		}
	})
}

// maintenanceLoop re-leases the UPnP mapping every 15 minutes, watches for
// WAN IP changes, and refreshes the reachability verdict hourly.
func (m *Manager) maintenanceLoop(ctx context.Context) {
	const tick = 15 * time.Minute
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	ticks := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ticks++
			m.stateMu.Lock()
			gw := m.upnp
			cfg := m.cfg
			lanIP := m.status.LANIP
			lastRouterIP := m.status.RouterExternalIP
			m.stateMu.Unlock()

			ipChanged := false
			if gw != nil {
				if err := gw.addMapping(cfg.Port, lanIP); err != nil {
					m.update(func(s *RemoteStatus) { s.UPnP.Error = "lease renewal failed: " + err.Error() })
				} else {
					m.update(func(s *RemoteStatus) { s.UPnP.Error = ""; s.UPnP.MappedAt = nowRFC3339() })
				}
				if ip, err := gw.externalIP(); err == nil && ip != lastRouterIP {
					ipChanged = true
					m.update(func(s *RemoteStatus) { s.RouterExternalIP = ip })
				}
			}
			if ipChanged || ticks%4 == 0 {
				m.runCheck(ctx)
			}
		}
	}
}

// checkErrorDetail maps probe error codes to actionable user-facing text.
func checkErrorDetail(ce *CheckError, cgnat bool) string {
	if cgnat {
		return "your ISP uses carrier-grade NAT — port forwarding cannot work on this connection; use Tailscale for remote access"
	}
	if ce == nil {
		return "unreachable from the internet"
	}
	switch ce.Code {
	case "timeout":
		return "no response from the internet side — the port isn't forwarded, or a firewall / your ISP is blocking it"
	case "connection_refused":
		return "the router forwarded the connection but nothing accepted it — check that the mapping points at this machine"
	case "tls_handshake":
		return "something answered on that port but it isn't speaking TLS — another service may own the port"
	case "challenge_mismatch":
		return "a server answered, but it isn't this one — the router forwards the port to a different device"
	default:
		if ce.Detail != "" {
			return ce.Detail
		}
		return "unreachable from the internet"
	}
}

// buildURLs derives the user-facing URLs from DNS config + phase.
func buildURLs(n dnsNames, s *RemoteStatus, port int) (remoteURL, lanURL string) {
	if n.configured {
		if n.wanHost != "" {
			remoteURL = fmt.Sprintf("https://%s:%d", n.wanHost, port)
		}
		if n.lanHost != "" {
			lanURL = fmt.Sprintf("https://%s:%d", n.lanHost, port)
		}
		return remoteURL, lanURL
	}
	// No DNS: bare-IP URL (self-signed cert — browsers will warn, native
	// clients can pin). Only meaningful when actually reachable.
	if s.Phase == PhaseReachable && s.ObservedIP != "" {
		remoteURL = fmt.Sprintf("https://%s:%d", s.ObservedIP, port)
	}
	return remoteURL, lanURL
}

// isCGNAT reports whether the router's WAN IP and the internet-observed IP
// disagree (classic CGNAT tell), or the router WAN IP is in a shared/private
// range (RFC 1918, RFC 6598 100.64/10).
func isCGNAT(routerIP, observedIP string) bool {
	r, err := netip.ParseAddr(routerIP)
	if err != nil {
		return false
	}
	if r.IsPrivate() || inCGNATRange(r) {
		return true
	}
	if observedIP == "" {
		return false
	}
	o, err := netip.ParseAddr(observedIP)
	if err != nil {
		return false
	}
	return r != o
}

var cgnatPrefix = netip.MustParsePrefix("100.64.0.0/10")

func inCGNATRange(a netip.Addr) bool {
	return a.Is4() && cgnatPrefix.Contains(a)
}

// detectLANIP finds the interface IP routed toward the internet (and thus
// toward the router) via the connected-UDP trick — no packets are sent.
func detectLANIP() string {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close() //nolint:errcheck // defer-close on throwaway UDP socket
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

// dnsNames precomputes every name derived from the provider config.
type dnsNames struct {
	configured bool
	provider   string
	zone       string // the provider-managed zone, no trailing dot
	base       string // hostname base the cert covers (zone or sub.zone)
	wanHost    string
	lanHost    string
	sans       []string
	// relative record names within zone ("@" = apex)
	wanRel string
	lanRel string // "" = provider can't host a second record (DuckDNS)
}

func buildDNSNames(cfg Config) dnsNames {
	if cfg.DNSProvider == "" || cfg.Domain == "" {
		return dnsNames{}
	}
	n := dnsNames{configured: true, provider: cfg.DNSProvider, zone: cfg.Domain}
	sub := strings.Trim(cfg.Subdomain, ".")
	if cfg.DNSProvider == "duckdns" {
		// DuckDNS stores exactly one A record per domain — no lan/wan split.
		// The wildcard cert still covers *.domain, but every name resolves
		// to the single stored IP, so only the WAN side is useful.
		n.base = cfg.Domain
		n.wanHost = cfg.Domain
		n.wanRel = "@"
		n.sans = []string{cfg.Domain, "*." + cfg.Domain}
		return n
	}
	if sub != "" {
		n.base = sub + "." + cfg.Domain
		n.wanRel = "wan." + sub
		n.lanRel = "lan." + sub
	} else {
		n.base = cfg.Domain
		n.wanRel = "wan"
		n.lanRel = "lan"
	}
	n.wanHost = "wan." + n.base
	n.lanHost = "lan." + n.base
	n.sans = []string{n.base, "*." + n.base}
	return n
}
