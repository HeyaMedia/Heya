package eventhub

import (
	"sync"
	"time"
)

type Hub struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}
}

func New() *Hub {
	return &Hub{subs: make(map[chan Event]struct{})}
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

func (h *Hub) Subscribe() chan Event {
	ch := make(chan Event, 256)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
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
