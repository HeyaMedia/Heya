package auth

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

type blockingSessionLookup struct {
	started chan string
	calls   atomic.Int32
}

func (s *blockingSessionLookup) GetSessionWithUserByToken(context.Context, string) (sqlc.GetSessionWithUserByTokenRow, error) {
	return sqlc.GetSessionWithUserByTokenRow{}, nil
}

func (s *blockingSessionLookup) GetUserByID(context.Context, int64) (sqlc.User, error) {
	return sqlc.User{}, nil
}

func (s *blockingSessionLookup) TouchSession(ctx context.Context, tokenHash string) error {
	s.calls.Add(1)
	s.started <- tokenHash
	<-ctx.Done()
	return ctx.Err()
}

func TestAsyncSessionLookupCoalescesAndJoinsTouches(t *testing.T) {
	underlying := &blockingSessionLookup{started: make(chan string, 1)}
	lookup := NewAsyncSessionLookup(context.Background(), underlying)

	TouchSessionAsync(lookup, "opaque-token")
	if got := <-underlying.started; got != TokenHash("opaque-token") {
		t.Fatalf("queued token hash = %q, want canonical hash", got)
	}
	// The first write is still in flight, so a burst for the same session is
	// represented by that one pending update.
	TouchSessionAsync(lookup, "opaque-token")
	if got := underlying.calls.Load(); got != 1 {
		t.Fatalf("touch calls = %d, want 1 coalesced call", got)
	}

	lookup.Close()
	lookup.Close()
	select {
	case <-lookup.done:
	default:
		t.Fatal("Close returned before the touch worker exited")
	}
}
