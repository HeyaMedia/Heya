package cmd

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/logbuf"
)

func TestWaitWithDeadline(t *testing.T) {
	t.Run("completed", func(t *testing.T) {
		var wg sync.WaitGroup
		if !waitWithDeadline(&wg, time.Second) {
			t.Fatal("completed wait group reported a timeout")
		}
	})

	t.Run("timeout", func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(1)
		if waitWithDeadline(&wg, time.Millisecond) {
			wg.Done()
			t.Fatal("blocked wait group reported completion")
		}
		wg.Done()
	})
}

func TestBridgeLogToHubStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ring := logbuf.New(8)
	hub := eventhub.New()
	events := hub.Subscribe()
	defer hub.Unsubscribe(events)

	done := make(chan struct{})
	go func() {
		bridgeLogToHub(ctx, ring, hub)
		close(done)
	}()

	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case ev := <-events:
			if ev.Type != eventhub.EventLog {
				t.Fatalf("bridged event type = %q, want %q", ev.Type, eventhub.EventLog)
			}
			cancel()
			select {
			case <-done:
				return
			case <-time.After(time.Second):
				t.Fatal("log bridge did not stop after context cancellation")
			}
		case <-ticker.C:
			_, _ = ring.Write([]byte(`{"level":"info","message":"hello"}`))
		case <-deadline:
			cancel()
			t.Fatal("log bridge did not forward an entry")
		}
	}
}
