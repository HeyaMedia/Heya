package service

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// Cadence of the background reachability probe. Down is re-checked often, so a
// recovery lands while the operator is still looking at the problem; up is
// re-checked rarely, because that probe's only job is to notice the provider
// leaving.
const (
	metadataProbeIntervalDown = 30 * time.Second
	metadataProbeIntervalUp   = 5 * time.Minute
	metadataProbeTimeout      = 10 * time.Second
)

// MetadataStatus is the last known reachability of the canonical metadata
// provider. The library, playback and transcoding are entirely local and keep
// working without it; enrichment and the matching of newly scanned files are
// what degrade.
//
// A zero CheckedAt means the provider was never probed — one-shot commands and
// passive runs skip it.
type MetadataStatus struct {
	Available bool
	LastError string
	CheckedAt time.Time
}

// MetadataStatus reports the cached reachability of the canonical metadata
// provider. Callers get the last probe's answer rather than issuing their own:
// this is read by the readiness endpoint, which must stay fast.
func (a *App) MetadataStatus() MetadataStatus {
	a.metadataMu.RLock()
	defer a.metadataMu.RUnlock()
	return a.metadataStatus
}

func (a *App) setMetadataStatus(status MetadataStatus) {
	a.metadataMu.Lock()
	defer a.metadataMu.Unlock()
	a.metadataStatus = status
}

// newMetadataStatus records the outcome of one probe.
func newMetadataStatus(err error) MetadataStatus {
	status := MetadataStatus{Available: err == nil, CheckedAt: time.Now()}
	if err != nil {
		status.LastError = err.Error()
	}
	return status
}

// watchMetadataAvailability keeps MetadataStatus current for as long as the
// process lives.
//
// Startup deliberately does not depend on the provider answering — see newApp —
// so this is what carries the signal the old hard failure used to: the
// readiness endpoint reads the status, and a provider leaving or returning is
// logged the moment it happens.
func (a *App) watchMetadataAvailability(ctx context.Context, probe func(context.Context) error) {
	go func() {
		for {
			interval := metadataProbeIntervalUp
			if !a.MetadataStatus().Available {
				interval = metadataProbeIntervalDown
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
			if !a.probeMetadataOnce(ctx, probe) {
				return
			}
		}
	}()
}

// probeMetadataOnce runs a single reachability probe, records it, and logs the
// crossings in either direction. Returns false when the lifetime ended, which
// is the watcher's cue to stop.
func (a *App) probeMetadataOnce(ctx context.Context, probe func(context.Context) error) bool {
	probeCtx, cancel := context.WithTimeout(ctx, metadataProbeTimeout)
	err := probe(probeCtx)
	cancel()
	// A cancelled lifetime fails the probe on its way out. That is shutdown,
	// not the provider going away, and must not be recorded as an outage.
	if ctx.Err() != nil {
		return false
	}

	was := a.MetadataStatus().Available
	a.setMetadataStatus(newMetadataStatus(err))
	switch {
	case err == nil && !was:
		log.Info().Msg("canonical metadata provider is reachable again")
	case err != nil && was:
		log.Warn().Err(err).Msg("canonical metadata provider became unreachable; enrichment and matching are degraded")
	}
	return true
}
