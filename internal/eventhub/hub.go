package eventhub

import (
	"sync"
	"time"
)

type Hub struct {
	mu sync.RWMutex
	// subs maps each subscriber channel to the user it belongs to. 0 means an
	// anonymous/internal consumer (cross-process relay, periodic emitters,
	// Jellyfin bridge) that receives every broadcast but no user-targeted
	// event. Authenticated WebSocket connections register with their real
	// user id via SubscribeUser so PublishToUser can reach only them.
	subs map[chan Event]int64
}

func New() *Hub {
	return &Hub{subs: make(map[chan Event]int64)}
}

func (h *Hub) Publish(event Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (h *Hub) Emit(t EventType, payload any) {
	h.Publish(Event{Type: t, Timestamp: time.Now(), Payload: payload})
}

// PublishToUser delivers an event only to subscribers registered for userID
// (via SubscribeUser). Anonymous/internal subscribers (Subscribe, id 0) never
// receive it. Use this for anything carrying data one user must not see about
// another: the global Publish fan-out has no per-recipient filtering, so a
// broadcast would leak the payload to every connected client. Delivery is
// in-process only — a userID's connection on another process (multi-pod) is
// not reached; single-pod deployments (the norm) are unaffected.
func (h *Hub) PublishToUser(userID int64, event Event) {
	if userID == 0 {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch, uid := range h.subs {
		if uid != userID {
			continue
		}
		select {
		case ch <- event:
		default:
		}
	}
}

func (h *Hub) EmitToUser(userID int64, t EventType, payload any) {
	h.PublishToUser(userID, Event{Type: t, Timestamp: time.Now(), Payload: payload})
}

// Subscribe registers an anonymous consumer that receives every broadcast
// (Publish/Emit) but no user-targeted events. For internal consumers not tied
// to a single user (relay, periodic emitters, the Jellyfin socket bridge).
func (h *Hub) Subscribe() chan Event {
	return h.subscribe(0)
}

// SubscribeUser registers a consumer bound to userID so it additionally
// receives PublishToUser events aimed at that user. Used by authenticated
// WebSocket connections.
func (h *Hub) SubscribeUser(userID int64) chan Event {
	return h.subscribe(userID)
}

func (h *Hub) subscribe(userID int64) chan Event {
	ch := make(chan Event, 256)
	h.mu.Lock()
	h.subs[ch] = userID
	h.mu.Unlock()
	return ch
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

// SubscriberCount returns the live subscriber count — useful for the debug
// stats endpoint to detect WS connection leaks at a glance.
func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}
