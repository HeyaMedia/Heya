package safedial

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// DialContextFunc is the shape used by http.Transport. Production callers
// should use NewPublicHTTPClient with no override; the injectable form exists
// for controlled tests that intentionally route a public-looking hostname to
// an httptest listener.
type DialContextFunc func(context.Context, string, string) (net.Conn, error)

// PublicHTTPClientOptions tunes connection reuse without exposing the proxy
// or dial hooks that form the public-network security boundary.
type PublicHTTPClientOptions struct {
	MaxIdleConns        int
	MaxIdleConnsPerHost int
}

const publicResponseHeaderTimeout = 30 * time.Second

// NewPublicHTTPClient returns a streaming-friendly HTTP client restricted to
// public-network HTTP(S) destinations. It has no whole-request timeout because
// callers may proxy long-lived media streams; connect/TLS timeouts remain on
// the transport, and request cancellation owns the stream lifetime.
func NewPublicHTTPClient() *http.Client {
	return NewPublicHTTPClientWithOptions(PublicHTTPClientOptions{})
}

// NewPublicHTTPClientWithOptions returns the canonical public-network client
// with optional connection-pool sizing for bursty fetchers.
func NewPublicHTTPClientWithOptions(options PublicHTTPClientOptions) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   Control,
	}
	return newPublicHTTPClient(dialer.DialContext, options)
}

// NewPublicHTTPClientWithDialContext builds the same scheme/redirect-validating
// client with a caller-supplied dial function. Supplying a dialer replaces the
// post-DNS Control guard and therefore makes that dialer part of the security
// boundary; this seam is intended for trusted, hermetic tests.
func NewPublicHTTPClientWithDialContext(dial DialContextFunc) *http.Client {
	if dial == nil {
		return NewPublicHTTPClient()
	}
	return newPublicHTTPClient(dial, PublicHTTPClientOptions{})
}

func newPublicHTTPClient(dial DialContextFunc, options PublicHTTPClientOptions) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Never honor HTTP(S)_PROXY. A proxy would make Control vet the proxy's
	// address while the proxy itself fetched the attacker-selected target.
	transport.Proxy = nil
	transport.DialContext = dial
	// Streaming media bodies intentionally have no whole-request timeout, but
	// accepting a connection is not enough progress: a public endpoint must
	// produce response headers within a finite window.
	transport.ResponseHeaderTimeout = publicResponseHeaderTimeout
	if options.MaxIdleConns > 0 {
		transport.MaxIdleConns = options.MaxIdleConns
	}
	if options.MaxIdleConnsPerHost > 0 {
		transport.MaxIdleConnsPerHost = options.MaxIdleConnsPerHost
	}
	return &http.Client{
		Transport: &publicHTTPTransport{base: transport},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return ValidateHTTPURL(request.URL)
		},
	}
}

type publicHTTPTransport struct {
	base *http.Transport
}

func (t *publicHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := ValidateHTTPURL(request.URL); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(request)
}

func (t *publicHTTPTransport) CloseIdleConnections() {
	t.base.CloseIdleConnections()
}

// ValidateHTTPURL rejects non-HTTP schemes, malformed/empty hosts, localhost
// names, and literal non-public IPs. Hostnames are checked again after DNS by
// Control when the transport connects, which is the rebinding-safe boundary.
func ValidateHTTPURL(target *url.URL) error {
	if target == nil {
		return fmt.Errorf("missing URL")
	}
	scheme := strings.ToLower(target.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("URL scheme %q is not allowed", target.Scheme)
	}
	host := target.Hostname()
	if host == "" {
		return fmt.Errorf("URL host is required")
	}
	normalizedHost := strings.TrimSuffix(strings.ToLower(host), ".")
	if normalizedHost == "localhost" || strings.HasSuffix(normalizedHost, ".localhost") {
		return fmt.Errorf("localhost target is not allowed")
	}
	if addr, err := netip.ParseAddr(host); err == nil && !isPublicAddress(addr) {
		return fmt.Errorf("non-public target %s is not allowed", addr)
	}
	return nil
}
