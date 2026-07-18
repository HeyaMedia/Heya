package logbuf

import (
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/karbowiak/heya/internal/secrettext"
)

type Entry struct {
	Time    time.Time      `json:"time"`
	Source  string         `json:"source,omitempty"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

type RingBuffer struct {
	mu      sync.RWMutex
	entries []Entry
	size    int
	pos     int
	full    bool
	source  string

	subsMu sync.RWMutex
	subs   map[chan Entry]struct{}
}

func New(size int) *RingBuffer {
	return NewWithSource(size, "serve")
}

func NewWithSource(size int, source string) *RingBuffer {
	if size < 1 {
		size = 1
	}
	return &RingBuffer{
		entries: make([]Entry, size),
		size:    size,
		source:  source,
		subs:    make(map[chan Entry]struct{}),
	}
}

func (rb *RingBuffer) Write(p []byte) (n int, err error) {
	e := Entry{Time: time.Now(), Source: rb.source}
	var raw map[string]any
	if json.Unmarshal(p, &raw) == nil {
		if lvl, ok := raw["level"].(string); ok {
			e.Level = lvl
		}
		if msg, ok := raw["message"].(string); ok {
			e.Message = secrettext.Redact(msg)
		}
		if t, ok := raw["time"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
				e.Time = parsed
			}
		}
		fields := make(map[string]any)
		for k, v := range raw {
			if k != "level" && k != "message" && k != "time" {
				fields[k] = v
			}
		}
		if len(fields) > 0 {
			e.Fields = secrettext.RedactMap(fields)
		}
	} else {
		e.Level = "info"
		e.Message = secrettext.Redact(string(p))
	}

	rb.append(e, true)

	return len(p), nil
}

// Store inserts an already-structured entry without notifying subscribers.
// The API uses this for worker logs that have already arrived over the
// cross-process event relay: storing them makes /api/logs backfill complete
// without emitting the same event back onto the WebSocket hub.
func (rb *RingBuffer) Store(e Entry) {
	if rb == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	if e.Source == "" {
		e.Source = rb.source
	}
	e.Message = secrettext.Redact(e.Message)
	e.Fields = secrettext.RedactMap(e.Fields)
	rb.append(e, false)
}

func (rb *RingBuffer) append(e Entry, publish bool) {
	rb.mu.Lock()
	rb.entries[rb.pos] = e
	rb.pos = (rb.pos + 1) % rb.size
	if rb.pos == 0 {
		rb.full = true
	}
	rb.mu.Unlock()
	if !publish {
		return
	}
	rb.subsMu.RLock()
	defer rb.subsMu.RUnlock()
	for ch := range rb.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func (rb *RingBuffer) Recent(n int) []Entry {
	if rb == nil || n <= 0 {
		return []Entry{}
	}
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	total := rb.pos
	if rb.full {
		total = rb.size
	}
	if n > total {
		n = total
	}

	result := make([]Entry, n)
	for i := 0; i < n; i++ {
		idx := rb.pos - n + i
		if idx < 0 {
			idx += rb.size
		}
		result[i] = rb.entries[idx]
	}
	return result
}

func (rb *RingBuffer) Capacity() int {
	if rb == nil {
		return 0
	}
	return rb.size
}

func (rb *RingBuffer) Subscribe() chan Entry {
	ch := make(chan Entry, 64)
	rb.subsMu.Lock()
	rb.subs[ch] = struct{}{}
	rb.subsMu.Unlock()
	return ch
}

func (rb *RingBuffer) Unsubscribe(ch chan Entry) {
	rb.subsMu.Lock()
	delete(rb.subs, ch)
	rb.subsMu.Unlock()
	close(ch)
}

func (rb *RingBuffer) MultiWriter(original io.Writer) io.Writer {
	return io.MultiWriter(original, rb)
}
