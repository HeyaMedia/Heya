package worker

import (
	"strings"
	"sync"
	"time"

	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/scanner"
)

const scannerFileEventInterval = 300 * time.Millisecond

type scannerEventBridge struct {
	hub    EventPublisher
	worker string

	mu       sync.Mutex
	lastPath time.Time
}

func newScannerEventBridge(hub EventPublisher, worker string) scanner.EventWriter {
	if hub == nil {
		return nil
	}
	return &scannerEventBridge{hub: hub, worker: worker}
}

func (b *scannerEventBridge) WriteEvent(ev scanner.Event) error {
	if b == nil || b.hub == nil {
		return nil
	}
	if b.dropPathEvent(ev) {
		return nil
	}
	b.hub.Emit(eventhub.EventScannerEvent, eventhub.ScannerEventPayload{
		Seq:         ev.Seq,
		Event:       ev.Event,
		Severity:    string(ev.Severity),
		LibraryID:   ev.LibraryID,
		LibraryName: ev.LibraryName,
		LibraryType: ev.LibraryType,
		Domain:      ev.Domain,
		Worker:      b.worker,
		Phase:       scannerEventPhase(ev),
		Root:        ev.Root,
		Path:        ev.Path,
		RelPath:     ev.RelPath,
		Kind:        ev.Kind,
		Reason:      ev.Reason,
		Message:     ev.Message,
		Data:        ev.Data,
	})
	return nil
}

func (b *scannerEventBridge) dropPathEvent(ev scanner.Event) bool {
	if !isPathNoiseEvent(ev.Event) {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	if b.lastPath.IsZero() || now.Sub(b.lastPath) >= scannerFileEventInterval {
		b.lastPath = now
		return false
	}
	return true
}

func isPathNoiseEvent(name string) bool {
	return strings.HasPrefix(name, "file.") ||
		name == "parse.result" ||
		name == "dir.ignored" ||
		name == "dir.extra"
}

func scannerEventPhase(ev scanner.Event) string {
	if phase, ok := ev.Data["phase"].(string); ok {
		return phase
	}
	switch {
	case strings.HasPrefix(ev.Event, "root.") ||
		strings.HasPrefix(ev.Event, "walk.") ||
		strings.HasPrefix(ev.Event, "file.") ||
		strings.HasPrefix(ev.Event, "dir.") ||
		strings.HasPrefix(ev.Event, "parse.") ||
		strings.HasPrefix(ev.Event, "nfo.") ||
		strings.HasPrefix(ev.Event, "plexmatch.") ||
		strings.HasPrefix(ev.Event, "domain."):
		return string(scanner.PhaseAnalyze)
	case strings.HasPrefix(ev.Event, "match."):
		return string(scanner.PhaseSearch)
	case strings.HasPrefix(ev.Event, "metadata."):
		return string(scanner.PhaseFetch)
	case strings.HasPrefix(ev.Event, "materialize.preview"):
		return string(scanner.PhaseMaterialize)
	case strings.HasPrefix(ev.Event, "materialize.apply"):
		return string(scanner.PhaseApply)
	default:
		return ""
	}
}
