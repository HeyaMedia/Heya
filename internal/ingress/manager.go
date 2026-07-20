package ingress

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/karbowiak/heya/internal/securityevents"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

var activeManager atomic.Pointer[Manager]

// Manager is the single owner of Caddy's process-global runtime. All topology
// changes are serialized because Caddy provisions a replacement config before
// retiring the previous one.
type Manager struct {
	handler        http.Handler
	log            zerolog.Logger
	securityEvents *securityevents.Recorder

	opMu sync.Mutex
	mu   sync.RWMutex

	host       HostConfig
	remote     *RemoteConfig
	tailnet    *TailnetConfig
	tailEpoch  uint64
	generation uint64
	startedAt  time.Time
	running    bool
	lastReload time.Time
	lastError  string
	listeners  []ListenerStatus
	events     []Event

	registries map[uint64]*prometheus.Registry

	sharedMu        sync.Mutex
	sharedListeners map[string]*sharedListener

	protocolMu sync.Mutex
	protocols  map[string]ProtocolStats

	metricMu     sync.Mutex
	metricSample metricSample
}

func New(handler http.Handler, logger zerolog.Logger, recorders ...*securityevents.Recorder) *Manager {
	manager := &Manager{
		handler:         handler,
		log:             logger,
		registries:      make(map[uint64]*prometheus.Registry),
		sharedListeners: make(map[string]*sharedListener),
		protocols:       make(map[string]ProtocolStats),
	}
	if len(recorders) > 0 {
		manager.securityEvents = recorders[0]
	}
	return manager
}

// Start installs the initial host listener and claims Caddy's process-global
// runtime for this manager.
func (m *Manager) Start(ctx context.Context, cfg HostConfig) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	if cfg.Address == "" {
		return errors.New("ingress: host address is required")
	}
	if cfg.DataDir == "" {
		return errors.New("ingress: data directory is required")
	}
	cfg.WAFMode = strings.ToLower(strings.TrimSpace(cfg.WAFMode))
	if cfg.WAFMode == "" {
		cfg.WAFMode = "off"
	}
	if cfg.WAFMode != "off" && cfg.WAFMode != "detect" && cfg.WAFMode != "block" {
		return fmt.Errorf("ingress: invalid WAF mode %q (want off, detect, or block)", cfg.WAFMode)
	}
	if cfg.LANIP == "" {
		cfg.LANIP = DetectLANIP()
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("ingress data directory: %w", err)
	}
	if other := activeManager.Load(); other != nil && other != m {
		return errors.New("ingress: another embedded Caddy runtime is already active")
	}
	activeManager.Store(m)

	m.mu.Lock()
	m.host = cfg
	if m.startedAt.IsZero() {
		m.startedAt = time.Now()
	}
	m.mu.Unlock()

	if err := m.reloadLocked(ctx, "initial Caddy configuration"); err != nil {
		activeManager.CompareAndSwap(m, nil)
		return err
	}
	if cfg.HTTPS {
		if err := waitForTLS(ctx, cfg.Address); err != nil {
			_ = caddy.Stop()
			activeManager.CompareAndSwap(m, nil)
			return fmt.Errorf("waiting for local HTTPS certificate: %w", err)
		}
	}
	return nil
}

func waitForTLS(ctx context.Context, address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		address = net.JoinHostPort("127.0.0.1", port)
	}
	readyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	dialer := &tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // readiness only; local CA may not be trusted
	var lastErr error
	for {
		conn, err := dialer.DialContext(readyCtx, "tcp", address)
		if err == nil {
			return conn.Close()
		}
		lastErr = err
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-readyCtx.Done():
			timer.Stop()
			return fmt.Errorf("%w (last handshake: %v)", readyCtx.Err(), lastErr)
		case <-timer.C:
		}
	}
}

// SetRemote atomically replaces the remote listener configuration. A failed
// Caddy load leaves the previous live configuration and certificate getter in
// place.
func (m *Manager) SetRemote(ctx context.Context, cfg RemoteConfig) error {
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("ingress: invalid remote port %d", cfg.Port)
	}
	if cfg.GetCertificate == nil {
		return errors.New("ingress: remote certificate getter is required")
	}

	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	previous := m.remote
	copyCfg := cfg
	copyCfg.Names = append([]string(nil), cfg.Names...)
	m.remote = &copyCfg
	m.mu.Unlock()

	if err := m.reloadLocked(ctx, "remote listener updated"); err != nil {
		m.mu.Lock()
		m.remote = previous
		m.mu.Unlock()
		return err
	}
	return nil
}

func (m *Manager) ClearRemote(ctx context.Context) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	if m.remote == nil {
		m.mu.Unlock()
		return nil
	}
	previous := m.remote
	m.remote = nil
	m.mu.Unlock()
	if err := m.reloadLocked(ctx, "remote listener disabled"); err != nil {
		m.mu.Lock()
		m.remote = previous
		m.mu.Unlock()
		return err
	}
	return nil
}

func (m *Manager) SetTailnet(ctx context.Context, cfg TailnetConfig) error {
	if cfg.Source == nil {
		return errors.New("ingress: tailnet source is required")
	}
	if cfg.Address == "" {
		return errors.New("ingress: tailnet address is required")
	}
	if (cfg.HTTPS || cfg.Funnel) && cfg.CertDomain == "" {
		return errors.New("ingress: tailnet HTTPS/Funnel requires a certificate domain")
	}

	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	previous := m.tailnet
	previousEpoch := m.tailEpoch
	copyCfg := cfg
	// A direct tsnet TCP listener and ListenFunnel cannot both own :443.
	// Caddy normally provisions a replacement config before retiring the old
	// one, which is exactly the wrong ordering for this particular transition:
	// tsnet rejects the Funnel bind while the direct listener is still open (or
	// vice versa). Detach the tailnet edge first, then attach the new mode. The
	// LAN/host listener remains live through both reloads.
	changingFunnelMode := previous != nil && previous.Funnel != cfg.Funnel
	if changingFunnelMode {
		m.tailnet = nil
		m.tailEpoch++
		m.mu.Unlock()

		if err := m.reloadLocked(ctx, "tailnet listener mode transition"); err != nil {
			m.mu.Lock()
			m.tailnet = previous
			m.tailEpoch = previousEpoch
			m.mu.Unlock()
			return err
		}

		m.mu.Lock()
		m.tailnet = &copyCfg
		m.mu.Unlock()
		if err := m.reloadLocked(ctx, "tailnet listener updated"); err != nil {
			// The replacement was rejected (for example, Funnel is not allowed
			// by the tailnet policy). Restore the previously working private
			// listener so a failed public-exposure attempt never takes private
			// tailnet access down with it.
			m.mu.Lock()
			m.tailnet = previous
			m.tailEpoch++
			m.mu.Unlock()
			if restoreErr := m.reloadLocked(ctx, "previous tailnet listener restored"); restoreErr != nil {
				return fmt.Errorf("%w (restoring previous tailnet listener: %v)", err, restoreErr)
			}
			return err
		}
		return nil
	}

	if previous == nil || previous.Source != cfg.Source {
		m.tailEpoch++
	}
	m.tailnet = &copyCfg
	m.mu.Unlock()

	if err := m.reloadLocked(ctx, "tailnet listener updated"); err != nil {
		m.mu.Lock()
		m.tailnet = previous
		m.tailEpoch = previousEpoch
		m.mu.Unlock()
		return err
	}
	return nil
}

func (m *Manager) ClearTailnet(ctx context.Context) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	if m.tailnet == nil {
		m.mu.Unlock()
		return nil
	}
	previous := m.tailnet
	previousEpoch := m.tailEpoch
	m.tailnet = nil
	m.tailEpoch++
	m.mu.Unlock()
	if err := m.reloadLocked(ctx, "tailnet listener disabled"); err != nil {
		m.mu.Lock()
		m.tailnet = previous
		m.tailEpoch = previousEpoch
		m.mu.Unlock()
		return err
	}
	return nil
}

// Close stops Caddy and closes any custom listeners whose final Caddy
// reference was not released because shutdown was interrupted.
func (m *Manager) Close() error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if activeManager.Load() != m {
		return nil
	}
	err := caddy.Stop()
	activeManager.CompareAndSwap(m, nil)

	m.sharedMu.Lock()
	for key, shared := range m.sharedListeners {
		_ = shared.listener.Close()
		delete(m.sharedListeners, key)
	}
	m.sharedMu.Unlock()

	m.mu.Lock()
	m.running = false
	m.addEventLocked("info", "lifecycle", "Caddy ingress stopped")
	m.mu.Unlock()
	return err
}

func (m *Manager) reloadLocked(_ context.Context, reason string) error {
	m.mu.RLock()
	nextGeneration := m.generation + 1
	m.mu.RUnlock()

	cfg, listeners, err := m.buildConfig(nextGeneration)
	if err != nil {
		m.recordReloadError(reason, err)
		return err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		m.recordReloadError(reason, err)
		return err
	}
	if err := caddy.Load(raw, true); err != nil {
		m.recordReloadError(reason, err)
		return fmt.Errorf("loading embedded Caddy config: %w", err)
	}

	m.mu.Lock()
	m.generation = nextGeneration
	m.running = true
	m.lastReload = time.Now()
	m.lastError = ""
	m.listeners = listeners
	for generation := range m.registries {
		if generation != nextGeneration {
			delete(m.registries, generation)
		}
	}
	m.addEventLocked("info", "reload", reason)
	m.mu.Unlock()
	m.log.Info().Uint64("generation", nextGeneration).Str("reason", reason).Msg("Caddy ingress configuration loaded")
	return nil
}

func (m *Manager) recordReloadError(reason string, err error) {
	m.mu.Lock()
	m.lastError = err.Error()
	m.addEventLocked("error", "reload", reason+": "+err.Error())
	m.mu.Unlock()
	m.log.Error().Err(err).Str("reason", reason).Msg("Caddy ingress configuration rejected")
}

func (m *Manager) addEventLocked(level, kind, message string) {
	m.events = append(m.events, Event{At: time.Now().UTC(), Level: level, Kind: kind, Message: message})
	if len(m.events) > 64 {
		m.events = append([]Event(nil), m.events[len(m.events)-64:]...)
	}
}

func (m *Manager) registerMetrics(generation uint64, registry *prometheus.Registry) {
	if registry == nil {
		return
	}
	m.mu.Lock()
	m.registries[generation] = registry
	m.mu.Unlock()
}

func (m *Manager) buildConfig(generation uint64) (map[string]any, []ListenerStatus, error) {
	m.mu.RLock()
	host := m.host
	var remoteCfg *RemoteConfig
	if m.remote != nil {
		copyRemote := *m.remote
		copyRemote.Names = append([]string(nil), m.remote.Names...)
		remoteCfg = &copyRemote
	}
	var tailCfg *TailnetConfig
	if m.tailnet != nil {
		copyTail := *m.tailnet
		tailCfg = &copyTail
	}
	m.mu.RUnlock()

	if remoteCfg != nil && samePort(host.Address, remoteCfg.Port) {
		return nil, nil, fmt.Errorf("remote port %d conflicts with host listener %s", remoteCfg.Port, host.Address)
	}

	servers := map[string]any{}
	listeners := make([]ListenerStatus, 0, 6)
	localSubjects, defaultSNI := localCertificateSubjects(host)
	automationPolicies := make([]any, 0, 3)
	automate := make([]string, 0, len(localSubjects))
	if host.HTTPS {
		automate = append(automate, localSubjects...)
		automationPolicies = append(automationPolicies, map[string]any{
			"subjects": localSubjects,
			"issuers":  []any{map[string]any{"module": "internal"}},
		})
	}

	servers["host"] = caddyHTTPServer(caddyServerOptions{
		Listen:     []string{"tcp/" + host.Address},
		Ingress:    "host",
		Generation: generation,
		HTTPS:      host.HTTPS,
		DefaultSNI: defaultSNI,
		HTTP3:      host.HTTPS,
		WAFMode:    host.WAFMode,
	})
	hostProtocols := []string{"h1"}
	if host.HTTPS {
		hostProtocols = []string{"h1", "h2", "h3"}
	}
	listeners = append(listeners, ListenerStatus{
		Name: "host", Kind: "host", Network: networkForProtocols(hostProtocols), Address: host.Address,
		Protocols: hostProtocols, TLS: host.HTTPS, Active: true,
		Description: "Primary LAN/host listener owned by embedded Caddy",
	})

	if remoteCfg != nil {
		remoteDefault := remoteCfg.DefaultSNI
		if remoteDefault == "" {
			remoteDefault = "heya-remote.local"
		}
		remoteSubjects := append([]string(nil), remoteCfg.Names...)
		if len(remoteSubjects) == 0 {
			remoteSubjects = []string{remoteDefault}
		}
		automationPolicies = append([]any{map[string]any{
			"subjects": remoteSubjects,
			"get_certificate": []any{map[string]any{
				"via": "heya_remote", "generation": generation,
			}},
		}}, automationPolicies...)
		address := net.JoinHostPort("", strconv.Itoa(remoteCfg.Port))
		servers["remote"] = caddyHTTPServer(caddyServerOptions{
			Listen: []string{"tcp/" + address}, Ingress: "remote", Generation: generation,
			HTTPS: true, DefaultSNI: remoteDefault, HTTP3: true, WAFMode: host.WAFMode,
		})
		listeners = append(listeners, ListenerStatus{
			Name: "remote", Kind: "remote", Network: "tcp+udp", Address: address,
			Protocols: []string{"h1", "h2", "h3"}, TLS: true, Public: true, Active: true,
			Description: "Direct internet listener mapped by UPnP or a manual port forward",
		})
	}

	if tailCfg != nil {
		tailHostPort := net.JoinHostPort(tailCfg.Address, "443")
		if tailCfg.HTTPS {
			automationPolicies = append([]any{map[string]any{
				"subjects": []string{tailCfg.CertDomain},
				"get_certificate": []any{map[string]any{
					"via": "heya_tailscale", "generation": generation,
				}},
			}}, automationPolicies...)
		}

		switch {
		case tailCfg.Funnel:
			servers["funnel"] = caddyHTTPServer(caddyServerOptions{
				Listen: []string{"heya-funnel/" + tailHostPort}, Ingress: "funnel", Generation: generation,
				PreTerminatedTLS: true, WAFMode: host.WAFMode,
			})
			listeners = append(listeners, ListenerStatus{
				Name: "funnel", Kind: "funnel", Network: "tcp", Address: tailHostPort,
				Protocols: []string{"h1", "h2"}, TLS: true, Public: true, Active: true,
				Description: "Tailscale Funnel listener; TLS is terminated by Tailscale",
			})
			if tailCfg.HTTPS {
				servers["tailnet_h3"] = caddyHTTPServer(caddyServerOptions{
					Listen: []string{"heya-tsnet/" + tailHostPort}, Ingress: "tailnet", Generation: generation,
					HTTPS: true, DefaultSNI: tailCfg.CertDomain, HTTP3Only: true, WAFMode: host.WAFMode,
				})
				listeners = append(listeners, ListenerStatus{
					Name: "tailnet-h3", Kind: "tailscale", Network: "udp", Address: tailHostPort,
					Protocols: []string{"h3"}, TLS: true, Active: true,
					Description: "Direct tailnet HTTP/3 alongside Funnel's TCP listener",
				})
			}
		case tailCfg.HTTPS:
			servers["tailnet"] = caddyHTTPServer(caddyServerOptions{
				Listen: []string{"heya-tsnet/" + tailHostPort}, Ingress: "tailnet", Generation: generation,
				HTTPS: true, DefaultSNI: tailCfg.CertDomain, HTTP3: true, WAFMode: host.WAFMode,
			})
			listeners = append(listeners, ListenerStatus{
				Name: "tailnet", Kind: "tailscale", Network: "tcp+udp", Address: tailHostPort,
				Protocols: []string{"h1", "h2", "h3"}, TLS: true, Active: true,
				Description: "Direct tailnet listener provided to Caddy by embedded tsnet",
			})
		default:
			tailHTTP := net.JoinHostPort(tailCfg.Address, "80")
			servers["tailnet"] = caddyHTTPServer(caddyServerOptions{
				Listen: []string{"heya-tsnet/" + tailHTTP}, Ingress: "tailnet", Generation: generation, WAFMode: host.WAFMode,
			})
			listeners = append(listeners, ListenerStatus{
				Name: "tailnet", Kind: "tailscale", Network: "tcp", Address: tailHTTP,
				Protocols: []string{"h1"}, Active: true,
				Description: "Plain HTTP tailnet listener",
			})
		}

		if tailCfg.HTTPS || tailCfg.Funnel {
			redirectAddr := net.JoinHostPort(tailCfg.Address, "80")
			servers["tailnet_redirect"] = redirectServer("heya-tsnet/"+redirectAddr, tailCfg.CertDomain)
			listeners = append(listeners, ListenerStatus{
				Name: "tailnet-redirect", Kind: "tailscale", Network: "tcp", Address: redirectAddr,
				Protocols: []string{"h1"}, Active: true,
				Description: "Tailnet HTTP to HTTPS redirect",
			})
		}
	}

	apps := map[string]any{
		"http": map[string]any{
			"metrics": map[string]any{},
			"servers": servers,
		},
	}
	if host.HTTPS || remoteCfg != nil || (tailCfg != nil && tailCfg.HTTPS) {
		tlsApp := map[string]any{
			"automation": map[string]any{"policies": automationPolicies},
			// Heya owns a single local file-system storage and keeps its
			// certificates through normal renewal. Caddy's process-global storage
			// cleaner is unnecessary here and, in Caddy 2.11, races asynchronous
			// internal-certificate events during TLS app startup.
			"disable_storage_clean": true,
		}
		if len(automate) > 0 {
			tlsApp["certificates"] = map[string]any{"automate": automate}
		}
		apps["tls"] = tlsApp
		apps["pki"] = map[string]any{
			"certificate_authorities": map[string]any{
				"local": map[string]any{
					"name": "Heya Local Authority", "root_common_name": "Heya Local Root CA",
					"intermediate_common_name": "Heya Local Intermediate CA", "install_trust": false,
				},
			},
		}
	}

	level := strings.ToUpper(host.LogLevel)
	if level == "" {
		level = "INFO"
	}
	if level == "TRACE" {
		level = "DEBUG"
	}
	if level == "FATAL" || level == "PANIC" || level == "DISABLED" {
		level = "ERROR"
	}
	root := filepath.Join(host.DataDir, "caddy")
	defaultLog := map[string]any{"level": level}
	if host.WAFMode != "off" {
		// The core tees only sanitized Coraza security signals into Heya's
		// bounded admin event recorder. Ordinary Caddy logs keep their normal
		// stderr destination and verbosity.
		defaultLog["core"] = map[string]any{"module": "heya_security"}
	}
	config := map[string]any{
		"admin": map[string]any{
			"disabled": true, "config": map[string]any{"persist": false},
		},
		"storage": map[string]any{"module": "file_system", "root": root},
		"logging": map[string]any{"logs": map[string]any{"default": defaultLog}},
		"apps":    apps,
	}
	return config, listeners, nil
}

type caddyServerOptions struct {
	Listen           []string
	Ingress          string
	Generation       uint64
	HTTPS            bool
	DefaultSNI       string
	HTTP3            bool
	HTTP3Only        bool
	PreTerminatedTLS bool
	WAFMode          string
}

const wafDirectives = `
Include @coraza.conf-recommended
SecRequestBodyLimit 27262976
SecRequestBodyInMemoryLimit 1048576
SecResponseBodyAccess Off
Include @crs-setup.conf.example
Include @owasp_crs/*.conf
SecRuleUpdateTargetByTag "OWASP_CRS" "!REQUEST_HEADERS:Authorization"
SecRuleUpdateTargetByTag "OWASP_CRS" "!REQUEST_HEADERS:Cookie"
SecRuleUpdateTargetByTag "OWASP_CRS" "!ARGS:password"
SecRuleUpdateTargetByTag "OWASP_CRS" "!ARGS:current_password"
SecRuleUpdateTargetByTag "OWASP_CRS" "!ARGS:new_password"
SecRuleEngine %s
`

func caddyHTTPServer(o caddyServerOptions) map[string]any {
	protocols := []string{"h1"}
	if o.HTTP3Only {
		protocols = []string{"h3"}
	} else if o.HTTPS || o.PreTerminatedTLS {
		protocols = []string{"h1", "h2"}
		if o.HTTP3 {
			protocols = append(protocols, "h3")
		}
	}
	handlers := make([]any, 0, 2)
	if o.WAFMode == "detect" || o.WAFMode == "block" {
		engine := "DetectionOnly"
		if o.WAFMode == "block" {
			engine = "On"
		}
		handlers = append(handlers, map[string]any{
			"handler":        "waf",
			"load_owasp_crs": true,
			"directives":     fmt.Sprintf(wafDirectives, engine),
		})
	}
	handlers = append(handlers, map[string]any{
		"handler": "heya", "ingress": o.Ingress, "generation": o.Generation,
	})

	server := map[string]any{
		"listen":          o.Listen,
		"protocols":       protocols,
		"automatic_https": map[string]any{"disable": true},
		"routes": []any{map[string]any{
			"handle": handlers,
		}},
		"read_header_timeout": "15s",
		"read_timeout":        "2m",
		"max_header_bytes":    64 << 10,
		"idle_timeout":        "5m",
	}
	if o.HTTPS {
		server["tls_connection_policies"] = []any{map[string]any{"default_sni": o.DefaultSNI}}
		if !o.HTTP3Only {
			server["listener_wrappers"] = []any{
				map[string]any{"wrapper": "http_redirect"},
				map[string]any{"wrapper": "tls"},
			}
		}
	}
	return server
}

func redirectServer(listen, targetHost string) map[string]any {
	location := "https://{http.request.host}{http.request.uri}"
	if targetHost != "" {
		// Redirect to the MagicDNS certificate name, not the requested IP. A
		// tailnet client commonly reaches :80 by IP, which is not covered by
		// Tailscale's certificate.
		location = "https://" + targetHost + "{http.request.uri}"
	}
	return map[string]any{
		"listen":          []string{listen},
		"protocols":       []string{"h1"},
		"automatic_https": map[string]any{"disable": true},
		"routes": []any{map[string]any{
			"handle": []any{map[string]any{
				"handler": "static_response", "status_code": 308,
				"headers": map[string]any{"Location": []string{location}},
			}},
		}},
	}
}

func localCertificateSubjects(cfg HostConfig) ([]string, string) {
	subjects := []string{"heya.local", "localhost", "127.0.0.1", "::1"}
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		subjects = append(subjects, hostname)
		if !strings.Contains(hostname, ".") {
			subjects = append(subjects, hostname+".local")
		}
	}
	defaultSNI := "127.0.0.1"
	if ip, err := netip.ParseAddr(cfg.LANIP); err == nil && !ip.IsUnspecified() {
		subjects = append(subjects, ip.String())
		defaultSNI = ip.String()
	}
	if host, _, err := net.SplitHostPort(cfg.Address); err == nil {
		host = strings.Trim(host, "[]")
		if ip, err := netip.ParseAddr(host); err == nil && !ip.IsUnspecified() {
			subjects = append(subjects, ip.String())
			defaultSNI = ip.String()
		} else if host != "" && host != "0.0.0.0" && host != "::" {
			subjects = append(subjects, host)
			defaultSNI = host
		}
	}
	sort.Strings(subjects)
	subjects = compactStrings(subjects)
	return subjects, defaultSNI
}

func compactStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func samePort(address string, port int) bool {
	_, rawPort, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	return rawPort == strconv.Itoa(port)
}

func networkForProtocols(protocols []string) string {
	for _, protocol := range protocols {
		if protocol == "h3" {
			return "tcp+udp"
		}
	}
	return "tcp"
}

// DetectLANIP returns the interface address selected for internet-bound
// traffic. Dialing UDP chooses a route locally; it sends no application data.
func DetectLANIP() string {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close() //nolint:errcheck // throwaway route-selection socket
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}

type sharedListener struct {
	listener net.Listener
	refs     int
}

type listenerRef struct {
	net.Listener
	once    sync.Once
	release func()
}

func (l *listenerRef) Close() error {
	l.once.Do(l.release)
	return nil
}

func (m *Manager) acquireTailListener(kind, address string) (net.Listener, error) {
	m.mu.RLock()
	tail := m.tailnet
	epoch := m.tailEpoch
	m.mu.RUnlock()
	if tail == nil || tail.Source == nil {
		return nil, errors.New("tailnet source is not attached")
	}
	key := fmt.Sprintf("%d/%s/%s", epoch, kind, address)
	m.sharedMu.Lock()
	if shared := m.sharedListeners[key]; shared != nil {
		shared.refs++
		ref := m.newListenerRefLocked(key, shared)
		m.sharedMu.Unlock()
		return ref, nil
	}
	m.sharedMu.Unlock()

	var (
		listener net.Listener
		err      error
	)
	switch kind {
	case "tcp":
		listener, err = tail.Source.ListenTCP(address)
	case "funnel":
		listener, err = tail.Source.ListenFunnel(address)
	default:
		err = fmt.Errorf("unknown tailnet listener kind %q", kind)
	}
	if err != nil {
		return nil, err
	}

	m.sharedMu.Lock()
	if existing := m.sharedListeners[key]; existing != nil {
		// Another Caddy provisioning path won the race. Keep only the shared
		// listener and close this redundant bind.
		_ = listener.Close()
		existing.refs++
		ref := m.newListenerRefLocked(key, existing)
		m.sharedMu.Unlock()
		return ref, nil
	}
	shared := &sharedListener{listener: listener, refs: 1}
	m.sharedListeners[key] = shared
	ref := m.newListenerRefLocked(key, shared)
	m.sharedMu.Unlock()
	return ref, nil
}

func (m *Manager) newListenerRefLocked(key string, shared *sharedListener) net.Listener {
	return &listenerRef{Listener: shared.listener, release: func() {
		m.sharedMu.Lock()
		defer m.sharedMu.Unlock()
		current := m.sharedListeners[key]
		if current != shared {
			return
		}
		shared.refs--
		if shared.refs <= 0 {
			_ = shared.listener.Close()
			delete(m.sharedListeners, key)
		}
	}}
}

func (m *Manager) tailPacket(address string) (net.PacketConn, error) {
	m.mu.RLock()
	tail := m.tailnet
	m.mu.RUnlock()
	if tail == nil || tail.Source == nil {
		return nil, errors.New("tailnet source is not attached")
	}
	return tail.Source.ListenPacket(address)
}
