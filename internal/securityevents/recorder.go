// Package securityevents retains a small, process-local view of authentication
// and WAF signals for the admin security dashboard. It deliberately stores no
// credentials, headers, request bodies, or WAF match data.
package securityevents

import (
	"strings"
	"sync"
	"time"
)

const (
	KindLoginFailed           = "login_failed"
	KindLoginThrottled        = "login_throttled"
	KindRegistrationThrottled = "registration_throttled"
	KindVerifierSaturated     = "verifier_saturated"
	KindWAFMatch              = "waf_match"
	KindWAFBlock              = "waf_block"
)

// Event is the intentionally narrow, admin-visible security event shape.
// AccountKey is a short one-way correlation key, never the submitted username.
type SecurityEvent struct {
	ID            uint64    `json:"id"`
	Time          time.Time `json:"time"`
	Kind          string    `json:"kind"`
	Surface       string    `json:"surface,omitempty"`
	ClientIP      string    `json:"client_ip,omitempty"`
	AccountKey    string    `json:"account_key,omitempty"`
	Action        string    `json:"action,omitempty"`
	RuleID        string    `json:"rule_id,omitempty"`
	Severity      string    `json:"severity,omitempty"`
	Message       string    `json:"message,omitempty"`
	Path          string    `json:"path,omitempty"`
	TransactionID string    `json:"transaction_id,omitempty"`
}

type SecurityCounters struct {
	LoginFailures         uint64 `json:"login_failures"`
	LoginThrottled        uint64 `json:"login_throttled"`
	RegistrationThrottled uint64 `json:"registration_throttled"`
	VerifierSaturated     uint64 `json:"verifier_saturated"`
	WAFMatches            uint64 `json:"waf_matches"`
	WAFBlocked            uint64 `json:"waf_blocked"`
}

type SecurityEventSnapshot struct {
	Counters SecurityCounters `json:"counters"`
	Recent   []SecurityEvent  `json:"recent"`
	Capacity int              `json:"capacity"`
}

// Recorder is a fixed-size chronological ring. Security traffic can be
// attacker-controlled, so both memory and field sizes are bounded.
type Recorder struct {
	mu       sync.RWMutex
	events   []SecurityEvent
	size     int
	pos      int
	full     bool
	nextID   uint64
	counters SecurityCounters
}

func New(size int) *Recorder {
	if size < 1 {
		size = 1
	}
	return &Recorder{events: make([]SecurityEvent, size), size: size}
}

func (r *Recorder) Record(event SecurityEvent) {
	if r == nil {
		return
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	event.Kind = bounded(event.Kind, 48)
	event.Surface = bounded(event.Surface, 48)
	event.ClientIP = bounded(event.ClientIP, 64)
	event.AccountKey = bounded(event.AccountKey, 32)
	event.Action = bounded(event.Action, 32)
	event.RuleID = bounded(event.RuleID, 24)
	event.Severity = bounded(event.Severity, 24)
	event.Message = bounded(event.Message, 280)
	event.Path = bounded(event.Path, 512)
	event.TransactionID = bounded(event.TransactionID, 64)

	r.mu.Lock()
	r.nextID++
	event.ID = r.nextID
	switch event.Kind {
	case KindLoginFailed:
		r.counters.LoginFailures++
	case KindLoginThrottled:
		r.counters.LoginThrottled++
	case KindRegistrationThrottled:
		r.counters.RegistrationThrottled++
	case KindVerifierSaturated:
		r.counters.VerifierSaturated++
	case KindWAFMatch:
		r.counters.WAFMatches++
	case KindWAFBlock:
		r.counters.WAFBlocked++
	}
	r.events[r.pos] = event
	r.pos = (r.pos + 1) % r.size
	if r.pos == 0 {
		r.full = true
	}
	r.mu.Unlock()
}

func (r *Recorder) Snapshot(limit int) SecurityEventSnapshot {
	if r == nil {
		return SecurityEventSnapshot{Recent: []SecurityEvent{}}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	total := r.pos
	if r.full {
		total = r.size
	}
	if limit <= 0 || limit > total {
		limit = total
	}
	recent := make([]SecurityEvent, limit)
	for i := 0; i < limit; i++ {
		idx := r.pos - limit + i
		if idx < 0 {
			idx += r.size
		}
		recent[i] = r.events[idx]
	}
	return SecurityEventSnapshot{Counters: r.counters, Recent: recent, Capacity: r.size}
}

func bounded(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) > max {
		return value[:max]
	}
	return value
}
