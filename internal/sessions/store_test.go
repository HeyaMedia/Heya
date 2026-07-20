package sessions

import (
	"context"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/eventhub"
)

// newTestStore wires a Store to a Hub subscriber that observes every
// broadcast session.update, so tests can assert on whether Upsert chose to
// broadcast without touching the (deliberately payload-free) event body.
func newTestStore(t *testing.T) (*Store, chan eventhub.Event) {
	t.Helper()
	hub := eventhub.New()
	ch := hub.Subscribe()
	t.Cleanup(func() { hub.Unsubscribe(ch) })
	store := New(context.Background(), hub)
	t.Cleanup(store.Close)
	return store, ch
}

func expectBroadcast(t *testing.T, ch chan eventhub.Event) {
	t.Helper()
	select {
	case ev := <-ch:
		if ev.Type != EventSessionUpdate {
			t.Fatalf("event type = %q, want %q", ev.Type, EventSessionUpdate)
		}
	case <-time.After(time.Second):
		t.Fatal("expected a session.update broadcast, got none")
	}
}

func expectNoBroadcast(t *testing.T, ch chan eventhub.Event) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected broadcast: %#v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestUpsertNewSessionAlwaysBroadcasts(t *testing.T) {
	store, ch := newTestStore(t)
	store.Upsert(Session{SessionID: "s1", UserID: 1, MediaItemID: 10, EntityType: "track", EntityID: 100})
	expectBroadcast(t, ch)
}

func TestUpsertPositionOnlyHeartbeatIsRateLimited(t *testing.T) {
	store, ch := newTestStore(t)
	store.Upsert(Session{SessionID: "s1", UserID: 1, MediaItemID: 10, EntityType: "track", EntityID: 100})
	expectBroadcast(t, ch) // new session

	// Same identity, only position moved — inside the rate-limit window this
	// must not broadcast again.
	store.Upsert(Session{SessionID: "s1", UserID: 1, MediaItemID: 10, EntityType: "track", EntityID: 100, PositionSeconds: 10})
	expectNoBroadcast(t, ch)
}

func TestUpsertPositionOnlyHeartbeatBroadcastsAfterRateLimitWindow(t *testing.T) {
	store, ch := newTestStore(t)
	store.Upsert(Session{SessionID: "s1", UserID: 1, MediaItemID: 10, EntityType: "track", EntityID: 100})
	expectBroadcast(t, ch)

	// Simulate the rate-limit window having already elapsed.
	store.lastBroadcastAt.Store(time.Now().Add(-2 * minHeartbeatBroadcastInterval).UnixNano())

	store.Upsert(Session{SessionID: "s1", UserID: 1, MediaItemID: 10, EntityType: "track", EntityID: 100, PositionSeconds: 20})
	expectBroadcast(t, ch)
}

func TestUpsertTrackChangeBroadcastsImmediately(t *testing.T) {
	store, ch := newTestStore(t)
	store.Upsert(Session{SessionID: "s1", UserID: 1, MediaItemID: 10, EntityType: "track", EntityID: 100})
	expectBroadcast(t, ch)

	// One player instance heartbeats the same session_id across track
	// changes (background music never unmounts); a different media item on
	// the same session is a significant identity change and must broadcast
	// immediately, even inside the rate-limit window.
	store.Upsert(Session{SessionID: "s1", UserID: 1, MediaItemID: 11, EntityType: "track", EntityID: 101})
	expectBroadcast(t, ch)
}

func TestUpsertPauseFlipBroadcastsImmediately(t *testing.T) {
	store, ch := newTestStore(t)
	store.Upsert(Session{SessionID: "s1", UserID: 1, MediaItemID: 10, EntityType: "track", EntityID: 100, Paused: false})
	expectBroadcast(t, ch)

	store.Upsert(Session{SessionID: "s1", UserID: 1, MediaItemID: 10, EntityType: "track", EntityID: 100, Paused: true})
	expectBroadcast(t, ch)
}

func TestEndForUserBroadcastsRegardlessOfRateLimit(t *testing.T) {
	store, ch := newTestStore(t)
	store.Upsert(Session{SessionID: "s1", UserID: 1, MediaItemID: 10, EntityType: "track", EntityID: 100})
	expectBroadcast(t, ch)

	if !store.EndForUser("s1", 1) {
		t.Fatal("EndForUser did not remove the owned session")
	}
	expectBroadcast(t, ch)
}
