package server

import (
	"strings"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/eventhub"
)

func TestWSEventCoalescerLatestScannerEventPerLibrary(t *testing.T) {
	c := newWSEventCoalescer()
	for seq, path := range []string{"first/file.mkv", "second/file.mkv"} {
		queued := c.Queue(eventhub.Event{
			Type:      eventhub.EventScannerEvent,
			Timestamp: time.Unix(int64(seq), 0),
			Payload: eventhub.ScannerEventPayload{
				Seq:       int64(seq),
				LibraryID: 7,
				Event:     "file.classified",
				RelPath:   path,
				Data:      map[string]any{"large": strings.Repeat("x", 4096)},
			},
		})
		if !queued {
			t.Fatal("scanner event was not queued")
		}
	}

	drained := c.Drain()
	if len(drained) != 1 {
		t.Fatalf("drained %d events, want 1", len(drained))
	}
	p := drained[0].Payload.(eventhub.ScannerEventPayload)
	if p.Detail != "second/file.mkv" || p.Seq != 1 {
		t.Fatalf("unexpected latest payload: %+v", p)
	}
	if p.Data != nil || p.Path != "" || p.RelPath != "" || p.Root != "" {
		t.Fatalf("scanner payload was not compacted: %+v", p)
	}
}

func TestWSEventCoalescerKeepsLibrariesSeparate(t *testing.T) {
	c := newWSEventCoalescer()
	for _, id := range []int64{3, 1} {
		c.Queue(eventhub.Event{Type: eventhub.EventScannerEvent, Payload: eventhub.ScannerEventPayload{LibraryID: id}})
	}
	drained := c.Drain()
	if len(drained) != 2 {
		t.Fatalf("drained %d events, want 2", len(drained))
	}
	first := drained[0].Payload.(eventhub.ScannerEventPayload)
	second := drained[1].Payload.(eventhub.ScannerEventPayload)
	if first.LibraryID != 1 || second.LibraryID != 3 {
		t.Fatalf("unexpected library order: %d, %d", first.LibraryID, second.LibraryID)
	}
}

func TestWSEventCoalescerMergesTaskCountsAndCurrentItem(t *testing.T) {
	c := newWSEventCoalescer()
	c.Queue(eventhub.Event{Type: eventhub.EventTaskProgress, Payload: eventhub.TaskProgressPayload{
		TaskID: "sonic", State: "running", Pending: 12, Running: 2,
	}})
	c.Queue(eventhub.Event{Type: eventhub.EventTaskProgress, Payload: eventhub.TaskProgressPayload{
		TaskID: "sonic", State: "running", CurrentItem: "Track", ItemKind: "analyze", CurrentStage: "CLAP",
	}})

	drained := c.Drain()
	if len(drained) != 1 {
		t.Fatalf("drained %d events, want 1", len(drained))
	}
	p := drained[0].Payload.(eventhub.TaskProgressPayload)
	if p.Pending != 12 || p.Running != 2 || p.CurrentItem != "Track" || p.CurrentStage != "CLAP" {
		t.Fatalf("task halves were not merged: %+v", p)
	}
}

func TestWSEventCoalescerScanCompletionDropsPendingDetail(t *testing.T) {
	c := newWSEventCoalescer()
	c.Queue(eventhub.Event{Type: eventhub.EventScannerEvent, Payload: eventhub.ScannerEventPayload{LibraryID: 9}})
	completion := eventhub.Event{Type: eventhub.EventScanCompleted, Payload: eventhub.ScanPayload{LibraryID: 9}}
	if c.Queue(completion) {
		t.Fatal("scan completion must remain immediate")
	}
	if drained := c.Drain(); len(drained) != 0 {
		t.Fatalf("drained %d stale events after completion", len(drained))
	}
}
