package sonicanalysis

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog/log"
)

// TextSearcher owns the CLAP text encoder. Lives independently of
// Analyzer because text search is on the read path (any-time queries
// from the UI), while Analyzer is only loaded during scheduled
// analysis windows. Loaded lazily on first Embed call; stays warm
// afterwards.
//
// Not destroyed at end-of-window — the text encoder is ~500 MB and
// staying warm avoids the 5-6 s reload cost per search burst.
// Server shutdown unloads via Close().
type TextSearcher struct {
	cfg     Config
	mu      sync.Mutex
	session *clapTextSession
	loading atomic.Bool
}

// NewTextSearcher creates a TextSearcher; the session itself isn't
// loaded until the first Embed call.
func NewTextSearcher(cfg Config) *TextSearcher {
	return &TextSearcher{cfg: cfg.normalize()}
}

// ErrTextSearcherUnavailable is returned by Embed when the underlying
// model files don't exist on disk (e.g. ModelFetcher hasn't finished
// downloading yet).
var ErrTextSearcherUnavailable = errors.New("sonicanalysis: text search model not loaded")

// Ready reports whether the encoder is already loaded.
func (t *TextSearcher) Ready() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.session != nil
}

// Embed returns a 512-dim L2-normalized embedding for `text`. Lazy-
// loads the encoder on first call. Subsequent calls share the warm
// session.
func (t *TextSearcher) Embed(text string) ([]float32, error) {
	if err := t.ensureLoaded(); err != nil {
		return nil, err
	}
	t.mu.Lock()
	sess := t.session
	t.mu.Unlock()
	if sess == nil {
		return nil, ErrTextSearcherUnavailable
	}
	return sess.Embed(text)
}

// ensureLoaded loads the encoder once; subsequent callers wait on
// the mutex and see a populated session. Bails early if another
// goroutine is mid-load.
func (t *TextSearcher) ensureLoaded() error {
	t.mu.Lock()
	if t.session != nil {
		t.mu.Unlock()
		return nil
	}
	if !t.loading.CompareAndSwap(false, true) {
		// Another goroutine is mid-load. Release the lock so it can
		// finish; the next call will succeed.
		t.mu.Unlock()
		return errors.New("sonicanalysis: text searcher load already in progress")
	}
	defer t.loading.Store(false)
	defer t.mu.Unlock()

	log.Info().Msg("sonicanalysis: lazy-loading CLAP text encoder")
	modelPath := filepath.Join(t.cfg.ModelsDir, "clap", "text_model.onnx")
	tokenizerPath := filepath.Join(t.cfg.ModelsDir, "clap", "tokenizer.json")
	sess, err := newClapTextSession(modelPath, tokenizerPath, t.cfg.Accelerator)
	if err != nil {
		return fmt.Errorf("clap text load: %w", err)
	}
	t.session = sess
	log.Info().Msg("sonicanalysis: CLAP text encoder ready")
	return nil
}

// Close destroys the underlying session. Safe to call multiple times.
func (t *TextSearcher) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.session != nil {
		t.session.Close()
		t.session = nil
	}
}
