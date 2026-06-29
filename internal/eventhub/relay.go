package eventhub

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// pgNotifyChannel is the Postgres LISTEN/NOTIFY channel used to bridge events
// between processes. The in-memory Hub only reaches WebSocket clients in the
// process that owns it (`heya serve`); a mutation performed in another process
// — e.g. a `heya library remove` CLI invocation, which talks straight to the
// database — has no hub with subscribers. NOTIFY carries the event to the
// server, where StartCrossProcessRelay re-emits it onto the live hub.
const pgNotifyChannel = "heya_events"

// Notify publishes an event to other processes via Postgres NOTIFY. It is
// fire-and-forget: if no server process is LISTENing (e.g. a CLI command run
// while nothing is serving), the notification is simply dropped — which is
// correct, since there are then no browsers to update. Safe to call from any
// process that holds a DB pool.
func Notify(ctx context.Context, db *pgxpool.Pool, t EventType, payload any) error {
	buf, err := json.Marshal(Event{Type: t, Timestamp: time.Now(), Payload: payload})
	if err != nil {
		return err
	}
	// pg_notify() with a bind parameter avoids the identifier-quoting dance of
	// the `NOTIFY chan, 'literal'` form. Payloads are tiny (well under the 8000
	// byte limit).
	_, err = db.Exec(ctx, "SELECT pg_notify($1, $2)", pgNotifyChannel, string(buf))
	return err
}

// StartCrossProcessRelay LISTENs for events published by other processes (via
// Notify) and re-emits them onto this in-memory hub, so connected WebSocket
// clients see cross-process mutations. Runs only in the server process,
// alongside the periodic emitters. Holds its own dedicated connection (not a
// pooled one, which can't stay parked on LISTEN) and reconnects with backoff.
func (h *Hub) StartCrossProcessRelay(ctx context.Context, pool *pgxpool.Pool) {
	go h.relayLoop(ctx, pool)
}

func (h *Hub) relayLoop(ctx context.Context, pool *pgxpool.Pool) {
	const maxBackoff = 30 * time.Second
	backoff := time.Second
	for ctx.Err() == nil {
		if err := h.listen(ctx, pool); err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Msg("eventhub: cross-process relay dropped, reconnecting")
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// listen runs one connection's lifetime: connect, LISTEN, then forward
// notifications until the connection errors or the context is cancelled.
// Returns the error that ended the loop so relayLoop can reconnect.
func (h *Hub) listen(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	if _, err := conn.Exec(ctx, "LISTEN "+pgNotifyChannel); err != nil {
		return err
	}

	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var ev Event
		if err := json.Unmarshal([]byte(n.Payload), &ev); err != nil {
			log.Warn().Err(err).Str("payload", n.Payload).Msg("eventhub: bad cross-process notification")
			continue
		}
		h.Publish(ev)
	}
}
