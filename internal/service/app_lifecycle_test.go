package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestAppCloseIsIdempotent(t *testing.T) {
	lifetimeCtx, cancel := context.WithCancel(context.Background())
	var cancelCalls atomic.Int32
	app := &App{
		lifetimeCtx: lifetimeCtx,
		lifetimeCancel: func() {
			cancelCalls.Add(1)
			cancel()
		},
	}

	app.Close()
	app.Close()

	if got := cancelCalls.Load(); got != 1 {
		t.Fatalf("lifetime cancel called %d times, want 1", got)
	}
	select {
	case <-lifetimeCtx.Done():
	default:
		t.Fatal("app lifetime was not cancelled")
	}
}

func TestNilAppClose(t *testing.T) {
	var app *App
	app.Close()
}

func TestAppLifetimeOutlivesConstructorContext(t *testing.T) {
	type contextKey struct{}
	parent, cancelParent := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "preserved"))
	lifetime, cancelLifetime := newAppLifetime(parent)
	app := &App{lifetimeCtx: lifetime, lifetimeCancel: cancelLifetime}

	cancelParent()
	select {
	case <-lifetime.Done():
		t.Fatal("constructor context cancellation unexpectedly cancelled App lifetime")
	default:
	}
	if got := lifetime.Value(contextKey{}); got != "preserved" {
		t.Fatalf("App lifetime context value = %v, want preserved", got)
	}

	app.Close()
	select {
	case <-lifetime.Done():
	default:
		t.Fatal("App.Close did not cancel App lifetime")
	}
}

type countingLease struct {
	closeCalls atomic.Int32
}

func (l *countingLease) Close() error {
	l.closeCalls.Add(1)
	return nil
}

func TestAppCloseReleasesCoordinatorLeaseOnce(t *testing.T) {
	lease := &countingLease{}
	app := &App{coordinatorLease: lease}

	app.Close()
	app.Close()

	if got := lease.closeCalls.Load(); got != 1 {
		t.Fatalf("coordinator lease close called %d times, want 1", got)
	}
}

func TestAppCloseJoinsAdmittedBackgroundWork(t *testing.T) {
	lifetimeCtx, cancel := context.WithCancel(context.Background())
	app := &App{lifetimeCtx: lifetimeCtx, lifetimeCancel: cancel}
	started := make(chan struct{})
	release := make(chan struct{})
	if !app.startBackground(func() {
		close(started)
		<-release
	}) {
		t.Fatal("background work was not admitted")
	}
	<-started

	closed := make(chan struct{})
	go func() {
		app.Close()
		close(closed)
	}()
	<-lifetimeCtx.Done()
	select {
	case <-closed:
		t.Fatal("App.Close returned before background work completed")
	default:
	}
	if app.startBackground(func() { t.Error("late background work ran") }) {
		t.Fatal("background work admitted after shutdown began")
	}

	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("App.Close did not return after background work completed")
	}
}

func TestBackgroundContextStopsWithParentOrApp(t *testing.T) {
	t.Run("parent", func(t *testing.T) {
		lifetimeCtx, cancelLifetime := context.WithCancel(context.Background())
		defer cancelLifetime()
		app := &App{lifetimeCtx: lifetimeCtx, lifetimeCancel: cancelLifetime}
		parent, cancelParent := context.WithCancel(context.Background())
		workCtx, cleanup := app.backgroundContext(parent)
		defer cleanup()

		cancelParent()
		select {
		case <-workCtx.Done():
		case <-time.After(time.Second):
			t.Fatal("background context outlived its parent")
		}
	})

	t.Run("app", func(t *testing.T) {
		lifetimeCtx, cancelLifetime := context.WithCancel(context.Background())
		app := &App{lifetimeCtx: lifetimeCtx, lifetimeCancel: cancelLifetime}
		workCtx, cleanup := app.backgroundContext(context.Background())
		defer cleanup()

		app.Close()
		select {
		case <-workCtx.Done():
		case <-time.After(time.Second):
			t.Fatal("background context outlived App.Close")
		}
	})
}

func TestBackgroundRetryStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() { done <- waitForBackgroundRetry(ctx, time.Hour) }()
	cancel()

	select {
	case retry := <-done:
		if retry {
			t.Fatal("cancelled retry wait asked caller to retry")
		}
	case <-time.After(time.Second):
		t.Fatal("retry wait ignored context cancellation")
	}
}

func TestImageDownloadRejectsWorkAfterAppClose(t *testing.T) {
	lifetimeCtx, cancel := context.WithCancel(context.Background())
	app := &App{lifetimeCtx: lifetimeCtx, lifetimeCancel: cancel}
	app.Close()

	if err := app.ImageDownload("", ""); !errors.Is(err, errAppClosing) {
		t.Fatalf("ImageDownload after Close error = %v, want %v", err, errAppClosing)
	}
}
