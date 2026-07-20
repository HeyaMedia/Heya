// Package ingress embeds Caddy as Heya's single HTTP ingress runtime.
//
// The package deliberately exposes Heya-shaped configuration and status
// types. Caddy's JSON schema and Prometheus metric names are implementation
// details kept behind Manager so the service/API/UI contract remains stable
// across pinned Caddy upgrades.
package ingress

import (
	"context"
	"crypto/tls"
	"net"
	"time"
)

// HostConfig describes the always-on host listener. HTTPS is disabled only
// for --dev-backend, where the stable development proxy expects plaintext.
type HostConfig struct {
	Address  string
	HTTPS    bool
	DataDir  string
	LANIP    string
	LogLevel string
	WAFMode  string
}

// CertificateGetter supplies a certificate to Caddy at handshake time. It is
// used for Heya's existing remote CertMagic manager and embedded tsnet node;
// Caddy still owns TLS termination and QUIC.
type CertificateGetter func(context.Context, *tls.ClientHelloInfo) (*tls.Certificate, error)

// RemoteConfig adds the host TCP+UDP listener reached through a router port
// mapping. Names contains the managed public certificate subjects when DNS is
// configured. DefaultSNI is required because the heya.media outside-in probe
// intentionally connects by IP without SNI.
type RemoteConfig struct {
	Port            int
	Names           []string
	DefaultSNI      string
	GetCertificate  CertificateGetter
	CertificateMode string
}

// TailnetSource is a ready embedded tsnet node. Custom Caddy network modules
// call these methods to obtain tailnet TCP/UDP and pre-terminated Funnel
// listeners without a loopback reverse-proxy hop.
type TailnetSource interface {
	ListenTCP(address string) (net.Listener, error)
	ListenPacket(address string) (net.PacketConn, error)
	ListenFunnel(address string) (net.Listener, error)
	GetCertificate(context.Context, *tls.ClientHelloInfo) (*tls.Certificate, error)
}

// TailnetConfig describes the listeners Caddy should attach to a ready tsnet
// node. Address is the node's concrete Tailscale IP; tsnet.ListenPacket
// requires it rather than an unspecified address.
type TailnetConfig struct {
	Address    string
	CertDomain string
	HTTPS      bool
	Funnel     bool
	Source     TailnetSource
}

type ListenerStatus struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Network     string   `json:"network"`
	Address     string   `json:"address"`
	Protocols   []string `json:"protocols"`
	TLS         bool     `json:"tls"`
	Public      bool     `json:"public"`
	Active      bool     `json:"active"`
	Description string   `json:"description,omitempty"`
	Error       string   `json:"error,omitempty"`
}

type ProtocolStats struct {
	HTTP1 uint64 `json:"http1"`
	HTTP2 uint64 `json:"http2"`
	HTTP3 uint64 `json:"http3"`
}

// HTTPMetrics is a curated view over Caddy's registry. Totals reset when the
// ingress config reloads or the process restarts; rates are calculated from
// consecutive in-memory samples and never persisted.
type HTTPMetrics struct {
	RequestsTotal     uint64        `json:"requests_total"`
	RequestsPerSecond float64       `json:"requests_per_second"`
	RequestsInFlight  float64       `json:"requests_in_flight"`
	ErrorsTotal       uint64        `json:"errors_total"`
	ErrorsPerSecond   float64       `json:"errors_per_second"`
	P50LatencyMS      float64       `json:"p50_latency_ms"`
	P95LatencyMS      float64       `json:"p95_latency_ms"`
	BytesReceived     uint64        `json:"bytes_received"`
	BytesSent         uint64        `json:"bytes_sent"`
	Protocols         ProtocolStats `json:"protocols"`
}

type IngressMetrics struct {
	Name              string        `json:"name"`
	RequestsTotal     uint64        `json:"requests_total"`
	RequestsPerSecond float64       `json:"requests_per_second"`
	RequestsInFlight  float64       `json:"requests_in_flight"`
	ErrorsTotal       uint64        `json:"errors_total"`
	P95LatencyMS      float64       `json:"p95_latency_ms"`
	BytesSent         uint64        `json:"bytes_sent"`
	Protocols         ProtocolStats `json:"protocols"`
}

type CertificateStatus struct {
	Name      string `json:"name"`
	Source    string `json:"source"`
	Subject   string `json:"subject"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

type Event struct {
	At      time.Time `json:"at"`
	Level   string    `json:"level"`
	Kind    string    `json:"kind"`
	Message string    `json:"message"`
}

type IngressStatus struct {
	Running         bool                `json:"running"`
	Version         string              `json:"version"`
	StartedAt       time.Time           `json:"started_at"`
	UptimeSeconds   int64               `json:"uptime_seconds"`
	Generation      uint64              `json:"generation"`
	LastReloadAt    time.Time           `json:"last_reload_at,omitempty"`
	LastReloadError string              `json:"last_reload_error,omitempty"`
	LocalCARoot     string              `json:"local_ca_root,omitempty"`
	Listeners       []ListenerStatus    `json:"listeners"`
	Certificates    []CertificateStatus `json:"certificates,omitempty"`
	HTTP            HTTPMetrics         `json:"http"`
	ByIngress       []IngressMetrics    `json:"by_ingress,omitempty"`
	RecentEvents    []Event             `json:"recent_events,omitempty"`
	UpdatedAt       time.Time           `json:"updated_at"`
}
