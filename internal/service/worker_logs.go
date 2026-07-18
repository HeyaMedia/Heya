package service

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/logbuf"
)

// StartWorkerLogRelay sends the dedicated worker's structured logs through
// the existing non-blocking PostgreSQL event relay. The API stores relayed
// entries in its own bounded ring so both the live WebSocket tail and the
// /api/logs backfill contain serve and worker activity.
func (a *App) StartWorkerLogRelay(ctx context.Context, ring *logbuf.RingBuffer) {
	if a == nil || ring == nil || a.relayPublisher == nil {
		return
	}
	a.startBackground(func() {
		workCtx, cancel := a.backgroundContext(ctx)
		defer cancel()
		entries := ring.Subscribe()
		defer ring.Unsubscribe(entries)
		for {
			select {
			case <-workCtx.Done():
				return
			case entry, ok := <-entries:
				if !ok {
					return
				}
				// A failed relay logs its own warning. Re-enqueuing that warning
				// would amplify a database outage into a self-sustaining loop.
				if strings.HasPrefix(entry.Message, "eventhub: worker event relay") {
					continue
				}
				a.relayPublisher.Emit(eventhub.EventLog, boundedWorkerLogPayload(entry))
			}
		}
	})
}

const maxWorkerLogRelayBytes = 7000

func boundedWorkerLogPayload(entry logbuf.Entry) eventhub.LogPayload {
	payload := eventhub.LogPayload{
		Time: entry.Time, Source: "worker", Level: entry.Level,
		Message: entry.Message, Fields: entry.Fields,
	}
	if raw, err := json.Marshal(payload); err == nil && len(raw) <= maxWorkerLogRelayBytes {
		return payload
	}
	// PostgreSQL NOTIFY payloads are capped below 8 KiB. Preserve the high
	// signal text and mark the loss instead of turning an unusually large
	// structured field into a relay failure (which itself would create a log).
	payload.Message = truncateUTF8Bytes(payload.Message, 5000)
	payload.Fields = map[string]any{"relay_truncated": true}
	return payload
}

func truncateUTF8Bytes(value string, max int) string {
	if max < 1 || len(value) <= max {
		return value
	}
	value = value[:max]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + "…"
}
