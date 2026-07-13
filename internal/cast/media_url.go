package cast

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

const (
	defaultMediaPort = "8080"
	maxMediaTokenTTL = 12 * time.Hour
	mediaTokenGrace  = time.Hour
)

type mediaTokenClaims struct {
	Version int    `json:"v"`
	UserID  int64  `json:"u"`
	Path    string `json:"p"`
	Subtree bool   `json:"s,omitempty"`
	Expires int64  `json:"e"`
}

// SetMediaOrigin configures the receiver-facing Heya origin. Empty baseURL is
// intentional: each receiver then gets the local interface address selected
// by the OS route to that receiver. That keeps a multi-homed Heya server from
// handing a Chromecast an unrelated VLAN or Tailscale address.
func (m *Manager) SetMediaOrigin(baseURL, port string) {
	m.mu.Lock()
	m.mediaBaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	m.mediaPort = strings.TrimSpace(port)
	m.mu.Unlock()
}

func (m *Manager) mediaOriginFor(dev Device) (string, error) {
	m.mu.RLock()
	baseURL, port := m.mediaBaseURL, m.mediaPort
	m.mu.RUnlock()
	if baseURL != "" {
		return baseURL, nil
	}
	if port == "" {
		port = defaultMediaPort
	}
	ip, err := routedLocalIP(dev.Addr)
	if err != nil {
		return "", fmt.Errorf("cast: selecting Heya address for %s: %w", dev.Name, err)
	}
	return "http://" + net.JoinHostPort(ip.String(), port), nil
}

// routedLocalIP asks the kernel routing table which source address it would
// use for the receiver. UDP Connect performs no network exchange; it only
// selects a route and therefore also works while the Cast control port is
// temporarily unavailable.
func routedLocalIP(receiver string) (net.IP, error) {
	ip := net.ParseIP(strings.TrimSpace(receiver))
	if ip == nil {
		resolved, err := net.DefaultResolver.LookupIPAddr(context.Background(), receiver)
		if err != nil {
			return nil, fmt.Errorf("resolve receiver %q: %w", receiver, err)
		}
		if len(resolved) == 0 {
			return nil, fmt.Errorf("resolve receiver %q: no addresses", receiver)
		}
		ip = resolved[0].IP
	}
	conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: ip, Port: 9})
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || local.IP == nil || local.IP.IsUnspecified() {
		return nil, fmt.Errorf("route returned no usable local address")
	}
	return local.IP, nil
}

func (m *Manager) mediaURLFor(dev Device, userID int64, track TrackInfo) (string, error) {
	if track.PullPath == "" {
		return "", fmt.Errorf("cast: media has no receiver-pull path")
	}
	origin, err := m.mediaOriginFor(dev)
	if err != nil {
		return "", err
	}
	scopePath := track.PullPath
	subtree := false
	if track.PullScopePath != "" {
		scopePath = strings.TrimRight(track.PullScopePath, "/")
		subtree = true
		if track.PullPath != scopePath && !strings.HasPrefix(track.PullPath, scopePath+"/") {
			return "", fmt.Errorf("cast: media pull path escapes its token scope")
		}
	}
	token, err := m.issueMediaToken(userID, scopePath, track.Duration, subtree)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(origin + track.PullPath)
	if err != nil {
		return "", fmt.Errorf("cast: building media URL: %w", err)
	}
	q, err := url.ParseQuery(track.PullQuery)
	if err != nil {
		return "", fmt.Errorf("cast: invalid media query: %w", err)
	}
	q.Set("cast_token", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// mediaDependencyURL gives an HLS/text-track dependency the same receiver
// origin and scoped token as the primary media URL. Other primary query
// options (sid/audio/quality) are intentionally not copied to subtitles.
func mediaDependencyURL(primaryURL, dependencyPath string) (string, error) {
	primary, err := url.Parse(primaryURL)
	if err != nil || primary.Scheme == "" || primary.Host == "" {
		return "", fmt.Errorf("cast: invalid primary media URL")
	}
	token := primary.Query().Get("cast_token")
	if token == "" || !strings.HasPrefix(dependencyPath, "/") {
		return "", fmt.Errorf("cast: invalid media dependency")
	}
	primary.Path = dependencyPath
	primary.RawPath = ""
	query := url.Values{}
	query.Set("cast_token", token)
	primary.RawQuery = query.Encode()
	primary.Fragment = ""
	return primary.String(), nil
}

func (m *Manager) issueMediaToken(userID int64, path string, durationSec int, subtree bool) (string, error) {
	ttl := time.Duration(durationSec)*time.Second + mediaTokenGrace
	if ttl < mediaTokenGrace {
		ttl = mediaTokenGrace
	}
	if ttl > maxMediaTokenTTL {
		ttl = maxMediaTokenTTL
	}
	claims := mediaTokenClaims{
		Version: 1,
		UserID:  userID,
		Path:    path,
		Subtree: subtree,
		Expires: time.Now().Add(ttl).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	sig := m.signMediaToken(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (m *Manager) signMediaToken(payload []byte) []byte {
	m.mu.RLock()
	key := m.mediaTokenKey
	m.mu.RUnlock()
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

// ValidateMediaToken verifies a receiver token against its exact HTTP path or
// (for HLS) its one resource subtree. It returns the owning user so the HTTP
// handler can recheck the live casting allowlist before serving any bytes.
func (m *Manager) ValidateMediaToken(token, expectedPath string) (int64, error) {
	payloadPart, sigPart, ok := strings.Cut(token, ".")
	if !ok || payloadPart == "" || sigPart == "" {
		return 0, fmt.Errorf("invalid cast media token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return 0, fmt.Errorf("invalid cast media token")
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigPart)
	if err != nil || !hmac.Equal(sig, m.signMediaToken(payload)) {
		return 0, fmt.Errorf("invalid cast media token")
	}
	var claims mediaTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Version != 1 || claims.UserID <= 0 {
		return 0, fmt.Errorf("invalid cast media token")
	}
	pathMatches := claims.Path == expectedPath
	if claims.Subtree {
		pathMatches = pathMatches || strings.HasPrefix(expectedPath, strings.TrimRight(claims.Path, "/")+"/")
	}
	if !pathMatches || time.Now().Unix() >= claims.Expires {
		return 0, fmt.Errorf("expired or mismatched cast media token")
	}
	return claims.UserID, nil
}

func newMediaTokenKey() []byte {
	key := make([]byte, sha256.Size)
	if _, err := rand.Read(key); err != nil {
		// A predictable fallback would turn scoped URLs into forgeable library
		// access. crypto/rand failure means this process cannot cast safely.
		panic(fmt.Sprintf("cast: cannot initialize media token key: %v", err))
	}
	return key
}
