package eventhub

import (
	"context"
	"sync"
	"time"
)

type Hub struct {
	mu sync.RWMutex
	// Browser subscribers carry their authenticated principal so broadcast
	// visibility can be enforced centrally. Trusted in-process consumers use
	// Subscribe, which marks them Internal without pretending they are a user.
	subs    map[chan Event]SubscriberPrincipal
	devices map[int64]map[string]ClientDevice
	// queueStatus is the last lower-cadence river_job snapshot. Both the live
	// queue events and stats emitter consume it so they never run independent
	// full-table counts over a large backlog.
	queueStatus QueueStatusPayload

	runtimeMu     sync.Mutex
	runtimeCtx    context.Context
	runtimeCancel context.CancelFunc
	runtimeClosed bool
	runtimeWG     sync.WaitGroup
}

// SubscriberPrincipal is the identity and trust level attached to one hub
// subscription. Internal must only be set for trusted in-process consumers;
// browser connections should set UserID and IsAdmin from their resolved
// session.
type SubscriberPrincipal struct {
	UserID   int64
	IsAdmin  bool
	Internal bool
}

type ClientDevice struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Kind         string         `json:"kind"`
	Capabilities []string       `json:"capabilities"`
	State        map[string]any `json:"state,omitempty"`
	LastSeen     time.Time      `json:"last_seen"`
}

func New() *Hub {
	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	return &Hub{
		subs:          make(map[chan Event]SubscriberPrincipal),
		devices:       make(map[int64]map[string]ClientDevice),
		runtimeCtx:    runtimeCtx,
		runtimeCancel: runtimeCancel,
	}
}

func (h *Hub) startRuntime(parent context.Context, work func(context.Context)) bool {
	if h == nil || work == nil {
		return false
	}
	if parent == nil {
		parent = context.Background()
	}
	h.runtimeMu.Lock()
	if h.runtimeClosed {
		h.runtimeMu.Unlock()
		return false
	}
	h.runtimeWG.Add(1)
	hubCtx := h.runtimeCtx
	h.runtimeMu.Unlock()

	go func() {
		defer h.runtimeWG.Done()
		ctx, cancel := context.WithCancel(parent)
		stopHubCancel := context.AfterFunc(hubCtx, cancel)
		defer func() {
			stopHubCancel()
			cancel()
		}()
		work(ctx)
	}()
	return true
}

// Close is terminal for Hub-owned relay/telemetry loops. It cancels and joins
// them before App releases the database they query or LISTEN on.
func (h *Hub) Close() {
	if h == nil {
		return
	}
	h.runtimeMu.Lock()
	if !h.runtimeClosed {
		h.runtimeClosed = true
		if h.runtimeCancel != nil {
			h.runtimeCancel()
		}
	}
	h.runtimeMu.Unlock()
	h.runtimeWG.Wait()
}

func (h *Hub) UpsertDevice(userID int64, device ClientDevice) {
	if userID == 0 || device.ID == "" {
		return
	}
	device.LastSeen = time.Now()
	h.mu.Lock()
	if h.devices[userID] == nil {
		h.devices[userID] = make(map[string]ClientDevice)
	}
	h.devices[userID][device.ID] = device
	h.mu.Unlock()
}

func (h *Hub) RemoveDevice(userID int64, deviceID string) {
	h.mu.Lock()
	if devices := h.devices[userID]; devices != nil {
		delete(devices, deviceID)
	}
	h.mu.Unlock()
}

func (h *Hub) ClientDevices(userID int64) []ClientDevice {
	cutoff := time.Now().Add(-35 * time.Second)
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []ClientDevice
	for id, d := range h.devices[userID] {
		if d.LastSeen.Before(cutoff) {
			delete(h.devices[userID], id)
			continue
		}
		out = append(out, d)
	}
	return out
}

func (h *Hub) Publish(event Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	visibility := VisibilityFor(event.Type)
	for ch, principal := range h.subs {
		if !principal.receivesBroadcast(visibility) {
			continue
		}
		select {
		case ch <- event:
		default:
		}
	}
}

func (h *Hub) Emit(t EventType, payload any) {
	h.Publish(Event{Type: t, Timestamp: time.Now(), Payload: payload})
}

// PublishToUser delivers an event only to browser subscribers registered for
// userID. Internal subscribers never receive it. Use this for payloads that
// belong to one user; delivery is in-process only, so a connection owned by
// another server process is not reached.
func (h *Hub) PublishToUser(userID int64, event Event) {
	h.publishToUser(userID, event, false)
}

func (h *Hub) EmitToUser(userID int64, t EventType, payload any) {
	h.PublishToUser(userID, Event{Type: t, Timestamp: time.Now(), Payload: payload})
}

// PublishToUserAndInternal delivers an event to browser subscribers registered
// for userID plus trusted internal subscribers. Use it for per-user payloads
// that a protocol bridge must also observe: the bridge performs its own
// user-scoped routing without exposing the payload to other browser users.
func (h *Hub) PublishToUserAndInternal(userID int64, event Event) {
	h.publishToUser(userID, event, true)
}

func (h *Hub) EmitToUserAndInternal(userID int64, t EventType, payload any) {
	h.PublishToUserAndInternal(userID, Event{Type: t, Timestamp: time.Now(), Payload: payload})
}

// Subscribe registers a trusted internal consumer. It receives authenticated
// and admin broadcasts, plus events explicitly sent with an AndInternal
// method, but it does not receive ordinary user-targeted events.
func (h *Hub) Subscribe() chan Event {
	return h.SubscribePrincipal(SubscriberPrincipal{Internal: true})
}

// SubscribeUser registers a consumer bound to userID so it additionally
// receives PublishToUser events aimed at that user. Used by authenticated
// WebSocket connections.
func (h *Hub) SubscribeUser(userID int64) chan Event {
	return h.SubscribePrincipal(SubscriberPrincipal{UserID: userID})
}

// SubscribePrincipal registers an authenticated browser or another explicitly
// described subscriber. WebSocket handlers should use this method so admin
// visibility is derived from the resolved session rather than client input.
func (h *Hub) SubscribePrincipal(principal SubscriberPrincipal) chan Event {
	ch := make(chan Event, 256)
	h.mu.Lock()
	h.subs[ch] = principal
	h.mu.Unlock()
	return ch
}

func (h *Hub) publishToUser(userID int64, event Event, includeInternal bool) {
	if userID == 0 {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	visibility := VisibilityFor(event.Type)
	for ch, principal := range h.subs {
		if !principal.receivesTargeted(userID, visibility, includeInternal) {
			continue
		}
		select {
		case ch <- event:
		default:
		}
	}
}

func (p SubscriberPrincipal) receivesBroadcast(visibility EventVisibility) bool {
	switch visibility {
	case VisibilityAdmin:
		return p.Internal || p.IsAdmin
	case VisibilityUserTargeted:
		return false
	default:
		return true
	}
}

func (p SubscriberPrincipal) receivesTargeted(userID int64, visibility EventVisibility, includeInternal bool) bool {
	if p.Internal {
		return includeInternal
	}
	if p.UserID != userID {
		return false
	}
	return visibility != VisibilityAdmin || p.IsAdmin
}

func (h *Hub) Unsubscribe(ch chan Event) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *Hub) HasSubscribers() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs) > 0
}

func (h *Hub) setQueueStatus(status QueueStatusPayload) {
	h.mu.Lock()
	h.queueStatus = status
	h.mu.Unlock()
}

func (h *Hub) queueStatusSnapshot() QueueStatusPayload {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.queueStatus
}

// SubscriberCount returns the live subscriber count — useful for the debug
// stats endpoint to detect WS connection leaks at a glance.
func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

type SubscriberStats struct {
	WebSocket int
	Admin     int
	Internal  int
}

// SubscriberStats distinguishes actual browser WebSocket connections from
// trusted in-process consumers. Counting both as "WS subscribers" made the
// diagnostics number climb whenever an internal bridge was added.
func (h *Hub) SubscriberStats() SubscriberStats {
	if h == nil {
		return SubscriberStats{}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	var stats SubscriberStats
	for _, principal := range h.subs {
		if principal.Internal {
			stats.Internal++
			continue
		}
		stats.WebSocket++
		if principal.IsAdmin {
			stats.Admin++
		}
	}
	return stats
}
