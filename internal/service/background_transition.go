package service

import (
	"context"
	"sync"
	"sync/atomic"
)

// backgroundTransition serializes a live subsystem's asynchronous state
// changes while allowing HTTP handlers to return immediately. Starting a new
// transition cancels the in-flight one; queued superseded generations skip
// their work after reaching the apply lock. The zero value is ready to use.
type backgroundTransition struct {
	generation atomic.Uint64
	applyMu    sync.Mutex
	cancelMu   sync.Mutex
	cancel     context.CancelFunc
}

func (t *backgroundTransition) Start(app *App, work func(context.Context)) bool {
	if app == nil || work == nil {
		return false
	}
	generation := t.generation.Add(1)
	parent := app.LifetimeContext()
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)

	// Publish the new cancellation source before admission. A newer transition
	// can now interrupt this one even while it is waiting for applyMu.
	t.cancelMu.Lock()
	if t.cancel != nil {
		t.cancel()
	}
	t.cancel = cancel
	t.cancelMu.Unlock()

	if !app.startBackground(func() {
		defer t.finish(generation, cancel)
		t.applyMu.Lock()
		defer t.applyMu.Unlock()
		if ctx.Err() != nil || generation != t.generation.Load() {
			return
		}
		work(ctx)
	}) {
		t.finish(generation, cancel)
		return false
	}
	return true
}

func (t *backgroundTransition) finish(generation uint64, cancel context.CancelFunc) {
	cancel()
	t.cancelMu.Lock()
	if generation == t.generation.Load() {
		t.cancel = nil
	}
	t.cancelMu.Unlock()
}
