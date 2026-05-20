package logbuf

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

type Entry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

type RingBuffer struct {
	mu      sync.RWMutex
	entries []Entry
	size    int
	pos     int
	full    bool

	subsMu sync.RWMutex
	subs   map[chan Entry]struct{}
}

func New(size int) *RingBuffer {
	return &RingBuffer{
		entries: make([]Entry, size),
		size:    size,
		subs:    make(map[chan Entry]struct{}),
	}
}

func (rb *RingBuffer) Write(p []byte) (n int, err error) {
	e := Entry{Time: time.Now()}
	var raw map[string]any
	if json.Unmarshal(p, &raw) == nil {
		if lvl, ok := raw["level"].(string); ok {
			e.Level = lvl
		}
		if msg, ok := raw["message"].(string); ok {
			e.Message = msg
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
			e.Fields = fields
		}
	} else {
		e.Level = "info"
		e.Message = string(p)
	}

	rb.mu.Lock()
	rb.entries[rb.pos] = e
	rb.pos = (rb.pos + 1) % rb.size
	if rb.pos == 0 {
		rb.full = true
	}
	rb.mu.Unlock()

	rb.subsMu.RLock()
	for ch := range rb.subs {
		select {
		case ch <- e:
		default:
		}
	}
	rb.subsMu.RUnlock()

	return len(p), nil
}

func (rb *RingBuffer) Recent(n int) []Entry {
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
