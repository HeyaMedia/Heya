package worker

import (
	"strings"
	"sync"
	"testing"

	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/scanner"
)

type capturingEventPublisher struct {
	mu      sync.Mutex
	events  []eventhub.EventType
	payload []any
}

func (p *capturingEventPublisher) Emit(eventType eventhub.EventType, payload any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, eventType)
	p.payload = append(p.payload, payload)
}

func (p *capturingEventPublisher) last(t *testing.T) (eventhub.EventType, any) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.events) == 0 {
		t.Fatal("no event captured")
	}
	return p.events[len(p.events)-1], p.payload[len(p.payload)-1]
}

func TestTaskProgressRedactsCredentialedPaths(t *testing.T) {
	t.Parallel()

	publisher := &capturingEventPublisher{}
	broadcaster := NewTaskProgressBroadcaster(publisher)
	broadcaster.emit(eventhub.TaskProgressPayload{
		TaskID:       "test",
		CurrentItem:  "https://reader:super-secret@storage.test/share/movie.mkv",
		CurrentStage: "probing https://reader:super-secret@storage.test/share/movie.mkv",
	})

	_, raw := publisher.last(t)
	payload := raw.(eventhub.TaskProgressPayload)
	if strings.Contains(payload.CurrentItem, "super-secret") || strings.Contains(payload.CurrentStage, "super-secret") {
		t.Fatalf("task progress retained credentials: %+v", payload)
	}
	current, ok := broadcaster.Current("test")
	if !ok || current != payload {
		t.Fatalf("retained task progress = %+v, %v; want emitted payload %+v", current, ok, payload)
	}
}

func TestScannerEventBridgeRedactsCredentialedFields(t *testing.T) {
	t.Parallel()

	publisher := &capturingEventPublisher{}
	bridge := newScannerEventBridge(publisher, "scanner")
	credentialed := "sftp://reader:super-secret@storage.test/share/movie.mkv"
	if err := bridge.WriteEvent(scanner.Event{
		Event:       "root.enter",
		LibraryName: credentialed,
		Root:        credentialed,
		Path:        credentialed,
		RelPath:     credentialed,
		Reason:      "failed " + credentialed,
		Message:     "open " + credentialed,
		Data: map[string]any{
			"path":   credentialed,
			"nested": []any{map[string]any{"error": "read " + credentialed}},
		},
	}); err != nil {
		t.Fatalf("WriteEvent: %v", err)
	}

	eventType, raw := publisher.last(t)
	if eventType != eventhub.EventScannerEvent {
		t.Fatalf("event type = %q, want %q", eventType, eventhub.EventScannerEvent)
	}
	payload := raw.(eventhub.ScannerEventPayload)
	if strings.Contains(strings.Join([]string{
		payload.LibraryName, payload.Root, payload.Path, payload.RelPath,
		payload.Reason, payload.Message,
	}, " "), "super-secret") {
		t.Fatalf("scanner event retained credentials: %+v", payload)
	}
	if strings.Contains(payload.Data["path"].(string), "super-secret") ||
		strings.Contains(payload.Data["nested"].([]any)[0].(map[string]any)["error"].(string), "super-secret") {
		t.Fatalf("scanner event data retained credentials: %+v", payload.Data)
	}
}
