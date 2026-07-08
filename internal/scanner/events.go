package scanner

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

type Severity string

const (
	SeverityInfo Severity = "info"
	SeverityWarn Severity = "warn"
)

type Event struct {
	Seq         int64          `json:"seq"`
	Time        time.Time      `json:"time"`
	Event       string         `json:"event"`
	Severity    Severity       `json:"severity"`
	LibraryID   int64          `json:"library_id,omitempty"`
	LibraryName string         `json:"library_name,omitempty"`
	LibraryType string         `json:"library_type,omitempty"`
	Domain      string         `json:"domain,omitempty"`
	Root        string         `json:"root,omitempty"`
	Path        string         `json:"path,omitempty"`
	RelPath     string         `json:"rel_path,omitempty"`
	Kind        string         `json:"kind,omitempty"`
	Reason      string         `json:"reason,omitempty"`
	Message     string         `json:"message,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
}

type Emitter interface {
	Emit(Event)
}

type EventSink struct {
	mu      sync.Mutex
	next    int64
	base    Event
	writers []EventWriter
}

func NewEventSink(base Event, writers ...EventWriter) *EventSink {
	return &EventSink{base: base, writers: writers}
}

func (s *EventSink) Emit(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	ev.Seq = s.next
	if ev.Severity == "" {
		ev.Severity = SeverityInfo
	}
	if ev.LibraryID == 0 {
		ev.LibraryID = s.base.LibraryID
	}
	if ev.LibraryName == "" {
		ev.LibraryName = s.base.LibraryName
	}
	if ev.LibraryType == "" {
		ev.LibraryType = s.base.LibraryType
	}
	if ev.Domain == "" {
		ev.Domain = s.base.Domain
	}
	for _, w := range s.writers {
		_ = w.WriteEvent(ev)
	}
}

type EventWriter interface {
	WriteEvent(Event) error
}

type EventRecorder struct {
	Events []Event
}

func (r *EventRecorder) WriteEvent(ev Event) error {
	r.Events = append(r.Events, ev)
	return nil
}

type JSONLWriter struct {
	w io.Writer
}

func NewJSONLWriter(w io.Writer) *JSONLWriter {
	return &JSONLWriter{w: w}
}

func (w *JSONLWriter) WriteEvent(ev Event) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w.w, string(b))
	return err
}

type HumanWriter struct {
	w io.Writer
}

func NewHumanWriter(w io.Writer) *HumanWriter {
	return &HumanWriter{w: w}
}

func (w *HumanWriter) WriteEvent(ev Event) error {
	parts := []string{fmt.Sprintf("%04d", ev.Seq), ev.Event}
	if ev.Domain != "" {
		parts = append(parts, "domain="+ev.Domain)
	}
	if ev.RelPath != "" {
		parts = append(parts, ev.RelPath)
	} else if ev.Path != "" {
		parts = append(parts, ev.Path)
	}
	if ev.Reason != "" {
		parts = append(parts, "reason="+ev.Reason)
	}
	if ev.Message != "" {
		parts = append(parts, ev.Message)
	}
	if len(ev.Data) > 0 {
		parts = append(parts, formatData(ev.Data))
	}
	_, err := fmt.Fprintln(w.w, strings.Join(parts, "  "))
	return err
}

func formatData(data map[string]any) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, data[k]))
	}
	return strings.Join(parts, " ")
}
