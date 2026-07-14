package remote

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/rs/zerolog"
	"go.uber.org/zap"
)

// certManager owns TLS material for the remote listener. Two tiers:
//
//   - a persistent self-signed certificate, always present, served whenever
//     no managed certificate matches — it's what makes the reachability
//     probe work before (or without) any DNS provider, since the heya.media
//     prober skips verification and only fingerprints the leaf;
//   - certmagic-managed ACME wildcard certs (DNS-01) when a provider is
//     configured, covering base + *.base. certmagic renews in-background
//     for the life of the process.
type certManager struct {
	names      dnsNames
	selfSigned *tls.Certificate
	magic      *certmagic.Config
	cache      *certmagic.Cache
	hasManaged atomic.Bool
	log        zerolog.Logger
}

func newCertManager(cfg Config, names dnsNames, logger zerolog.Logger) (*certManager, error) {
	if err := os.MkdirAll(cfg.CertDir, 0o700); err != nil {
		return nil, err
	}
	cm := &certManager{names: names, log: logger}

	ss, err := loadOrCreateSelfSigned(cfg.CertDir)
	if err != nil {
		return nil, fmt.Errorf("self-signed fallback: %w", err)
	}
	cm.selfSigned = ss

	if !names.configured {
		return cm, nil
	}

	provider, err := buildProvider(cfg)
	if err != nil {
		return nil, err
	}

	// certmagic insists on a zap logger; its noise goes nowhere and cert
	// state is surfaced through snapshotStatus instead.
	zlog := zap.NewNop()
	cache := certmagic.NewCache(certmagic.CacheOptions{
		Logger: zlog,
		GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) {
			return cm.magic, nil
		},
	})
	magic := certmagic.New(cache, certmagic.Config{
		Storage:           &certmagic.FileStorage{Path: filepath.Join(cfg.CertDir, "acme")},
		Logger:            zlog,
		DefaultServerName: names.base,
	})
	ca := cfg.ACMECA
	if ca == "" {
		ca = certmagic.LetsEncryptProductionCA
	}
	issuer := certmagic.NewACMEIssuer(magic, certmagic.ACMEIssuer{
		CA:     ca,
		Email:  cfg.ACMEEmail,
		Agreed: true,
		Logger: zlog,
		DNS01Solver: &certmagic.DNS01Solver{
			DNSManager: certmagic.DNSManager{
				DNSProvider:      pinZone(provider, cfg.Domain),
				TTL:              time.Minute,
				PropagationDelay: propagationDelay(cfg.DNSProvider),
				Logger:           zlog,
			},
		},
	})
	magic.Issuers = []certmagic.Issuer{issuer}
	cm.magic = magic
	cm.cache = cache
	return cm, nil
}

// issue obtains (or loads from storage) the wildcard cert for base+*.base.
// Blocking; run from the manager's issueLoop goroutine. Renewals afterwards
// are certmagic's own background job for the life of the cache.
func (c *certManager) issue(ctx context.Context) error {
	if c.magic == nil {
		return nil
	}
	if err := c.magic.ManageSync(ctx, c.names.sans); err != nil {
		return err
	}
	c.hasManaged.Store(true)
	return nil
}

// snapshotStatus reports the current certificate tier for the status API.
func (c *certManager) snapshotStatus() CertStatus {
	if c.magic == nil {
		return CertStatus{Mode: "self_signed"}
	}
	if !c.hasManaged.Load() {
		return CertStatus{Mode: "self_signed", SANs: c.names.sans, Issuing: false}
	}
	st := CertStatus{Mode: "acme", SANs: c.names.sans}
	// Expiry from the cached managed cert; base name is always in SANs.
	if crt, err := c.magic.CacheManagedCertificate(context.Background(), c.names.base); err == nil && crt.Leaf != nil {
		st.Expiry = crt.Leaf.NotAfter.UTC().Format(time.RFC3339)
	}
	return st
}

// tlsConfig serves managed certs when present, self-signed otherwise. The
// fallback also catches SNI-less connections (the heya.media prober dials
// by bare IP) and any name outside the managed SAN set.
func (c *certManager) tlsConfig() *tls.Config {
	var magicTLS *tls.Config
	if c.magic != nil {
		magicTLS = c.magic.TLSConfig()
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if magicTLS != nil && c.hasManaged.Load() && hello.ServerName != "" {
				if crt, err := magicTLS.GetCertificate(hello); err == nil {
					return crt, nil
				}
			}
			return c.selfSigned, nil
		},
	}
}

func (c *certManager) close() {
	if c.cache != nil {
		c.cache.Stop()
	}
}

// loadOrCreateSelfSigned returns a persistent self-signed cert from certDir,
// minting a 10-year ECDSA one on first use. Persistence keeps the leaf
// fingerprint stable across restarts, which native clients can pin.
func loadOrCreateSelfSigned(certDir string) (*tls.Certificate, error) {
	certPath := filepath.Join(certDir, "self-signed.crt")
	keyPath := filepath.Join(certDir, "self-signed.key")

	if crt, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		if leaf, perr := x509.ParseCertificate(crt.Certificate[0]); perr == nil && time.Now().Before(leaf.NotAfter) {
			crt.Leaf = leaf
			return &crt, nil
		}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Heya remote access", Organization: []string{"Heya"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"heya.local"},
	}
	if lan := detectLANIP(); lan != "" {
		if ip := net.ParseIP(lan); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return nil, err
	}
	crt, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	if leaf, perr := x509.ParseCertificate(crt.Certificate[0]); perr == nil {
		crt.Leaf = leaf
	}
	return &crt, nil
}

// tlsListener is the remote-access HTTPS listener: same handler tree as the
// LAN listener, TLS via certManager, its own port (the UPnP-mapped one).
type tlsListener struct {
	srv *http.Server
	ln  net.Listener
}

func startTLSListener(port int, handler http.Handler, certs *certManager) (*tlsListener, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("binding :%d: %w", port, err)
	}
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	tlsLn := tls.NewListener(ln, certs.tlsConfig())
	go func() {
		if err := srv.Serve(tlsLn); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			// Listener death outside shutdown is surfaced on the next
			// status check — the port simply stops answering.
			_ = err
		}
	}()
	return &tlsListener{srv: srv, ln: ln}, nil
}

func (t *tlsListener) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = t.srv.Shutdown(ctx)
	_ = t.ln.Close()
}
