package ingress

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/certmagic"

	// Link only the modules Heya's generated config actually references. The
	// full standard bundle includes reverse proxies, templates, tracing and an
	// ACME server that would add binary/dependency weight without being usable
	// through Heya's deliberately closed Caddy surface.
	_ "github.com/caddyserver/caddy/v2/modules/caddypki"
	_ "github.com/caddyserver/caddy/v2/modules/caddytls"
	_ "github.com/caddyserver/caddy/v2/modules/filestorage"
)

func init() {
	caddy.RegisterModule(heyaHandler{})
	caddy.RegisterModule(heyaRemoteCertificateManager{})
	caddy.RegisterModule(heyaTailscaleCertificateManager{})

	caddy.RegisterNetwork("heya-tsnet", tailnetTCPListener)
	caddy.RegisterNetwork("heya-tsnet-udp", tailnetPacketListener)
	caddy.RegisterNetwork("heya-funnel", funnelListener)
	caddyhttp.RegisterNetworkHTTP3("heya-tsnet", "heya-tsnet-udp")
}

type heyaHandler struct {
	Ingress    string `json:"ingress,omitempty"`
	Generation uint64 `json:"generation,omitempty"`

	manager *Manager
	handler http.Handler
}

func (heyaHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.heya",
		New: func() caddy.Module { return new(heyaHandler) },
	}
}

func (h *heyaHandler) Provision(ctx caddy.Context) error {
	m := activeManager.Load()
	if m == nil || m.handler == nil {
		return errors.New("heya ingress handler is not registered")
	}
	h.manager = m
	h.handler = m.handler
	m.registerMetrics(h.Generation, ctx.GetMetricsRegistry())
	return nil
}

func (h *heyaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, _ caddyhttp.Handler) error {
	h.handler.ServeHTTP(w, r)
	// Record after Heya returns so a status request does not include itself in
	// the protocol totals before Caddy increments the corresponding request
	// counter. The two views then describe the same completed request set.
	h.manager.observeProtocol(h.Ingress, r.ProtoMajor)
	return nil
}

type heyaRemoteCertificateManager struct {
	Generation uint64 `json:"generation,omitempty"`
	getter     CertificateGetter
}

func (heyaRemoteCertificateManager) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "tls.get_certificate.heya_remote",
		New: func() caddy.Module { return new(heyaRemoteCertificateManager) },
	}
}

func (m *heyaRemoteCertificateManager) Provision(caddy.Context) error {
	manager := activeManager.Load()
	if manager == nil {
		return errors.New("heya ingress manager is not active")
	}
	manager.mu.RLock()
	if manager.remote != nil {
		m.getter = manager.remote.GetCertificate
	}
	manager.mu.RUnlock()
	if m.getter == nil {
		return errors.New("heya remote certificate source is not configured")
	}
	return nil
}

func (m heyaRemoteCertificateManager) GetCertificate(ctx context.Context, hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return m.getter(ctx, hello)
}

type heyaTailscaleCertificateManager struct {
	Generation uint64 `json:"generation,omitempty"`
	getter     CertificateGetter
}

func (heyaTailscaleCertificateManager) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "tls.get_certificate.heya_tailscale",
		New: func() caddy.Module { return new(heyaTailscaleCertificateManager) },
	}
}

func (m *heyaTailscaleCertificateManager) Provision(caddy.Context) error {
	manager := activeManager.Load()
	if manager == nil {
		return errors.New("heya ingress manager is not active")
	}
	manager.mu.RLock()
	if manager.tailnet != nil && manager.tailnet.Source != nil {
		m.getter = manager.tailnet.Source.GetCertificate
	}
	manager.mu.RUnlock()
	if m.getter == nil {
		return errors.New("heya tailnet certificate source is not configured")
	}
	return nil
}

func (m heyaTailscaleCertificateManager) GetCertificate(ctx context.Context, hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return m.getter(ctx, hello)
}

func tailnetTCPListener(_ context.Context, _ string, host, portRange string, portOffset uint, _ net.ListenConfig) (any, error) {
	m := activeManager.Load()
	if m == nil {
		return nil, errors.New("heya ingress manager is not active")
	}
	address, err := customNetworkAddress(host, portRange, portOffset)
	if err != nil {
		return nil, err
	}
	return m.acquireTailListener("tcp", address)
}

func tailnetPacketListener(_ context.Context, _ string, host, portRange string, portOffset uint, _ net.ListenConfig) (any, error) {
	m := activeManager.Load()
	if m == nil {
		return nil, errors.New("heya ingress manager is not active")
	}
	address, err := customNetworkAddress(host, portRange, portOffset)
	if err != nil {
		return nil, err
	}
	return m.tailPacket(address)
}

func funnelListener(_ context.Context, _ string, host, portRange string, portOffset uint, _ net.ListenConfig) (any, error) {
	m := activeManager.Load()
	if m == nil {
		return nil, errors.New("heya ingress manager is not active")
	}
	address, err := customNetworkAddress(host, portRange, portOffset)
	if err != nil {
		return nil, err
	}
	return m.acquireTailListener("funnel", address)
}

func customNetworkAddress(host, portRange string, portOffset uint) (string, error) {
	first, _, _ := strings.Cut(portRange, "-")
	port, err := strconv.ParseUint(first, 10, 16)
	if err != nil {
		return "", fmt.Errorf("invalid custom network port %q: %w", portRange, err)
	}
	port += uint64(portOffset)
	if port > 65535 {
		return "", fmt.Errorf("custom network port overflow: %d", port)
	}
	return net.JoinHostPort(host, strconv.FormatUint(port, 10)), nil
}

var (
	_ caddy.Provisioner           = (*heyaHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*heyaHandler)(nil)
	_ caddy.Provisioner           = (*heyaRemoteCertificateManager)(nil)
	_ certmagic.Manager           = (*heyaRemoteCertificateManager)(nil)
	_ caddy.Provisioner           = (*heyaTailscaleCertificateManager)(nil)
	_ certmagic.Manager           = (*heyaTailscaleCertificateManager)(nil)
)
