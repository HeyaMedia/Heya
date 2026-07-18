package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackgroundTransitionCancelsAndSkipsSupersededWork(t *testing.T) {
	lifetime, cancelLifetime := context.WithCancel(context.Background())
	app := &App{lifetimeCtx: lifetime, lifetimeCancel: cancelLifetime}
	var transition backgroundTransition
	firstStarted := make(chan struct{})
	firstStopped := make(chan struct{})
	releaseFirst := make(chan struct{})
	latestRan := make(chan struct{})
	var middleRuns atomic.Int32

	if !transition.Start(app, func(ctx context.Context) {
		close(firstStarted)
		<-ctx.Done()
		<-releaseFirst
		close(firstStopped)
	}) {
		t.Fatal("first transition was not admitted")
	}
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first transition did not start")
	}

	if !transition.Start(app, func(context.Context) { middleRuns.Add(1) }) {
		t.Fatal("middle transition was not admitted")
	}
	if !transition.Start(app, func(context.Context) { close(latestRan) }) {
		t.Fatal("latest transition was not admitted")
	}
	close(releaseFirst)

	select {
	case <-firstStopped:
	case <-time.After(time.Second):
		t.Fatal("superseding transition did not cancel in-flight work")
	}
	select {
	case <-latestRan:
	case <-time.After(time.Second):
		t.Fatal("latest transition did not run")
	}
	if middleRuns.Load() != 0 {
		t.Fatalf("superseded queued transition ran %d times", middleRuns.Load())
	}

	app.stopBackground()
	if transition.Start(app, func(context.Context) {}) {
		t.Fatal("transition admitted after App background shutdown")
	}
}
