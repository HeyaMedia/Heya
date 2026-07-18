package cmd

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRiverStartContextOutlivesSignalContext(t *testing.T) {
	type contextKey struct{}
	parent, cancelParent := context.WithTimeout(
		context.WithValue(context.Background(), contextKey{}, "preserved"),
		time.Minute,
	)
	riverCtx, cancelRiver := newRiverStartContext(parent)

	cancelParent()
	select {
	case <-riverCtx.Done():
		t.Fatal("signal context cancellation unexpectedly cancelled River start context")
	default:
	}
	if _, hasDeadline := riverCtx.Deadline(); hasDeadline {
		t.Fatal("River start context retained the signal context deadline")
	}
	if got := riverCtx.Value(contextKey{}); got != "preserved" {
		t.Fatalf("River start context value = %v, want preserved", got)
	}

	cancelRiver()
	select {
	case <-riverCtx.Done():
	default:
		t.Fatal("explicit River cancellation did not cancel start context")
	}
}

func TestWaitForWorkerShutdownLeaseLossCancelsRuntimeAndRiver(t *testing.T) {
	signalCtx := context.Background()
	workerCtx, cancelWorker := context.WithCancel(signalCtx)
	defer cancelWorker()
	riverCtx, cancelRiver := context.WithCancel(context.Background())
	defer cancelRiver()

	wantErr := errors.New("database session ended")
	leaseLost := make(chan error, 1)
	leaseFailure := watchWorkerLease(workerCtx, leaseLost, cancelWorker, cancelRiver)
	leaseLost <- wantErr

	err := waitForWorkerShutdown(signalCtx, leaseFailure)
	if !errors.Is(err, wantErr) {
		t.Fatalf("shutdown error = %v, want wrapped lease error", err)
	}
	select {
	case <-workerCtx.Done():
	default:
		t.Fatal("lease loss did not cancel worker runtime context")
	}
	select {
	case <-riverCtx.Done():
	default:
		t.Fatal("lease loss did not hard-cancel River start context")
	}
}

func TestWaitForWorkerShutdownSignalKeepsRiverGraceful(t *testing.T) {
	signalCtx, cancelSignal := context.WithCancel(context.Background())
	workerCtx, cancelWorker := context.WithCancel(signalCtx)
	defer cancelWorker()
	riverCtx, cancelRiver := context.WithCancel(context.Background())
	defer cancelRiver()
	leaseFailure := watchWorkerLease(workerCtx, nil, cancelWorker, cancelRiver)

	cancelSignal()
	if err := waitForWorkerShutdown(signalCtx, leaseFailure); err != nil {
		t.Fatalf("signal shutdown error = %v, want nil", err)
	}
	select {
	case <-workerCtx.Done():
	default:
		t.Fatal("signal did not cancel derived worker runtime context")
	}
	select {
	case <-riverCtx.Done():
		t.Fatal("ordinary signal hard-cancelled River before graceful Stop")
	default:
	}
}
