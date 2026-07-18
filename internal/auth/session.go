package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

var ErrInvalidSession = errors.New("invalid session")

type SessionLookup interface {
	GetSessionWithUserByToken(ctx context.Context, tokenHash string) (sqlc.GetSessionWithUserByTokenRow, error)
	GetUserByID(ctx context.Context, id int64) (sqlc.User, error)
	TouchSession(ctx context.Context, tokenHash string) error
}

type SessionResolution struct {
	Session sqlc.Session
	User    sqlc.User
	Token   string
}

// AsyncSessionLookup wraps the database lookup with one bounded, lifecycle-
// owned touch queue. Authentication happens on every API request; spawning a
// detached goroutine for every best-effort last-seen update made shutdown race
// an unbounded number of database users. Lookups remain direct while touches
// are coalesced per token until the queued write completes.
type AsyncSessionLookup struct {
	SessionLookup
	ctx       context.Context
	cancel    context.CancelFunc
	touches   chan string
	pendingMu sync.Mutex
	pending   map[string]struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func NewAsyncSessionLookup(parent context.Context, lookup SessionLookup) *AsyncSessionLookup {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	s := &AsyncSessionLookup{
		SessionLookup: lookup,
		ctx:           ctx,
		cancel:        cancel,
		touches:       make(chan string, 256),
		pending:       make(map[string]struct{}),
		done:          make(chan struct{}),
	}
	go s.runTouches()
	return s
}

func (s *AsyncSessionLookup) queueSessionTouch(token string) {
	if s == nil || s.SessionLookup == nil || token == "" || s.ctx.Err() != nil {
		return
	}
	hash := TokenHash(token)
	s.pendingMu.Lock()
	if _, exists := s.pending[hash]; exists {
		s.pendingMu.Unlock()
		return
	}
	s.pending[hash] = struct{}{}
	s.pendingMu.Unlock()

	select {
	case s.touches <- hash:
	case <-s.ctx.Done():
		s.finishTouch(hash)
	default:
		// Last-seen timestamps are advisory. Under extreme request fan-in, drop
		// rather than letting authentication wait behind telemetry.
		s.finishTouch(hash)
	}
}

func (s *AsyncSessionLookup) runTouches() {
	defer close(s.done)
	for {
		select {
		case <-s.ctx.Done():
			return
		case hash := <-s.touches:
			writeCtx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
			_ = s.TouchSession(writeCtx, hash)
			cancel()
			s.finishTouch(hash)
		}
	}
}

func (s *AsyncSessionLookup) finishTouch(hash string) {
	s.pendingMu.Lock()
	delete(s.pending, hash)
	s.pendingMu.Unlock()
}

func (s *AsyncSessionLookup) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		s.cancel()
		<-s.done
	})
}

func ResolveSession(ctx context.Context, db SessionLookup, token string) (SessionResolution, error) {
	if token == "" || db == nil {
		return SessionResolution{}, ErrInvalidSession
	}
	// One joined round trip: this runs on nearly every API request, and a
	// dangling-session row (user deleted) surfaces as no-rows via the JOIN,
	// same as an unknown token.
	row, err := db.GetSessionWithUserByToken(ctx, TokenHash(token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SessionResolution{}, ErrInvalidSession
		}
		return SessionResolution{}, fmt.Errorf("session lookup failed: %w", err)
	}
	return SessionResolution{Session: row.Session, User: row.User, Token: token}, nil
}

func TouchSessionAsync(db SessionLookup, token string) {
	if db == nil || token == "" {
		return
	}
	if queue, ok := db.(interface{ queueSessionTouch(string) }); ok {
		queue.queueSessionTouch(token)
		return
	}
	// Non-App callers (small protocol/unit-test adapters) have no lifecycle
	// owner. Keep their behavior safe and bounded without creating a goroutine.
	bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = db.TouchSession(bgCtx, TokenHash(token))
}
