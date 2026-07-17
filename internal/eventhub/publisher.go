package eventhub

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// RelayPublisher implements the worker.EventPublisher shape without importing
// the worker package. A dedicated worker process has no WebSocket subscribers
// of its own, so its events must cross Postgres LISTEN/NOTIFY to the serve
// process that owns browser connections.
//
// Emission is deliberately non-blocking: UI telemetry must never hold a River
// worker open behind a slow database notification. The queue is generously
// sized and dropped telemetry is summarized instead of logging once per event.
type RelayPublisher struct {
	ctx     context.Context
	db      *pgxpool.Pool
	events  chan relayPublication
	dropped atomic.Uint64
}

type relayPublication struct {
	typeName EventType
	payload  any
}

func NewRelayPublisher(ctx context.Context, db *pgxpool.Pool) *RelayPublisher {
	p := &RelayPublisher{
		ctx:    ctx,
		db:     db,
		events: make(chan relayPublication, 1024),
	}
	go p.run()
	return p
}

func (p *RelayPublisher) Emit(eventType EventType, payload any) {
	if p == nil || p.db == nil || p.ctx.Err() != nil {
		return
	}
	select {
	case p.events <- relayPublication{typeName: eventType, payload: payload}:
	default:
		p.dropped.Add(1)
	}
}

func (p *RelayPublisher) run() {
	report := time.NewTicker(30 * time.Second)
	defer report.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case publication := <-p.events:
			notifyCtx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
			err := Notify(notifyCtx, p.db, publication.typeName, publication.payload)
			cancel()
			if err != nil && p.ctx.Err() == nil {
				log.Warn().Err(err).Str("event_type", string(publication.typeName)).Msg("eventhub: worker event relay failed")
			}
		case <-report.C:
			if dropped := p.dropped.Swap(0); dropped > 0 {
				log.Warn().Uint64("dropped", dropped).Msg("eventhub: worker relay queue overflowed")
			}
		}
	}
}
