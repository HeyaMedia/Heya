// Package requestmeta carries network facts established by Heya's own ingress
// boundary. Callers must not trust client-supplied forwarding headers: embedded
// Caddy is the first HTTP hop for every supported production topology.
package requestmeta

import (
	"context"
	"net"
	"net/http"
	"strings"
)

type contextKey uint8

const (
	ingressKey contextKey = iota
	clientIPKey
	secureTransportKey
)

func WithIngress(ctx context.Context, ingress string) context.Context {
	return context.WithValue(ctx, ingressKey, strings.TrimSpace(ingress))
}

func Ingress(ctx context.Context) string {
	ingress, _ := ctx.Value(ingressKey).(string)
	return ingress
}

func WithSecureTransport(ctx context.Context, secure bool) context.Context {
	return context.WithValue(ctx, secureTransportKey, secure)
}

func SecureTransport(ctx context.Context) bool {
	secure, _ := ctx.Value(secureTransportKey).(bool)
	return secure
}

// WithClientIP derives the direct peer address from RemoteAddr. X-Forwarded-*
// is deliberately ignored until Heya grows an explicit trusted-proxy config.
func WithClientIP(r *http.Request) *http.Request {
	ip := directIP(r.RemoteAddr)
	ctx := context.WithValue(r.Context(), clientIPKey, ip)
	ctx = WithSecureTransport(ctx, SecureTransport(ctx) || r.TLS != nil)
	return r.WithContext(ctx)
}

func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey).(string)
	if ip == "" {
		return "unknown"
	}
	return ip
}

func directIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err == nil && host != "" {
		return strings.Trim(host, "[]")
	}
	if ip := net.ParseIP(strings.Trim(remoteAddr, "[]")); ip != nil {
		return ip.String()
	}
	return "unknown"
}
