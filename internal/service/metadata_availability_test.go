package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMetadataStatusTracksProviderComingAndGoing(t *testing.T) {
	app := &App{}
	ctx := context.Background()

	if status := app.MetadataStatus(); !status.CheckedAt.IsZero() {
		t.Fatalf("an unprobed App should report no check, got %+v", status)
	}

	outage := errors.New("dial tcp: connection refused")
	if !app.probeMetadataOnce(ctx, func(context.Context) error { return outage }) {
		t.Fatal("probe reported a finished lifetime")
	}
	status := app.MetadataStatus()
	if status.Available {
		t.Fatal("a failed probe must not report the provider as available")
	}
	if status.LastError != outage.Error() {
		t.Fatalf("LastError = %q, want %q", status.LastError, outage.Error())
	}
	if status.CheckedAt.IsZero() {
		t.Fatal("a completed probe must stamp CheckedAt")
	}

	if !app.probeMetadataOnce(ctx, func(context.Context) error { return nil }) {
		t.Fatal("probe reported a finished lifetime")
	}
	status = app.MetadataStatus()
	if !status.Available {
		t.Fatal("a recovered provider must report as available")
	}
	if status.LastError != "" {
		t.Fatalf("a recovered provider must clear LastError, got %q", status.LastError)
	}
}

// Shutdown cancels the lifetime while a probe is in flight. That failure is the
// process going away, not the provider, and recording it would leave a
// misleading outage as the last thing the status ever said.
func TestMetadataProbeIgnoresLifetimeShutdown(t *testing.T) {
	app := &App{}
	app.setMetadataStatus(MetadataStatus{Available: true, CheckedAt: time.Now()})

	ctx, cancel := context.WithCancel(context.Background())
	if app.probeMetadataOnce(ctx, func(ctx context.Context) error {
		cancel()
		return ctx.Err()
	}) {
		t.Fatal("probe should report a finished lifetime so the watcher stops")
	}

	if status := app.MetadataStatus(); !status.Available {
		t.Fatal("shutdown must not be recorded as a provider outage")
	}
}

func TestMetadataWatcherStopsWithTheLifetime(t *testing.T) {
	app := &App{}
	ctx, cancel := context.WithCancel(context.Background())
	probed := make(chan struct{}, 1)

	app.watchMetadataAvailability(ctx, func(context.Context) error {
		select {
		case probed <- struct{}{}:
		default:
		}
		return nil
	})
	cancel()

	// The first probe is a full interval away, so a cancelled lifetime has to
	// unblock the wait rather than the probe.
	select {
	case <-probed:
		t.Fatal("watcher probed after its lifetime ended")
	case <-time.After(50 * time.Millisecond):
	}
}
