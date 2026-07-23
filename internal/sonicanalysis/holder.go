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
// Concurrent callers share the same model bundle. One borrower performs a
// cold load while later borrowers wait on loadDone; once ready, the Analyzer's
// bounded CPU/GPU lanes coordinate the per-track pipeline.
type Holder struct {
	cfg         Config
	idleTimeout time.Duration

	mu              sync.Mutex
	closed          bool
	pendingCfg      *Config // Reconfigure arrived while leased; applied when refs drop to 0
	analyzer        *Analyzer
	loadDone        chan struct{}
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
	State           AnalyzerState `json:"state"`
	Accelerator     Accelerator   `json:"accelerator"`
	Refs            int           `json:"refs"`
	LoadedAt        *time.Time    `json:"loaded_at,omitempty"`
	IdleUnloadAt    *time.Time    `json:"idle_unload_at,omitempty"`
	LastBorrowAt    *time.Time    `json:"last_borrow_at,omitempty"`
	IdleTimeoutSec  int           `json:"idle_timeout_sec"`
	TotalBorrows    int64         `json:"total_borrows"`
	PreprocessAhead int           `json:"preprocess_ahead"`
	GPUWorkers      int           `json:"gpu_workers"`
	PipelineWorkers int           `json:"pipeline_workers"`
	// PendingAccelerator is set while a mid-batch Reconfigure waits for the
	// current leases to drain; Accelerator still shows what's running now.
	PendingAccelerator *Accelerator `json:"pending_accelerator,omitempty"`
}

// Status returns a snapshot of the Holder's state for diagnostic use.
// Safe to call concurrently with Borrow/Release.
func (h *Holder) Status() Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := Status{
		Accelerator:     h.cfg.Accelerator,
		Refs:            h.refs,
		IdleTimeoutSec:  int(h.idleTimeout / time.Second),
		TotalBorrows:    h.lifetimeBorrows,
		PreprocessAhead: h.cfg.PreprocessAhead,
		GPUWorkers:      h.cfg.GPUWorkers,
		PipelineWorkers: h.cfg.PreprocessAhead + h.cfg.GPUWorkers,
	}
	if h.pendingCfg != nil {
		acc := h.pendingCfg.Accelerator
		st.PendingAccelerator = &acc
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
	return &Holder{cfg: cfg.normalize(), idleTimeout: idleTimeout}
}

// PipelineWorkers is the River queue width required to feed this holder's
// configured preprocessing and GPU lanes.
func (h *Holder) PipelineWorkers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.PreprocessAhead + h.cfg.GPUWorkers
}

// Lease wraps a borrowed Analyzer. Callers must Close the lease in a
// defer; leaking it pins the model in memory.
type Lease struct {
	holder   *Holder
	Analyzer *Analyzer
	once     sync.Once
}

// Close releases the borrow. Safe to call multiple times.
func (l *Lease) Close() {
	if l == nil {
		return
	}
	l.once.Do(l.holder.release)
}

// Borrow returns a Lease holding a ready Analyzer. Lazy-loads the
// model bundle on first call; reuses it on subsequent calls. Pending
// idle-unloads are cancelled. The ctx is forwarded to Analyzer.Load,
// so cancelling it aborts a cold-load mid-flight.
func (h *Holder) Borrow(ctx context.Context) (*Lease, error) {
	for {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return nil, ErrHolderClosed
		}
		if h.idleStop != nil {
			close(h.idleStop)
			h.idleStop = nil
		}
		h.idleUnloadAt = time.Time{}
		if h.analyzer == nil {
			h.analyzer = NewAnalyzer(h.cfg)
		}
		a := h.analyzer

		switch a.State() {
		case StateReady:
			h.recordBorrowLocked()
			h.mu.Unlock()
			return &Lease{holder: h, Analyzer: a}, nil

		case StateUnloaded:
			if done := h.loadDone; done != nil {
				h.mu.Unlock()
				select {
				case <-done:
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			// Reserve one lease before dropping the lock so Close/Reconfigure
			// cannot destroy or replace the analyzer during its cold load.
			done := make(chan struct{})
			h.loadDone = done
			h.recordBorrowLocked()
			h.mu.Unlock()

			err := a.Load(ctx)
			h.mu.Lock()
			if err == nil {
				h.loadedAt = time.Now()
			}
			if h.loadDone == done {
				close(done)
				h.loadDone = nil
			}
			h.mu.Unlock()
			if err != nil {
				h.release()
				return nil, err
			}
			return &Lease{holder: h, Analyzer: a}, nil

		case StateLoading:
			done := h.loadDone
			h.mu.Unlock()
			if done == nil {
				return nil, errors.New("sonicanalysis: analyzer loading without holder coordination")
			}
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}

		case StateUnloading:
			h.mu.Unlock()
			return nil, errors.New("sonicanalysis: analyzer is unloading")
		}
	}
}

func (h *Holder) recordBorrowLocked() {
	h.refs++
	h.lastBorrowAt = time.Now()
	h.lifetimeBorrows++
}

// Reconfigure swaps the underlying config (typically the accelerator).
// If a lease is outstanding the new config is stashed and applied
// automatically when the last lease is released (see release), and
// ErrHolderBusy is returned so the caller can report "saved, applies
// when the current batch finishes" — it does NOT mean the change was
// dropped. Used by the Settings UI when the user changes accelerator
// without restarting the server.
func (h *Holder) Reconfigure(cfg Config) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrHolderClosed
	}
	if h.refs > 0 {
		c := cfg
		h.pendingCfg = &c
		return ErrHolderBusy
	}
	h.pendingCfg = nil
	h.applyConfigLocked(cfg)
	return nil
}

// applyConfigLocked tears down the current analyzer (cancelling any
// idle-unload timer) and installs cfg for the next Borrow. Caller must
// hold h.mu and have ensured refs == 0.
func (h *Holder) applyConfigLocked(cfg Config) {
	cfg = cfg.normalize()
	if h.idleStop != nil {
		close(h.idleStop)
		h.idleStop = nil
	}
	h.idleUnloadAt = time.Time{}
	if h.analyzer != nil && h.analyzer.State() == StateReady {
		h.analyzer.Unload()
	}
	h.analyzer = nil
	h.loadedAt = time.Time{}
	h.cfg = cfg
}

// Close prevents new borrows and tears down the holder. If an analysis is
// still using the native model sessions, their unload is deferred until the
// last lease is released. This matters during bounded shutdown: native
// inference is not context-interruptible, so the worker may outlive its stop
// timeout and must be allowed to finish without its sessions disappearing.
func (h *Holder) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	h.pendingCfg = nil
	if h.idleStop != nil {
		close(h.idleStop)
		h.idleStop = nil
	}
	h.idleUnloadAt = time.Time{}
	if h.refs > 0 {
		h.mu.Unlock()
		return
	}
	a := h.detachAnalyzerLocked()
	h.mu.Unlock()
	unloadAnalyzer(a)
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
	if h.refs == 0 && h.closed {
		a := h.detachAnalyzerLocked()
		h.mu.Unlock()
		unloadAnalyzer(a)
		return
	}
	// A Reconfigure that arrived mid-batch was stashed; this refs→0
	// transition is the "next idle" it was waiting for. Apply it now —
	// the analyzer is torn down, so the next Borrow lazy-loads with the
	// new config. No idle-unload timer needed after this.
	if h.refs == 0 && h.pendingCfg != nil {
		cfg := *h.pendingCfg
		h.pendingCfg = nil
		h.applyConfigLocked(cfg)
		h.mu.Unlock()
		return
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
	a := h.detachAnalyzerLocked()
	h.mu.Unlock()
	unloadAnalyzer(a)
}

// detachAnalyzerLocked transfers ownership of the current analyzer to the
// caller so a potentially expensive native unload can happen without h.mu.
func (h *Holder) detachAnalyzerLocked() *Analyzer {
	a := h.analyzer
	h.analyzer = nil
	h.loadedAt = time.Time{}
	h.idleUnloadAt = time.Time{}
	return a
}

func unloadAnalyzer(a *Analyzer) {
	if a != nil && a.State() == StateReady {
		a.Unload()
	}
}

// ErrHolderBusy is returned by Reconfigure when a lease is still
// outstanding. The config is NOT lost — it's stashed and applied
// automatically when the current leases drain.
var ErrHolderBusy = errors.New("sonicanalysis: holder is busy; settings will apply when the current batch finishes")

// ErrHolderClosed is returned when work is submitted after shutdown began.
var ErrHolderClosed = errors.New("sonicanalysis: holder is closed")
