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
	"errors"
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
	"github.com/karbowiak/heya/internal/securityevents"
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

func TestCaddyHTTPServerPlacesPinnedWAFBeforeHeya(t *testing.T) {
	raw, err := json.Marshal(caddyHTTPServer(caddyServerOptions{
		Ingress: "remote", WAFMode: "detect", TrustedNetworks: []string{"100.64.0.0/10", "192.168.0.0/16"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	config := string(raw)
	waf := strings.Index(config, `"handler":"waf"`)
	heya := strings.Index(config, `"handler":"heya"`)
	if waf < 0 || heya < 0 || waf > heya {
		t.Fatalf("WAF must run before Heya: %s", config)
	}
	if !strings.Contains(config, `"load_owasp_crs":true`) || !strings.Contains(config, "SecRuleEngine DetectionOnly") {
		t.Fatalf("detection-only embedded CRS missing: %s", config)
	}
	if !strings.Contains(config, "ctl:ruleRemoveById=920350") || !strings.Contains(config, "192\\\\.168") {
		t.Fatalf("private numeric Host exclusion missing: %s", config)
	}
	if !strings.Contains(config, `@ipMatch 100.64.0.0/10,192.168.0.0/16`) || !strings.Contains(config, "ctl:ruleEngine=Off") {
		t.Fatalf("trusted-network WAF bypass missing: %s", config)
	}
	if !strings.Contains(config, "GET HEAD POST OPTIONS PUT PATCH DELETE") {
		t.Fatalf("REST API method policy missing: %s", config)
	}
}

func TestManagerRejectsUnknownWAFMode(t *testing.T) {
	manager := New(http.NotFoundHandler(), zerolog.Nop())
	err := manager.Start(t.Context(), HostConfig{
		Address: availableTCPAddress(t), DataDir: t.TempDir(), WAFMode: "surprise",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid WAF mode") {
		t.Fatalf("Start error = %v, want invalid WAF mode", err)
	}
}

func TestManagerServesHTTPSAndRedirectsPlainHTTPOnSamePort(t *testing.T) {
	address := availableTCPAddress(t)
	dataDir := t.TempDir()
	securityRecorder := securityevents.New(32)
	manager := New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Heya-Protocol", fmt.Sprintf("%d", r.ProtoMajor))
		if r.URL.Path == "/error" {
			http.Error(w, "test error", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "heya")
	}), zerolog.Nop(), securityRecorder)
	if err := manager.Start(t.Context(), HostConfig{
		Address: address, HTTPS: true, DataDir: dataDir, LANIP: "127.0.0.1", LogLevel: "error", WAFMode: "block",
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
	for _, event := range securityRecorder.Snapshot(32).Recent {
		if event.RuleID == "920350" {
			t.Fatalf("private numeric Host produced CRS 920350 event: %+v", event)
		}
	}

	publicHostRequest, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+address+"/health", nil)
	if err != nil {
		t.Fatalf("create public numeric Host request: %v", err)
	}
	publicHostRequest.Host = "203.0.113.10"
	publicHostResponse, err := client.Do(publicHostRequest)
	if err != nil {
		t.Fatalf("public numeric Host request: %v", err)
	}
	_ = publicHostResponse.Body.Close()
	foundPublicNumericHostMatch := false
	for _, event := range securityRecorder.Snapshot(32).Recent {
		if event.RuleID == "920350" {
			foundPublicNumericHostMatch = true
			break
		}
	}
	if !foundPublicNumericHostMatch {
		t.Fatal("public numeric Host did not produce CRS 920350 event")
	}
	trustedResponse, err := localtls.Client(dataDir, 10*time.Second).Get("https://" + address + "/health")
	if err != nil {
		t.Fatalf("HTTPS GET with Heya local root: %v", err)
	}
	_ = trustedResponse.Body.Close()

	blockedResponse, err := client.Get("https://" + address + "/?id=1%27%20OR%20%271%27=%271")
	if err != nil {
		t.Fatalf("WAF request: %v", err)
	}
	blockedBody, readErr := io.ReadAll(blockedResponse.Body)
	_ = blockedResponse.Body.Close()
	if readErr != nil {
		t.Fatalf("read WAF response: %v", readErr)
	}
	if blockedResponse.StatusCode != http.StatusForbidden || strings.Contains(string(blockedBody), "heya") {
		t.Fatalf("WAF response = %d %q, want blocked before application handler", blockedResponse.StatusCode, blockedBody)
	}
	securitySnapshot := securityRecorder.Snapshot(32)
	if securitySnapshot.Counters.WAFMatches == 0 || securitySnapshot.Counters.WAFBlocked == 0 {
		t.Fatalf("WAF security telemetry missing: %+v", securitySnapshot.Counters)
	}

	patchRequest, err := http.NewRequestWithContext(t.Context(), http.MethodPatch, "https://"+address+"/api/me/settings", strings.NewReader(`{"theme":"dark"}`))
	if err != nil {
		t.Fatalf("create PATCH request: %v", err)
	}
	patchRequest.Header.Set("Content-Type", "application/json")
	patchResponse, err := client.Do(patchRequest)
	if err != nil {
		t.Fatalf("PATCH request: %v", err)
	}
	_ = patchResponse.Body.Close()
	if patchResponse.StatusCode != http.StatusOK {
		t.Fatalf("PATCH response = %d, want REST method admitted by CRS", patchResponse.StatusCode)
	}
	for _, event := range securityRecorder.Snapshot(32).Recent {
		if event.RuleID == "911100" {
			t.Fatalf("valid Heya PATCH produced CRS 911100 event: %+v", event)
		}
	}

	if err := manager.SetTrustedNetworks(t.Context(), []string{"127.0.0.0/8"}); err != nil {
		t.Fatalf("SetTrustedNetworks: %v", err)
	}
	trustedWAFResponse, err := client.Get("https://" + address + "/?id=1%27%20OR%20%271%27=%271")
	if err != nil {
		t.Fatalf("trusted WAF request: %v", err)
	}
	trustedWAFBody, readErr := io.ReadAll(trustedWAFResponse.Body)
	_ = trustedWAFResponse.Body.Close()
	if readErr != nil {
		t.Fatalf("read trusted WAF response: %v", readErr)
	}
	if trustedWAFResponse.StatusCode != http.StatusOK || string(trustedWAFBody) != "heya" {
		t.Fatalf("trusted WAF response = %d %q, want application bypass", trustedWAFResponse.StatusCode, trustedWAFBody)
	}

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

func TestManagerSwitchesTailnetHTTPSListenerToFunnel(t *testing.T) {
	hostAddress := availableTCPAddress(t)
	tailTCP443 := availableTCPAddress(t)
	tailTCP80 := availableTCPAddress(t)
	tailUDP443 := availableUDPAddress(t)
	certificate := localTestCertificate(t)
	source := &conflictingTailnetSource{
		tcp443: tailTCP443,
		tcp80:  tailTCP80,
		udp443: tailUDP443,
		cert:   certificate,
	}

	manager := New(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "tailnet")
	}), zerolog.Nop())
	if err := manager.Start(t.Context(), HostConfig{
		Address: hostAddress, DataDir: t.TempDir(), LogLevel: "error",
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	direct := TailnetConfig{
		Address: "100.64.0.1", CertDomain: "heya.example.ts.net",
		HTTPS: true, Source: source,
	}
	if err := manager.SetTailnet(t.Context(), direct); err != nil {
		t.Fatalf("SetTailnet direct HTTPS: %v", err)
	}
	if got := manager.Status().Listeners; !hasListener(got, "tailnet") {
		t.Fatalf("direct tailnet listener missing: %+v", got)
	}

	funnel := direct
	funnel.Funnel = true
	if err := manager.SetTailnet(t.Context(), funnel); err != nil {
		t.Fatalf("switch to Funnel: %v", err)
	}
	if got := manager.Status().Listeners; !hasListener(got, "funnel") {
		t.Fatalf("Funnel listener missing: %+v", got)
	}
	funnelClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // test-only certificate
		Timeout:   10 * time.Second,
	}
	response, err := funnelClient.Get("https://" + tailTCP443 + "/funnel")
	if err != nil {
		t.Fatalf("Funnel HTTPS GET: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatalf("read Funnel HTTPS body: %v", readErr)
	}
	if response.StatusCode != http.StatusOK || string(body) != "tailnet" {
		t.Fatalf("Funnel response = %d %q", response.StatusCode, body)
	}

	if err := manager.SetTailnet(t.Context(), direct); err != nil {
		t.Fatalf("switch back to direct HTTPS: %v", err)
	}
	if got := manager.Status().Listeners; !hasListener(got, "tailnet") || hasListener(got, "funnel") {
		t.Fatalf("unexpected listeners after disabling Funnel: %+v", got)
	}

	source.funnelErr = errors.New("Funnel not available for this node")
	if err := manager.SetTailnet(t.Context(), funnel); err == nil {
		t.Fatal("switch to unavailable Funnel unexpectedly succeeded")
	}
	if got := manager.Status().Listeners; !hasListener(got, "tailnet") || hasListener(got, "funnel") {
		t.Fatalf("private listener was not restored after Funnel failure: %+v", got)
	}
}

// conflictingTailnetSource deliberately maps direct TCP and Funnel to the
// same real socket. It mirrors tsnet's one-listener-per-port rule and catches
// make-before-break reloads that try to bind :443 twice.
type conflictingTailnetSource struct {
	tcp443    string
	tcp80     string
	udp443    string
	cert      *tls.Certificate
	funnelErr error
}

func (s *conflictingTailnetSource) ListenTCP(address string) (net.Listener, error) {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if port == "443" {
		return net.Listen("tcp", s.tcp443)
	}
	return net.Listen("tcp", s.tcp80)
}

func (s *conflictingTailnetSource) ListenPacket(string) (net.PacketConn, error) {
	return net.ListenPacket("udp", s.udp443)
}

func (s *conflictingTailnetSource) ListenFunnel(string) (net.Listener, error) {
	if s.funnelErr != nil {
		return nil, s.funnelErr
	}
	listener, err := net.Listen("tcp", s.tcp443)
	if err != nil {
		return nil, err
	}
	return tls.NewListener(listener, &tls.Config{
		Certificates: []tls.Certificate{*s.cert},
		NextProtos:   []string{"h2", "http/1.1"},
	}), nil
}

func (s *conflictingTailnetSource) GetCertificate(context.Context, *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return s.cert, nil
}

func hasListener(listeners []ListenerStatus, name string) bool {
	for _, listener := range listeners {
		if listener.Name == name {
			return true
		}
	}
	return false
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

func availableUDPAddress(t *testing.T) string {
	t.Helper()
	packet, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := packet.LocalAddr().String()
	if err := packet.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}
