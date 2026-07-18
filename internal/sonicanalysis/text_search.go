package sonicanalysis

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"

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
	closed  bool
	session *clapTextSession
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

// ErrTextSearcherClosed is returned after application shutdown has made the
// searcher terminal. Close must not be followed by an implicit 500 MB model
// reload from a late request.
var ErrTextSearcherClosed = errors.New("sonicanalysis: text searcher is closed")

// Ready reports whether the encoder is already loaded.
func (t *TextSearcher) Ready() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.closed && t.session != nil
}

// Embed returns a 512-dim L2-normalized embedding for `text`. Lazy-
// loads the encoder on first call. Subsequent calls share the warm
// session.
//
// The session is used UNDER t.mu on purpose: Close/Reconfigure destroy
// the native ONNX session, and an embed running on a snapshot taken
// outside the lock would be a use-after-free (segfault, not an error).
// Serializing embeds is fine — a text embed is milliseconds and comes
// from interactive searches.
func (t *TextSearcher) Embed(text string) ([]float32, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrTextSearcherClosed
	}
	if err := t.ensureLoadedLocked(); err != nil {
		return nil, err
	}
	return t.session.Embed(text)
}

// Reconfigure closes any loaded session and swaps the config, so the
// next Embed lazy-loads with the new settings (models dir/accelerator).
// It takes the same mutex as Embed/ensureLoaded, so an in-flight embed
// or load finishes before the old session is destroyed — reconfigure
// in place instead of swapping the *TextSearcher pointer, which would
// race with concurrent readers of the pointer.
func (t *TextSearcher) Reconfigure(cfg Config) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	if t.session != nil {
		t.session.Close()
		t.session = nil
	}
	t.cfg = cfg.normalize()
}

// Close releases the lazily loaded native text-encoder session. It takes the
// same lock as Embed and Reconfigure, so shutdown cannot destroy ONNX state
// beneath an in-flight native call. Close is idempotent.
func (t *TextSearcher) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.closed = true
	if t.session != nil {
		t.session.Close()
		t.session = nil
	}
}

// ensureLoadedLocked loads the encoder once. The caller holds t.mu for the
// complete load and embed operation, which both coalesces a cold-load burst
// and prevents Close/Reconfigure from destroying native state in use.
func (t *TextSearcher) ensureLoadedLocked() error {
	if t.closed {
		return ErrTextSearcherClosed
	}
	if t.session != nil {
		return nil
	}

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
