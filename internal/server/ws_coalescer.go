package server

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/karbowiak/heya/internal/eventhub"
)

// wsEventCoalescer keeps high-frequency progress signals out of the browser's
// JSON parser and Vue event loop. The in-process hub remains lossless; only the
// default UI-facing WebSocket stream is reduced to the latest renderable state.
type wsEventCoalescer struct {
	scanProgress *eventhub.Event
	scanner      map[int64]eventhub.Event
	tasks        map[string]eventhub.Event
}

func newWSEventCoalescer() *wsEventCoalescer {
	return &wsEventCoalescer{
		scanner: make(map[int64]eventhub.Event),
		tasks:   make(map[string]eventhub.Event),
	}
}

// Queue returns true when ev was absorbed and should not be written
// immediately. Terminal scan events stay immediate and clear stale progress.
func (c *wsEventCoalescer) Queue(ev eventhub.Event) bool {
	switch ev.Type {
	case eventhub.EventScanProgress:
		copy := ev
		c.scanProgress = &copy
		return true
	case eventhub.EventScannerEvent:
		id, ok := scannerLibraryID(ev.Payload)
		if !ok || id == 0 {
			return false
		}
		ev.Payload = compactScannerPayload(ev.Payload)
		c.scanner[id] = ev
		return true
	case eventhub.EventTaskProgress:
		p, ok := taskProgressPayload(ev.Payload)
		if !ok || p.TaskID == "" {
			return false
		}
		if prev, exists := c.tasks[p.TaskID]; exists {
			if old, valid := taskProgressPayload(prev.Payload); valid {
				p = mergeTaskProgress(old, p)
			}
		}
		ev.Payload = p
		c.tasks[p.TaskID] = ev
		return true
	case eventhub.EventScanCompleted:
		if id, ok := scanLibraryID(ev.Payload); ok {
			delete(c.scanner, id)
		}
		return false
	default:
		return false
	}
}

func (c *wsEventCoalescer) Drain() []eventhub.Event {
	result := make([]eventhub.Event, 0, 1+len(c.scanner)+len(c.tasks))
	if c.scanProgress != nil {
		result = append(result, *c.scanProgress)
		c.scanProgress = nil
	}

	// Stable ordering makes captures/tests readable when several libraries or
	// tasks update inside the same window.
	libIDs := make([]int64, 0, len(c.scanner))
	for id := range c.scanner {
		libIDs = append(libIDs, id)
	}
	sort.Slice(libIDs, func(i, j int) bool { return libIDs[i] < libIDs[j] })
	for _, id := range libIDs {
		result = append(result, c.scanner[id])
		delete(c.scanner, id)
	}

	taskIDs := make([]string, 0, len(c.tasks))
	for id := range c.tasks {
		taskIDs = append(taskIDs, id)
	}
	sort.Strings(taskIDs)
	for _, id := range taskIDs {
		result = append(result, c.tasks[id])
		delete(c.tasks, id)
	}
	return result
}

func mergeTaskProgress(prev, next eventhub.TaskProgressPayload) eventhub.TaskProgressPayload {
	if next.State == "idle" {
		return next
	}
	merged := prev
	merged.TaskID = next.TaskID
	merged.State = next.State

	// Periodic count snapshots carry no item metadata. Worker updates carry an
	// item/stage but no counts; merging the two prevents either half from being
	// lost when both arrive inside one coalescing window.
	if next.CurrentItem == "" && next.ItemKind == "" && next.CurrentStage == "" {
		merged.Pending = next.Pending
		merged.Running = next.Running
	}
	if next.CurrentItem != "" {
		if next.CurrentItem != prev.CurrentItem && next.CurrentStage == "" {
			merged.CurrentStage = ""
		}
		merged.CurrentItem = next.CurrentItem
		merged.ItemKind = next.ItemKind
	}
	if next.CurrentStage != "" {
		merged.CurrentStage = next.CurrentStage
	}
	return merged
}

func compactScannerPayload(payload any) any {
	p, ok := scannerPayload(payload)
	if !ok {
		return payload
	}
	if p.Detail == "" {
		p.Detail = firstNonEmpty(
			p.RelPath,
			p.Root,
			p.Path,
			dataString(p.Data, "title"),
			dataString(p.Data, "artist"),
			dataString(p.Data, "album"),
			dataString(p.Data, "key"),
			dataString(p.Data, "provider_id"),
		)
	}
	p.Root = ""
	p.Path = ""
	p.RelPath = ""
	p.Data = nil
	return p
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func dataString(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func scannerLibraryID(payload any) (int64, bool) {
	p, ok := scannerPayload(payload)
	return p.LibraryID, ok
}

func scannerPayload(payload any) (eventhub.ScannerEventPayload, bool) {
	switch p := payload.(type) {
	case eventhub.ScannerEventPayload:
		return p, true
	case *eventhub.ScannerEventPayload:
		if p == nil {
			return eventhub.ScannerEventPayload{}, false
		}
		return *p, true
	default:
		var decoded eventhub.ScannerEventPayload
		if !decodePayload(payload, &decoded) {
			return decoded, false
		}
		return decoded, true
	}
}

func taskProgressPayload(payload any) (eventhub.TaskProgressPayload, bool) {
	switch p := payload.(type) {
	case eventhub.TaskProgressPayload:
		return p, true
	case *eventhub.TaskProgressPayload:
		if p == nil {
			return eventhub.TaskProgressPayload{}, false
		}
		return *p, true
	default:
		var decoded eventhub.TaskProgressPayload
		if !decodePayload(payload, &decoded) {
			return decoded, false
		}
		return decoded, true
	}
}

func scanLibraryID(payload any) (int64, bool) {
	switch p := payload.(type) {
	case eventhub.ScanPayload:
		return p.LibraryID, true
	case *eventhub.ScanPayload:
		if p == nil {
			return 0, false
		}
		return p.LibraryID, true
	default:
		var decoded eventhub.ScanPayload
		if !decodePayload(payload, &decoded) {
			return 0, false
		}
		return decoded.LibraryID, true
	}
}

func decodePayload(payload any, target any) bool {
	data, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, target) == nil
}
