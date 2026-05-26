package sonicanalysis

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Holder owns an Analyzer and amortises model load/unload across many
// per-track jobs. The Analyzer's bundle is the full per-track stack —
// Discogs specialized heads (track / artist / release embeddings),
// EffNet base (genre + 1280-dim features), classifier heads (mood /
// danceability / voice), and the CLAP audio encoder. The whole bundle
// is ~10s cold-load and hundreds of MB resident, so the per-track
// sonic-analysis worker can't afford to Load/Unload per job. The
// Holder lazy-loads on first Borrow, keeps everything alive while any
// lease is outstanding, and idle-unloads the entire bundle once the
// refcount drops to zero and the timeout elapses. The CLAP *text*
// encoder lives outside this Holder, in TextSearcher, since it serves
// a different surface (text-prompt → similar-tracks search).
//
// Concurrency model: the production caller (analyze_track_facets
// worker, on the sonic_analysis queue with MaxWorkers=1) only ever
// holds one lease at a time. The refcount + idleStop machinery
// generalises if that ever changes, but the hot path stays simple.
type Holder struct {
	cfg         Config
	idleTimeout time.Duration

	mu              sync.Mutex
	analyzer        *Analyzer
	refs            int
	idleStop        chan struct{}
	loadedAt        time.Time // when the resident model finished loading; zero when not Ready
	idleUnloadAt    time.Time // when the scheduled idle-unload will fire; zero when refs > 0 or no timeout
	lastBorrowAt    time.Time // most recent Borrow() — diagnostic
	lifetimeBorrows int64     // running total of Borrow() calls
}

// Status is a read-only snapshot of the Holder's runtime state, for
// the /api/sonic/status diagnostic endpoint and the dashboard TUI.
type Status struct {
	State          AnalyzerState `json:"state"`
	Accelerator    Accelerator   `json:"accelerator"`
	Refs           int           `json:"refs"`
	LoadedAt       *time.Time    `json:"loaded_at,omitempty"`
	IdleUnloadAt   *time.Time    `json:"idle_unload_at,omitempty"`
	LastBorrowAt   *time.Time    `json:"last_borrow_at,omitempty"`
	IdleTimeoutSec int           `json:"idle_timeout_sec"`
	TotalBorrows   int64         `json:"total_borrows"`
}

// Status returns a snapshot of the Holder's state for diagnostic use.
// Safe to call concurrently with Borrow/Release.
func (h *Holder) Status() Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := Status{
		Accelerator:    h.cfg.Accelerator,
		Refs:           h.refs,
		IdleTimeoutSec: int(h.idleTimeout / time.Second),
		TotalBorrows:   h.lifetimeBorrows,
	}
	if h.analyzer == nil {
		st.State = StateUnloaded
	} else {
		st.State = h.analyzer.State()
	}
	if !h.loadedAt.IsZero() {
		t := h.loadedAt
		st.LoadedAt = &t
	}
	if !h.idleUnloadAt.IsZero() {
		t := h.idleUnloadAt
		st.IdleUnloadAt = &t
	}
	if !h.lastBorrowAt.IsZero() {
		t := h.lastBorrowAt
		st.LastBorrowAt = &t
	}
	return st
}

// NewHolder returns a Holder with no model loaded. Models open on the
// first Borrow. idleTimeout=0 disables auto-unload (useful in tests +
// short-lived CLI invocations); production passes 5 * time.Minute so
// the GPU memory comes back when the analysis batch finishes.
func NewHolder(cfg Config, idleTimeout time.Duration) *Holder {
	return &Holder{cfg: cfg, idleTimeout: idleTimeout}
}

// Lease wraps a borrowed Analyzer. Callers must Close the lease in a
// defer; leaking it pins the model in memory.
type Lease struct {
	holder   *Holder
	Analyzer *Analyzer
	closed   bool
}

// Close releases the borrow. Safe to call multiple times.
func (l *Lease) Close() {
	if l == nil || l.closed {
		return
	}
	l.closed = true
	l.holder.release()
}

// Borrow returns a Lease holding a ready Analyzer. Lazy-loads the
// model bundle on first call; reuses it on subsequent calls. Pending
// idle-unloads are cancelled. The ctx is forwarded to Analyzer.Load,
// so cancelling it aborts a cold-load mid-flight.
func (h *Holder) Borrow(ctx context.Context) (*Lease, error) {
	h.mu.Lock()
	if h.idleStop != nil {
		close(h.idleStop)
		h.idleStop = nil
	}
	h.idleUnloadAt = time.Time{}
	if h.analyzer == nil {
		h.analyzer = NewAnalyzer(h.cfg)
	}
	needLoad := h.analyzer.State() != StateReady
	h.refs++
	h.lastBorrowAt = time.Now()
	h.lifetimeBorrows++
	a := h.analyzer
	h.mu.Unlock()

	if needLoad {
		if err := a.Load(ctx); err != nil {
			h.release()
			return nil, err
		}
		h.mu.Lock()
		h.loadedAt = time.Now()
		h.mu.Unlock()
	}
	return &Lease{holder: h, Analyzer: a}, nil
}

// Reconfigure swaps the underlying config (typically the accelerator).
// Refuses with ErrHolderBusy if any lease is outstanding — the caller
// should retry on next idle. Used by the Settings UI when the user
// changes accelerator without restarting the server.
func (h *Holder) Reconfigure(cfg Config) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.refs > 0 {
		return ErrHolderBusy
	}
	if h.idleStop != nil {
		close(h.idleStop)
		h.idleStop = nil
	}
	if h.analyzer != nil && h.analyzer.State() == StateReady {
		h.analyzer.Unload()
	}
	h.analyzer = nil
	h.cfg = cfg
	return nil
}

// Close tears down the holder, unloading any resident model. The
// process shutdown sequence calls this so we don't leak ORT sessions.
func (h *Holder) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.idleStop != nil {
		close(h.idleStop)
		h.idleStop = nil
	}
	if h.analyzer != nil && h.analyzer.State() == StateReady {
		h.analyzer.Unload()
	}
}

// State returns the underlying Analyzer's state, or StateUnloaded
// when no Analyzer has been instantiated yet.
func (h *Holder) State() AnalyzerState {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.analyzer == nil {
		return StateUnloaded
	}
	return h.analyzer.State()
}

func (h *Holder) release() {
	h.mu.Lock()
	h.refs--
	if h.refs < 0 {
		h.refs = 0
	}
	if h.refs > 0 || h.idleTimeout <= 0 {
		h.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	h.idleStop = stop
	timeout := h.idleTimeout
	h.idleUnloadAt = time.Now().Add(timeout)
	h.mu.Unlock()

	go func() {
		select {
		case <-time.After(timeout):
			h.unloadIfIdle(stop)
		case <-stop:
		}
	}()
}

func (h *Holder) unloadIfIdle(myStop chan struct{}) {
	h.mu.Lock()
	if h.idleStop != myStop {
		h.mu.Unlock()
		return
	}
	h.idleStop = nil
	h.idleUnloadAt = time.Time{}
	if h.refs > 0 || h.analyzer == nil {
		h.mu.Unlock()
		return
	}
	a := h.analyzer
	h.mu.Unlock()
	if a.State() == StateReady {
		a.Unload()
	}
	h.mu.Lock()
	h.loadedAt = time.Time{}
	h.mu.Unlock()
}

// ErrHolderBusy is returned by Reconfigure when a lease is still
// outstanding.
var ErrHolderBusy = errors.New("sonicanalysis: holder is busy; reconfigure on next idle")
