package ingress

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/localtls"
	"github.com/quic-go/quic-go/http3"
	"github.com/rs/zerolog"
)

func TestTailnetRedirectUsesCertificateDomain(t *testing.T) {
	raw, err := json.Marshal(redirectServer("heya-tsnet/:80", "heya.example.ts.net"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `https://heya.example.ts.net{http.request.uri}`) {
		t.Fatalf("redirect config does not target the certificate domain: %s", raw)
	}
}

func TestManagerServesHTTPSAndRedirectsPlainHTTPOnSamePort(t *testing.T) {
	address := availableTCPAddress(t)
	dataDir := t.TempDir()
	manager := New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Heya-Protocol", fmt.Sprintf("%d", r.ProtoMajor))
		if r.URL.Path == "/error" {
			http.Error(w, "test error", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "heya")
	}), zerolog.Nop())
	if err := manager.Start(t.Context(), HostConfig{
		Address: address, HTTPS: true, DataDir: dataDir, LANIP: "127.0.0.1", LogLevel: "error",
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	transport := &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test-only local CA
		ForceAttemptHTTP2: true,
	}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	response, err := client.Get("https://" + address + "/health")
	if err != nil {
		t.Fatalf("HTTPS GET: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatalf("read HTTPS body: %v", readErr)
	}
	if response.StatusCode != http.StatusOK || string(body) != "heya" {
		t.Fatalf("HTTPS response = %d %q", response.StatusCode, body)
	}
	trustedResponse, err := localtls.Client(dataDir, 10*time.Second).Get("https://" + address + "/health")
	if err != nil {
		t.Fatalf("HTTPS GET with Heya local root: %v", err)
	}
	_ = trustedResponse.Body.Close()

	h3Transport := &http3.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test-only local CA
	}
	t.Cleanup(func() { _ = h3Transport.Close() })
	h3Client := &http.Client{Transport: h3Transport, Timeout: 10 * time.Second}
	h3Response, err := h3Client.Get("https://" + address + "/health")
	if err != nil {
		t.Fatalf("HTTP/3 GET: %v", err)
	}
	_, readErr = io.Copy(io.Discard, h3Response.Body)
	_ = h3Response.Body.Close()
	if readErr != nil {
		t.Fatalf("read HTTP/3 body: %v", readErr)
	}
	if h3Response.ProtoMajor != 3 || h3Response.Header.Get("X-Heya-Protocol") != "3" {
		t.Fatalf("HTTP/3 response protocol = %s, handler saw %q", h3Response.Proto, h3Response.Header.Get("X-Heya-Protocol"))
	}

	redirectClient := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	redirect, err := redirectClient.Get("http://" + address + "/health?check=1")
	if err != nil {
		t.Fatalf("HTTP redirect GET: %v", err)
	}
	_ = redirect.Body.Close()
	if redirect.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("redirect status = %d, want %d", redirect.StatusCode, http.StatusPermanentRedirect)
	}
	if got := redirect.Header.Get("Location"); got != "https://"+address+"/health?check=1" {
		t.Fatalf("redirect Location = %q", got)
	}

	status := manager.Status()
	if !status.Running || len(status.Listeners) != 1 {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.LocalCARoot != localtls.RootPath(dataDir) {
		t.Fatalf("local CA root = %q, want %q", status.LocalCARoot, localtls.RootPath(dataDir))
	}
	if got := status.Listeners[0].Protocols; len(got) != 3 || got[2] != "h3" {
		t.Fatalf("listener protocols = %v", got)
	}
	if status.HTTP.RequestsTotal == 0 {
		t.Fatalf("Caddy request metrics were not gathered: %+v", status.HTTP)
	}
	if status.HTTP.Protocols.HTTP3 == 0 {
		t.Fatalf("HTTP/3 request was not observed: %+v", status.HTTP.Protocols)
	}
	errorResponse, err := client.Get("https://" + address + "/error")
	if err != nil {
		t.Fatalf("HTTPS error GET: %v", err)
	}
	_ = errorResponse.Body.Close()
	if got := manager.Status().HTTP.ErrorsTotal; got != 1 {
		t.Fatalf("HTTP 5xx metric = %d, want 1", got)
	}
}

func TestManagerHotAddsRemoteHTTPSAndHTTP3(t *testing.T) {
	hostAddress := availableTCPAddress(t)
	remoteAddress := availableTCPAddress(t)
	_, remotePortText, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		t.Fatal(err)
	}
	remotePort, err := strconv.Atoi(remotePortText)
	if err != nil {
		t.Fatal(err)
	}

	manager := New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Heya-Protocol", strconv.Itoa(r.ProtoMajor))
		_, _ = io.WriteString(w, "remote")
	}), zerolog.Nop())
	if err := manager.Start(t.Context(), HostConfig{
		Address: hostAddress, DataDir: t.TempDir(), LogLevel: "error",
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	certificate := localTestCertificate(t)
	if err := manager.SetRemote(t.Context(), RemoteConfig{
		Port: remotePort, Names: []string{"127.0.0.1"}, DefaultSNI: "127.0.0.1",
		CertificateMode: "test", GetCertificate: func(context.Context, *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return certificate, nil
		},
	}); err != nil {
		t.Fatalf("SetRemote: %v", err)
	}

	tlsConfig := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test-only certificate
	httpsClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsConfig, ForceAttemptHTTP2: true},
		Timeout:   10 * time.Second,
	}
	response, err := httpsClient.Get("https://" + remoteAddress + "/remote")
	if err != nil {
		t.Fatalf("remote HTTPS GET: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("remote HTTPS status = %d", response.StatusCode)
	}

	h3Transport := &http3.Transport{TLSClientConfig: tlsConfig.Clone()}
	t.Cleanup(func() { _ = h3Transport.Close() })
	h3Response, err := (&http.Client{Transport: h3Transport, Timeout: 10 * time.Second}).Get("https://" + remoteAddress + "/remote")
	if err != nil {
		t.Fatalf("remote HTTP/3 GET: %v", err)
	}
	_ = h3Response.Body.Close()
	if h3Response.ProtoMajor != 3 {
		t.Fatalf("remote protocol = %s", h3Response.Proto)
	}

	status := manager.Status()
	if len(status.Listeners) != 2 || status.HTTP.Protocols.HTTP3 == 0 {
		t.Fatalf("unexpected remote status: %+v", status)
	}
	if err := manager.ClearRemote(t.Context()); err != nil {
		t.Fatalf("ClearRemote: %v", err)
	}
	if got := manager.Status().Listeners; len(got) != 1 || got[0].Kind != "host" {
		t.Fatalf("listeners after ClearRemote = %+v", got)
	}
}

func localTestCertificate(t *testing.T) *tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatal(err)
	}
	pair.Leaf, _ = x509.ParseCertificate(der)
	return &pair
}

func availableTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}
