package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/stretchr/testify/require"
)

func TestUpdateWatchProgressDoesNotEmitWhenPersistenceFails(t *testing.T) {
	// pgxpool is lazy, and the already-cancelled operation below never reaches
	// this intentionally unreachable address. This gives the service a real
	// pool while keeping the test independent of a running Postgres instance.
	pool, err := pgxpool.New(context.Background(), "postgres://test:test@127.0.0.1:1/heya?sslmode=disable")
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	hub := eventhub.New()
	owner := hub.SubscribePrincipal(eventhub.SubscriberPrincipal{UserID: 41})
	internal := hub.Subscribe()
	app := &App{db: pool, hub: hub}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = app.UpdateWatchProgress(ctx, 41, "movie", 99, 90, 100)
	require.Error(t, err)

	for name, ch := range map[string]<-chan eventhub.Event{"owner": owner, "internal": internal} {
		select {
		case event := <-ch:
			t.Fatalf("%s received %q even though watch progress was not persisted", name, event.Type)
		default:
		}
	}
}
