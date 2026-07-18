package eventhub

import (
	"context"
	"sync"
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
	ctx       context.Context
	cancel    context.CancelFunc
	db        *pgxpool.Pool
	events    chan relayPublication
	dropped   atomic.Uint64
	done      chan struct{}
	closeOnce sync.Once
}

type relayPublication struct {
	typeName EventType
	payload  any
}

func NewRelayPublisher(ctx context.Context, db *pgxpool.Pool) *RelayPublisher {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	p := &RelayPublisher{
		ctx:    runCtx,
		cancel: cancel,
		db:     db,
		events: make(chan relayPublication, 1024),
		done:   make(chan struct{}),
	}
	go func() {
		defer close(p.done)
		p.run()
	}()
	return p
}

// Close cancels and joins the notifier loop. It is safe to call repeatedly;
// once closed, Emit drops new telemetry instead of touching the database.
func (p *RelayPublisher) Close() {
	if p == nil {
		return
	}
	p.closeOnce.Do(func() {
		p.cancel()
		<-p.done
	})
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
