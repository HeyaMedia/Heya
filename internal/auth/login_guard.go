package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

const (
	LoginTrackedKeyCapacity = 4096
	loginGuardMaxKeys       = LoginTrackedKeyCapacity
	loginGuardKeyTTL        = time.Hour

	LoginIPBurst              = 10
	LoginIPRefillSeconds      = 30
	LoginAccountBurst         = 5
	LoginAccountRefillSeconds = 180
)

type loginLimitEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// LoginGuard combines two independent token buckets: one for the source IP
// and one for the normalized account identifier. It also bounds concurrent
// password hashes so an IP-rotating botnet cannot turn the password verifier
// into an unbounded CPU/memory queue.
type LoginGuard struct {
	mu             sync.Mutex
	byIP           map[string]*loginLimitEntry
	byAccount      map[string]*loginLimitEntry
	passwordSlots  chan struct{}
	allowedTotal   atomic.Uint64
	throttledTotal atomic.Uint64
	checksStarted  atomic.Uint64
	saturatedTotal atomic.Uint64
	checksActive   atomic.Int64
}

type LoginGuardStats struct {
	AllowedTotal          uint64 `json:"allowed_total"`
	ThrottledTotal        uint64 `json:"throttled_total"`
	PasswordChecksStarted uint64 `json:"password_checks_started"`
	SaturatedTotal        uint64 `json:"saturated_total"`
	ActiveIPBuckets       int    `json:"active_ip_buckets"`
	ActiveAccountBuckets  int    `json:"active_account_buckets"`
	PasswordChecksActive  int64  `json:"password_checks_active"`
	PasswordCheckCapacity int    `json:"password_check_capacity"`
}

func NewLoginGuard() *LoginGuard {
	slots := runtime.GOMAXPROCS(0)
	if slots < 2 {
		slots = 2
	}
	if slots > 4 {
		slots = 4
	}
	return &LoginGuard{
		byIP:          make(map[string]*loginLimitEntry),
		byAccount:     make(map[string]*loginLimitEntry),
		passwordSlots: make(chan struct{}, slots),
	}
}

// Allow consumes both buckets. They are intentionally separate rather than a
// combined IP+account key: the former catches credential stuffing while the
// latter catches a distributed attack against one user.
func (g *LoginGuard) Allow(clientIP, username string) bool {
	if g == nil {
		return true
	}
	now := time.Now()
	ipKey := strings.TrimSpace(clientIP)
	if ipKey == "" {
		ipKey = "unknown"
	}
	accountKey := AccountKey(username)

	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweepLocked(now)
	ipAllowed := allowLoginKey(g.byIP, ipKey, now, rate.Every(LoginIPRefillSeconds*time.Second), LoginIPBurst)
	accountAllowed := allowLoginKey(g.byAccount, accountKey, now, rate.Every(LoginAccountRefillSeconds*time.Second), LoginAccountBurst)
	allowed := ipAllowed && accountAllowed
	if allowed {
		g.allowedTotal.Add(1)
	} else {
		g.throttledTotal.Add(1)
	}
	return allowed
}

func allowLoginKey(entries map[string]*loginLimitEntry, key string, now time.Time, limit rate.Limit, burst int) bool {
	entry := entries[key]
	if entry == nil {
		entry = &loginLimitEntry{limiter: rate.NewLimiter(limit, burst)}
		entries[key] = entry
	}
	entry.lastSeen = now
	return entry.limiter.AllowN(now, 1)
}

// ClearAccount lets a legitimate successful login recover immediately from
// typing mistakes. The IP bucket is deliberately retained so one valid leaked
// credential cannot reset a credential-stuffing source's allowance.
func (g *LoginGuard) ClearAccount(username string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	delete(g.byAccount, AccountKey(username))
	g.mu.Unlock()
}

// BeginPasswordCheck acquires one bounded verifier slot without waiting.
// Waiting would let untrusted requests accumulate goroutines indefinitely.
func (g *LoginGuard) BeginPasswordCheck() (release func(), ok bool) {
	if g == nil {
		return func() {}, true
	}
	select {
	case g.passwordSlots <- struct{}{}:
		g.checksStarted.Add(1)
		g.checksActive.Add(1)
		var once sync.Once
		return func() {
			once.Do(func() {
				<-g.passwordSlots
				g.checksActive.Add(-1)
			})
		}, true
	default:
		g.saturatedTotal.Add(1)
		return nil, false
	}
}

func (g *LoginGuard) Stats() LoginGuardStats {
	if g == nil {
		return LoginGuardStats{}
	}
	g.mu.Lock()
	activeIPs := len(g.byIP)
	activeAccounts := len(g.byAccount)
	g.mu.Unlock()
	return LoginGuardStats{
		AllowedTotal:          g.allowedTotal.Load(),
		ThrottledTotal:        g.throttledTotal.Load(),
		PasswordChecksStarted: g.checksStarted.Load(),
		SaturatedTotal:        g.saturatedTotal.Load(),
		ActiveIPBuckets:       activeIPs,
		ActiveAccountBuckets:  activeAccounts,
		PasswordChecksActive:  g.checksActive.Load(),
		PasswordCheckCapacity: cap(g.passwordSlots),
	}
}

func (g *LoginGuard) sweepLocked(now time.Time) {
	if len(g.byIP)+len(g.byAccount) <= loginGuardMaxKeys {
		return
	}
	cutoff := now.Add(-loginGuardKeyTTL)
	for key, entry := range g.byIP {
		if entry.lastSeen.Before(cutoff) {
			delete(g.byIP, key)
		}
	}
	for key, entry := range g.byAccount {
		if entry.lastSeen.Before(cutoff) {
			delete(g.byAccount, key)
		}
	}
	// A high-cardinality spray can fill the map entirely inside the TTL. Keep
	// memory bounded by evicting arbitrary old-ish keys; limiter accuracy for a
	// scanning source is less important than server availability.
	for len(g.byIP)+len(g.byAccount) > loginGuardMaxKeys {
		for key := range g.byIP {
			delete(g.byIP, key)
			break
		}
		if len(g.byIP) == 0 {
			for key := range g.byAccount {
				delete(g.byAccount, key)
				break
			}
		}
	}
}

// AccountKey is safe to log: it correlates attempts without retaining or
// exposing the submitted account name.
func AccountKey(username string) string {
	normalized := strings.ToLower(strings.TrimSpace(username))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:8])
}
