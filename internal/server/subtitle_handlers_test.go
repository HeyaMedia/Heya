package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEnsureCachedSubtitleCoalescesConcurrentExtraction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subtitle.vtt")
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	var calls atomic.Int32
	extract := func(context.Context) error {
		calls.Add(1)
		startOnce.Do(func() { close(started) })
		<-release
		return os.WriteFile(path, []byte("WEBVTT\n"), 0o600)
	}

	const waiters = 16
	results := make(chan error, waiters)
	for range waiters {
		go func() { results <- ensureCachedSubtitle(context.Background(), context.Background(), path, extract) }()
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("subtitle extraction did not start")
	}
	close(release)
	for range waiters {
		if err := <-results; err != nil {
			t.Fatalf("ensure cached subtitle: %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("extractor calls = %d, want 1", got)
	}
}

func TestEnsureCachedSubtitleWaiterCanCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subtitle.ass")
	started := make(chan struct{})
	release := make(chan struct{})
	leaderDone := make(chan error, 1)
	go func() {
		leaderDone <- ensureCachedSubtitle(context.Background(), context.Background(), path, func(context.Context) error {
			close(started)
			<-release
			return os.WriteFile(path, []byte("[Script Info]\n"), 0o600)
		})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("leader extraction did not start")
	}

	waitCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ensureCachedSubtitle(waitCtx, context.Background(), path, func(context.Context) error {
		t.Fatal("a joined waiter must not start a second extractor")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter error = %v, want context.Canceled", err)
	}

	close(release)
	select {
	case err := <-leaderDone:
		if err != nil {
			t.Fatalf("leader extraction: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("leader did not complete after cancelled waiter left")
	}
}

func TestEnsureCachedSubtitleLeaderCanCancelWithoutCancellingFollower(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subtitle.vtt")
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	var startOnce sync.Once
	extract := func(ctx context.Context) error {
		calls.Add(1)
		startOnce.Do(func() { close(started) })
		select {
		case <-release:
			return os.WriteFile(path, []byte("WEBVTT\n"), 0o600)
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	workCtx, cancelWork := context.WithCancel(context.Background())
	t.Cleanup(cancelWork)
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() { leaderDone <- ensureCachedSubtitle(leaderCtx, workCtx, path, extract) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("leader extraction did not start")
	}

	followerDone := make(chan error, 1)
	go func() { followerDone <- ensureCachedSubtitle(context.Background(), workCtx, path, extract) }()
	cancelLeader()
	select {
	case err := <-leaderDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("leader error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled leader did not return")
	}

	close(release)
	select {
	case err := <-followerDone:
		if err != nil {
			t.Fatalf("follower extraction: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("follower did not receive shared extraction result")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("extractor calls = %d, want 1", got)
	}
}

func TestCachedSubtitleReadyRejectsNonRegularTarget(t *testing.T) {
	ready, err := cachedSubtitleReady(t.TempDir())
	if err == nil || ready {
		t.Fatalf("ready = %v, err = %v; want a non-regular-target error", ready, err)
	}
}
