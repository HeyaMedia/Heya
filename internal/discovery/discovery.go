// Package discovery advertises this Heya server on the local network over
// mDNS / DNS-SD (RFC 6762 + 6763) so first-party clients — HeyaClient,
// HeyaTV — can find it without the user typing a URL.
//
// The advertisement is a single DNS-SD service instance:
//
//	<instance>._heya._tcp.local.   SRV → <host>.local.:<port>
//	                               TXT → v/id/name/ver/scheme/path/api[/remote]
//
// A client browses `_heya._tcp`, reads the TXT record to learn the scheme
// and API base, and confirms the find with an unauthenticated
// GET /api/server/info (see internal/server/discovery_huma.go) before
// offering the server to the user.
//
// Advertising only. Heya never *browses* for other Heya servers — that is
// the client's job, and internal/cast already owns the browse side for
// AirPlay/Chromecast receivers.
//
// The subsystem is deliberately available in dev as well as production
// (unlike tailscale/remote): mDNS is a LAN-scoped, read-only broadcast that
// opens no ports and grants no access, and being able to point a real client
// at `heya serve --dev-backend` is the whole point of building it.
package discovery

import (
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/zeroconf/v2"
	"github.com/rs/zerolog"
)

const (
	// ServiceType is the DNS-SD service Heya publishes and clients browse.
	// Registered nowhere official — `_heya._tcp` is ours by convention, the
	// same way Plex uses `_plexmediasvr._tcp`.
	ServiceType = "_heya._tcp"
	// Domain is the multicast DNS domain. Always "local." — mDNS has no
	// other one. This is the form to browse with.
	Domain = "local."
	// TXTVersion is the schema version of the TXT record below. Clients MUST
	// check it and ignore instances they don't understand, so we can change
	// the record later without breaking installed clients.
	TXTVersion = "1"
)

// Config is the fully-resolved runtime configuration handed to Advertise.
// Provenance and persistence live in the service layer; by the time it gets
// here every value is final.
type Config struct {
	// Instance is the human-readable service instance name — what a client
	// shows in its "servers found" list. Must be unique on the LAN; two
	// Heya servers sharing one produce a DNS-SD name conflict.
	Instance string
	Port     int
	HTTPS    bool
	// ServerID is the stable per-install identity. Clients key saved-server
	// entries on it so a rename or an IP change doesn't read as a new server.
	ServerID string
	Version  string
	// RemoteURL is the public https://… origin when direct remote access is
	// configured, so a client provisioned on the LAN also knows how to reach
	// this server from outside. Empty when remote access is off.
	RemoteURL string

	// --- Container/NAT escape hatches (env-only) ---

	// Host overrides the advertised hostname (default: os.Hostname). Together
	// with Addresses it switches registration to zeroconf's proxy mode, which
	// publishes records for a machine *other than* the one running this
	// process — the shape you need when Heya runs in a container/pod whose own
	// IP is unreachable from the LAN.
	Host string
	// Addresses are the literal IPs to advertise. Empty = derive from the
	// interfaces we bind.
	Addresses []string
	// Interfaces pins multicast to specific NIC names. Empty = every
	// multicast-capable interface, which is right on a normal host and wrong
	// on a box with a dozen virtual bridges.
	Interfaces []string
}

// AdvertisementStatus is the observability snapshot surfaced by /api/discovery/status and
// the settings UI.
type AdvertisementStatus struct {
	Enabled     bool   `json:"enabled"`
	Advertising bool   `json:"advertising"`
	Instance    string `json:"instance,omitempty"`
	ServiceType string `json:"service_type"`
	Domain      string `json:"domain"`
	Hostname    string `json:"hostname,omitempty"`
	Port        int    `json:"port,omitempty"`
	// TXT is the exact record on the wire — the fastest way to answer "why
	// isn't my client picking the right URL?" without a packet capture.
	TXT        []string `json:"txt,omitempty"`
	Interfaces []string `json:"interfaces,omitempty"`
	Addresses  []string `json:"addresses,omitempty"`
	StartedAt  string   `json:"started_at,omitempty"`
	LastError  string   `json:"last_error,omitempty"`
}

// StatusFn receives a snapshot on every transition — serve.go wires it to the
// event hub ("discovery.status").
type StatusFn func(AdvertisementStatus)

// Manager owns the mDNS responder. Advertise/Stop/Close are serialized by
// opMu; status reads take only stateMu.
type Manager struct {
	log      zerolog.Logger
	onStatus StatusFn

	opMu sync.Mutex

	stateMu sync.Mutex
	status  AdvertisementStatus
	server  *zeroconf.Server
}

// NewManager builds an idle manager that advertises nothing until Advertise
// is called.
func NewManager(logger zerolog.Logger, onStatus StatusFn) *Manager {
	return &Manager{
		log:      logger,
		onStatus: onStatus,
		status:   AdvertisementStatus{ServiceType: ServiceType, Domain: Domain},
	}
}

// Status returns a copy of the current status.
func (m *Manager) Status() AdvertisementStatus {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	return m.status
}

func (m *Manager) update(fn func(*AdvertisementStatus)) {
	m.stateMu.Lock()
	fn(&m.status)
	snap := m.status
	m.stateMu.Unlock()
	if m.onStatus != nil {
		m.onStatus(snap)
	}
}

// Advertise (re)publishes the service instance. Idempotent by teardown: an
// already-advertising manager is shut down and re-registered, which is how a
// name or port change takes effect. Registration is non-blocking — zeroconf
// spawns its own responder goroutines.
func (m *Manager) Advertise(cfg Config) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.shutdownLocked()

	instance := sanitizeInstance(cfg.Instance)
	if instance == "" {
		instance = "Heya"
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		err := fmt.Errorf("invalid discovery port %d", cfg.Port)
		m.update(func(s *AdvertisementStatus) {
			*s = AdvertisementStatus{ServiceType: ServiceType, Domain: Domain, Enabled: true, LastError: err.Error()}
		})
		return err
	}

	selected, ifaceNames, pinned, err := resolveInterfaces(cfg.Interfaces)
	if err != nil {
		m.update(func(s *AdvertisementStatus) {
			*s = AdvertisementStatus{ServiceType: ServiceType, Domain: Domain, Enabled: true, LastError: err.Error()}
		})
		return err
	}
	// Only hand zeroconf an explicit interface list when the operator pinned
	// one; otherwise let it apply its own default so we never diverge from it.
	var ifaces []net.Interface
	if pinned {
		ifaces = selected
	}

	host := hostLabel(cfg.Host)
	if host == "" {
		osHost, _ := os.Hostname()
		host = hostLabel(osHost)
	}
	if host == "" {
		err := fmt.Errorf("no hostname could be resolved to advertise under (set HEYA_DISCOVERY_HOST)")
		m.update(func(s *AdvertisementStatus) {
			*s = AdvertisementStatus{ServiceType: ServiceType, Domain: Domain, Enabled: true, LastError: err.Error()}
		})
		return err
	}

	addresses := trimAll(cfg.Addresses)
	if len(addresses) == 0 {
		addresses = interfaceAddresses(selected)
	}
	if len(addresses) == 0 {
		err := fmt.Errorf("no usable address found on %s (set HEYA_DISCOVERY_ADDRESSES)", describeInterfaces(ifaceNames))
		m.update(func(s *AdvertisementStatus) {
			*s = AdvertisementStatus{ServiceType: ServiceType, Domain: Domain, Enabled: true, LastError: err.Error()}
		})
		return err
	}

	// Always proxy mode, even when advertising ourselves. Plain Register
	// derives the host name from os.Hostname() and only appends the `.local.`
	// suffix when it thinks one is missing — on a machine already called
	// `mac.local` that yields either `mac.local.local.` or, with an untrimmed
	// domain, a name with no trailing dot that miekg/dns refuses to pack, so
	// NOTHING is answered. Handing it a bare label plus explicit addresses
	// takes that guess away and is also what container deployments need.
	txt := BuildTXT(cfg)
	server, err := zeroconf.RegisterProxy(instance, ServiceType, Domain, cfg.Port, host, addresses, txt, ifaces)
	if err != nil {
		m.update(func(s *AdvertisementStatus) {
			*s = AdvertisementStatus{ServiceType: ServiceType, Domain: Domain, Enabled: true, LastError: err.Error()}
		})
		return fmt.Errorf("registering %s.%s: %w", instance, ServiceType, err)
	}

	m.stateMu.Lock()
	m.server = server
	m.stateMu.Unlock()

	m.update(func(s *AdvertisementStatus) {
		*s = AdvertisementStatus{
			ServiceType: ServiceType,
			Domain:      Domain,
			Enabled:     true,
			Advertising: true,
			Instance:    instance,
			Hostname:    host + "." + strings.TrimSuffix(Domain, "."),
			Port:        cfg.Port,
			TXT:         txt,
			Interfaces:  ifaceNames,
			Addresses:   addresses,
			StartedAt:   time.Now().UTC().Format(time.RFC3339),
		}
	})
	m.log.Info().
		Str("instance", instance).
		Str("service", ServiceType).
		Int("port", cfg.Port).
		Strs("interfaces", ifaceNames).
		Msg("advertising on the local network over mDNS")
	return nil
}

// Stop withdraws the advertisement, sending DNS-SD goodbye packets so clients
// drop the entry immediately instead of waiting out the TTL.
func (m *Manager) Stop() error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	stopped := m.shutdownLocked()
	m.update(func(s *AdvertisementStatus) {
		*s = AdvertisementStatus{ServiceType: ServiceType, Domain: Domain}
	})
	if stopped {
		m.log.Info().Msg("stopped advertising on the local network")
	}
	return nil
}

// Close is Stop with the signature the serve teardown expects.
func (m *Manager) Close() error { return m.Stop() }

// shutdownLocked tears down the responder if one is running and reports
// whether it did. Callers hold opMu.
func (m *Manager) shutdownLocked() bool {
	m.stateMu.Lock()
	server := m.server
	m.server = nil
	m.stateMu.Unlock()
	if server == nil {
		return false
	}
	server.Shutdown()
	return true
}

// BuildTXT renders the DNS-SD TXT record. Keys are lowercase and each entry
// stays far below the 255-byte per-string limit.
//
// Contract (v=1) — clients may rely on these and must tolerate new keys:
//
//	v      record schema version; ignore instances you don't understand
//	id     stable per-install server id (survives rename + IP change)
//	name   display name
//	ver    Heya build version
//	scheme http | https — how to talk to the port in the SRV record
//	path   base path of the web app
//	api    base path of the JSON API
//	remote public origin for off-LAN access, when configured
func BuildTXT(cfg Config) []string {
	scheme := "http"
	if cfg.HTTPS {
		scheme = "https"
	}
	txt := []string{
		"v=" + TXTVersion,
		"name=" + txtSafe(cfg.Instance),
		"scheme=" + scheme,
		"path=/",
		"api=/api",
	}
	if cfg.ServerID != "" {
		txt = append(txt, "id="+txtSafe(cfg.ServerID))
	}
	if cfg.Version != "" {
		txt = append(txt, "ver="+txtSafe(cfg.Version))
	}
	if cfg.RemoteURL != "" {
		txt = append(txt, "remote="+txtSafe(cfg.RemoteURL))
	}
	return txt
}

// txtSafe strips the characters that would corrupt a key=value TXT entry and
// caps length so one long field can't push the record over the wire limit.
func txtSafe(v string) string {
	v = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(v))
	if len(v) > 180 {
		v = v[:180]
	}
	return v
}

// sanitizeInstance keeps the instance name to one printable line. DNS-SD
// instance names are UTF-8 and may contain spaces, so this is deliberately
// permissive — only control characters and dots (which would be read as
// label separators by sloppy clients) are removed.
func sanitizeInstance(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r == 0x7f:
			return -1
		case r == '.':
			return ' '
		default:
			return r
		}
	}, strings.TrimSpace(name))
	name = strings.Join(strings.Fields(name), " ")
	if len(name) > 63 {
		name = strings.TrimSpace(name[:63])
	}
	return name
}

// resolveInterfaces maps configured NIC names to net.Interface values,
// reporting whether the operator actually pinned any. An empty list selects
// every multicast-capable interface, which is the same set zeroconf would
// choose on its own.
func resolveInterfaces(names []string) (selected []net.Interface, selectedNames []string, pinned bool, err error) {
	all, err := net.Interfaces()
	if err != nil {
		return nil, nil, false, fmt.Errorf("listing network interfaces: %w", err)
	}

	wanted := map[string]bool{}
	for _, name := range trimAll(names) {
		wanted[strings.ToLower(name)] = true
	}

	for _, iface := range all {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		if len(wanted) > 0 && !wanted[strings.ToLower(iface.Name)] {
			continue
		}
		selected = append(selected, iface)
		selectedNames = append(selectedNames, iface.Name)
	}
	sort.Strings(selectedNames)

	if len(wanted) > 0 && len(selected) == 0 {
		return nil, nil, true, fmt.Errorf("no multicast-capable interface matched %s", strings.Join(trimAll(names), ", "))
	}
	return selected, selectedNames, len(wanted) > 0, nil
}

// interfaceAddresses collects the addresses to publish as A/AAAA records,
// mirroring zeroconf's own selection: skip loopback, prefer global-unicast
// IPv6, and fall back to link-local v6 only when there is no global one (a
// link-local answer without a zone is useless to most clients, but it beats
// publishing nothing on an IPv6-only segment).
//
// IPv4 is then ordered by how likely a LAN client is to reach it. A developer
// box routinely has a dozen addresses — VM bridges, container networks, a
// tailnet CGNAT address — and clients try them in order, so leading with the
// wrong one costs a connect timeout per attempt. HEYA_DISCOVERY_ADDRESSES
// overrides the guess entirely.
func interfaceAddresses(ifaces []net.Interface) []string {
	var v4, v6, v6local []net.IP
	seen := map[string]bool{}
	add := func(dst *[]net.IP, ip net.IP) {
		if seen[ip.String()] {
			return
		}
		seen[ip.String()] = true
		*dst = append(*dst, ip)
	}

	for i := range ifaces {
		addrs, err := ifaces[i].Addrs()
		if err != nil {
			continue
		}
		for _, address := range addrs {
			ipnet, ok := address.(*net.IPNet)
			if !ok || ipnet.IP.IsLoopback() {
				continue
			}
			switch {
			case ipnet.IP.To4() != nil:
				add(&v4, ipnet.IP)
			case ipnet.IP.IsGlobalUnicast():
				add(&v6, ipnet.IP)
			case ipnet.IP.IsLinkLocalUnicast():
				add(&v6local, ipnet.IP)
			}
		}
	}
	if len(v6) == 0 {
		v6 = v6local
	}

	primary := defaultRouteIP()
	sort.SliceStable(v4, func(i, j int) bool { return addressRank(v4[i], primary) < addressRank(v4[j], primary) })

	out := make([]string, 0, len(v4)+len(v6))
	for _, ip := range append(v4, v6...) {
		out = append(out, ip.String())
	}
	return out
}

// addressRank orders IPv4 candidates: the address on the default route first
// (it is the one the machine itself uses to reach the world, so it is almost
// always the real LAN NIC), then other RFC1918 addresses, then anything else,
// and CGNAT last — 100.64/10 is a tailnet or carrier address that a plain LAN
// client cannot route to.
func addressRank(ip net.IP, primary net.IP) int {
	switch {
	case primary != nil && ip.Equal(primary):
		return 0
	case isCGNAT(ip):
		return 3
	case ip.IsPrivate():
		return 1
	default:
		return 2
	}
}

func isCGNAT(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127
}

// defaultRouteIP reports the local address the kernel would use to reach the
// internet. The UDP "connect" allocates no socket traffic — it only asks the
// routing table — so this is cheap and never blocks. nil when it can't tell.
func defaultRouteIP() net.IP {
	// TEST-NET-1 (RFC 5737): guaranteed unrouted, so nothing is ever sent.
	conn, err := net.Dial("udp4", "192.0.2.1:80")
	if err != nil {
		return nil
	}
	defer func() { _ = conn.Close() }()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil
	}
	return addr.IP
}

// hostLabel reduces a hostname to the single label to publish under
// `.local.`: "mac.local" → "mac", "box.lan" → "box", "knas" → "knas". mDNS
// has exactly one domain, so anything past the first label is a search-domain
// artifact that would otherwise be baked into the advertised name.
func hostLabel(host string) string {
	host = strings.TrimSpace(strings.Trim(host, "."))
	if host == "" {
		return ""
	}
	label := strings.SplitN(host, ".", 2)[0]
	// DNS labels max out at 63 bytes; a longer one would be rejected at pack
	// time, which fails the whole response rather than just the name.
	if len(label) > 63 {
		label = label[:63]
	}
	return label
}

func describeInterfaces(names []string) string {
	if len(names) == 0 {
		return "any interface"
	}
	return strings.Join(names, ", ")
}

func trimAll(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// SplitList parses a comma-separated env value into a trimmed, non-empty
// slice. Shared by the config layer for the interface and address knobs.
func SplitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return trimAll(strings.Split(raw, ","))
}
