package eventhub

import (
	"context"
	"testing"
)

func requireEvent(t *testing.T, ch <-chan Event, want EventType) Event {
	t.Helper()
	select {
	case event := <-ch:
		if event.Type != want {
			t.Fatalf("event type = %q, want %q", event.Type, want)
		}
		return event
	default:
		t.Fatalf("expected %q event", want)
		return Event{}
	}
}

func requireNoEvent(t *testing.T, ch <-chan Event) {
	t.Helper()
	select {
	case event := <-ch:
		t.Fatalf("unexpected %q event", event.Type)
	default:
	}
}

func TestClientDevicesAreUserScopedAndUpserted(t *testing.T) {
	h := New()
	h.UpsertDevice(1, ClientDevice{ID: "client:desktop", Name: "Desktop"})
	h.UpsertDevice(2, ClientDevice{ID: "client:phone", Name: "Phone"})
	h.UpsertDevice(1, ClientDevice{ID: "client:desktop", Name: "Office Desktop", State: map[string]any{"playing": true}})

	one := h.ClientDevices(1)
	if len(one) != 1 || one[0].Name != "Office Desktop" || one[0].State["playing"] != true {
		t.Fatalf("unexpected user 1 devices: %#v", one)
	}
	two := h.ClientDevices(2)
	if len(two) != 1 || two[0].ID != "client:phone" {
		t.Fatalf("unexpected user 2 devices: %#v", two)
	}
}

func TestBroadcastVisibilityUsesSubscriberPrincipal(t *testing.T) {
	h := New()
	internal := h.Subscribe()
	admin := h.SubscribePrincipal(SubscriberPrincipal{UserID: 1, IsAdmin: true})
	user := h.SubscribePrincipal(SubscriberPrincipal{UserID: 2})

	// Operational events may contain logs, paths, job arguments, or network
	// state. Trusted internal consumers and admins see them; regular users do
	// not, even when a publisher uses the ordinary broadcast API.
	h.Emit(EventLog, LogPayload{Message: "secret-ish operational detail"})
	requireEvent(t, internal, EventLog)
	requireEvent(t, admin, EventLog)
	requireNoEvent(t, user)

	// Newly introduced event types fail closed until their policy is explicit.
	h.Emit(EventType("future.event"), map[string]any{"detail": "unclassified"})
	requireEvent(t, internal, EventType("future.event"))
	requireEvent(t, admin, EventType("future.event"))
	requireNoEvent(t, user)

	// Catalog invalidations remain authenticated broadcasts.
	h.Emit(EventMediaAdded, MediaPayload{MediaItemID: 9})
	requireEvent(t, internal, EventMediaAdded)
	requireEvent(t, admin, EventMediaAdded)
	requireEvent(t, user, EventMediaAdded)

	// A missing target fails closed instead of leaking per-user state.
	h.Emit(EventRadioICY, RadioICYPayload{Title: "Private station title"})
	requireNoEvent(t, internal)
	requireNoEvent(t, admin)
	requireNoEvent(t, user)
}

func TestTargetedVisibilityPreservesUserAndInternalRouting(t *testing.T) {
	h := New()
	internal := h.Subscribe()
	owner := h.SubscribePrincipal(SubscriberPrincipal{UserID: 11})
	other := h.SubscribePrincipal(SubscriberPrincipal{UserID: 22})
	otherAdmin := h.SubscribePrincipal(SubscriberPrincipal{UserID: 33, IsAdmin: true})

	h.EmitToUser(11, EventRadioICY, RadioICYPayload{Title: "Owner only"})
	requireEvent(t, owner, EventRadioICY)
	requireNoEvent(t, internal)
	requireNoEvent(t, other)
	requireNoEvent(t, otherAdmin)

	h.EmitToUserAndInternal(11, EventMediaWatched, WatchPayload{UserID: 11, MediaItemID: 7})
	requireEvent(t, owner, EventMediaWatched)
	requireEvent(t, internal, EventMediaWatched)
	requireNoEvent(t, other)
	requireNoEvent(t, otherAdmin)

	// Admin-only visibility is still enforced if a caller targets a regular
	// user explicitly.
	h.EmitToUser(11, EventLog, LogPayload{Message: "not for regular users"})
	requireNoEvent(t, owner)
}

func TestHubCloseCancelsAndJoinsOwnedRuntime(t *testing.T) {
	t.Parallel()

	hub := New()
	started := make(chan struct{})
	finished := make(chan struct{})
	if !hub.startRuntime(context.Background(), func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(finished)
	}) {
		t.Fatal("runtime work was not admitted")
	}
	<-started
	hub.Close()
	select {
	case <-finished:
	default:
		t.Fatal("Hub.Close returned before runtime work finished")
	}
	if hub.startRuntime(context.Background(), func(context.Context) {
		t.Error("runtime work started after Hub.Close")
	}) {
		t.Fatal("runtime work was admitted after Hub.Close")
	}
}
