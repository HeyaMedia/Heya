package metadata

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type deferredRemoteWorkKey struct{}

// WithDeferredRemoteWork tells a metadata provider that asynchronous remote
// work should be returned to the durable caller instead of polled in-process.
// Library scan workers use this so a slow discovery or resolution does not
// occupy a River worker while HeyaMetadata is still gathering data.
func WithDeferredRemoteWork(ctx context.Context, retryAfter time.Duration) context.Context {
	if retryAfter <= 0 {
		retryAfter = 30 * time.Second
	}
	return context.WithValue(ctx, deferredRemoteWorkKey{}, retryAfter)
}

// DeferredRemoteWorkDelay reports whether the caller requested durable,
// deferred handling of asynchronous metadata work.
func DeferredRemoteWorkDelay(ctx context.Context) (time.Duration, bool) {
	delay, ok := ctx.Value(deferredRemoteWorkKey{}).(time.Duration)
	return delay, ok && delay > 0
}

// DeferredWorkError carries the delay requested by an asynchronous metadata
// resource. Callers should retry the same durable operation after RetryAfter;
// the provider has already persisted the discovery or resolution identifier.
type DeferredWorkError struct {
	Operation  string
	RetryAfter time.Duration
}

func (e *DeferredWorkError) Error() string {
	if e == nil {
		return "metadata work deferred"
	}
	if e.Operation == "" {
		return fmt.Sprintf("metadata work deferred for %s", e.RetryAfter)
	}
	return fmt.Sprintf("%s deferred for %s", e.Operation, e.RetryAfter)
}

// DeferredWorkRetryAfter unwraps a deferred metadata result without tying the
// scanner or queue packages to a concrete provider implementation.
func DeferredWorkRetryAfter(err error) (time.Duration, bool) {
	var deferred *DeferredWorkError
	if !errors.As(err, &deferred) || deferred.RetryAfter <= 0 {
		return 0, false
	}
	return deferred.RetryAfter, true
}
