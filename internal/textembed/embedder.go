// Package textembed wraps a BGE-large-en text-embedding model (ONNX) for the
// optional ML recommendation engine: text → 1024-dim L2-normalized vector. It
// reuses the shared ONNX Runtime environment + accelerator logic from
// internal/sonicanalysis so the two ML subsystems don't fight over ORT init.
package textembed

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"

	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
	ort "github.com/yalue/onnxruntime_go"
)

const (
	// Dim is BGE-large-en-v1.5's hidden size (the embedding dimension).
	Dim = 1024
	// maxLen caps tokens per doc — metadata docs are short; keeps inference fast.
	maxLen = 384

	// Version stamps every stored embedding. Bump it whenever the model or the
	// composed doc changes so the backfill re-embeds the whole catalog.
	Version = 1

	ModelFile     = "model_quantized.onnx"
	TokenizerFile = "tokenizer.json"
)

// Embedder wraps BGE-large-en (ONNX) + its WordPiece tokenizer. Always-warm and
// mutex-serialized (native ORT sessions are not concurrency-safe). Mirrors
// sonicanalysis.TextSearcher; CLS-pooled + L2-normalized so cosine == dot.
type Embedder struct {
	mu      sync.Mutex
	tk      *tokenizer.Tokenizer
	session *ort.DynamicAdvancedSession
	usedEP  string
}

// New loads the model + tokenizer from modelsDir with the given accelerator.
func New(modelsDir string, accel sonicanalysis.Accelerator) (*Embedder, error) {
	if err := sonicanalysis.EnsureONNX(); err != nil {
		return nil, fmt.Errorf("onnx init: %w", err)
	}
	opts, ep, err := sonicanalysis.BuildSessionOptions(accel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = opts.Destroy() }()

	tk, err := pretrained.FromFile(filepath.Join(modelsDir, TokenizerFile))
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}
	sess, err := ort.NewDynamicAdvancedSession(
		filepath.Join(modelsDir, ModelFile),
		[]string{"input_ids", "attention_mask", "token_type_ids"},
		[]string{"last_hidden_state"},
		opts,
	)
	if err != nil {
		return nil, fmt.Errorf("load BGE session (ep=%s): %w", ep, err)
	}
	return &Embedder{tk: tk, session: sess, usedEP: ep}, nil
}

func (e *Embedder) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.session != nil {
		_ = e.session.Destroy()
		e.session = nil
	}
}

// UsedEP reports which execution provider actually attached (cpu/coreml/…).
func (e *Embedder) UsedEP() string { return e.usedEP }

// Embed returns the L2-normalized 1024-d embedding of text (BGE CLS pooling).
func (e *Embedder) Embed(text string) (vec []float32, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.session == nil {
		return nil, fmt.Errorf("embedder closed")
	}
	// sugarme/tokenizer v0.3 panics on some multibyte input; sanitize to ASCII
	// (BGE-en can't use non-Latin text meaningfully anyway) and guard.
	defer func() {
		if r := recover(); r != nil {
			vec, err = nil, fmt.Errorf("tokenizer panic: %v", r)
		}
	}()

	enc, err := e.tk.EncodeSingle(sanitize(text), true)
	if err != nil {
		return nil, err
	}
	ids := enc.Ids
	if len(ids) > maxLen {
		ids = ids[:maxLen]
	}
	n := len(ids)
	if n == 0 {
		return nil, fmt.Errorf("zero tokens")
	}
	iid := make([]int64, n)
	am := make([]int64, n)
	tt := make([]int64, n) // single sentence → mask all 1, type all 0
	for i, id := range ids {
		iid[i], am[i], tt[i] = int64(id), 1, 0
	}

	shape := ort.NewShape(1, int64(n))
	ti, err := ort.NewTensor(shape, iid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = ti.Destroy() }()
	ta, err := ort.NewTensor(shape, am)
	if err != nil {
		return nil, err
	}
	defer func() { _ = ta.Destroy() }()
	tt2, err := ort.NewTensor(shape, tt)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tt2.Destroy() }()
	out, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(n), Dim))
	if err != nil {
		return nil, err
	}
	defer func() { _ = out.Destroy() }()

	if err := e.session.Run([]ort.Value{ti, ta, tt2}, []ort.Value{out}); err != nil {
		return nil, err
	}
	cls := make([]float32, Dim)
	copy(cls, out.GetData()[:Dim]) // [CLS] token embedding
	l2norm(cls)
	return cls, nil
}

// sanitize strips to printable ASCII — sidesteps a multibyte offset panic in
// sugarme/tokenizer v0.3.0; harmless for an English embedder.
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			b.WriteByte(' ')
		case r >= 32 && r < 127:
			b.WriteRune(r)
		case r >= 127:
			b.WriteByte(' ')
		}
	}
	return b.String()
}

func l2norm(v []float32) {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	if s == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(s))
	for i := range v {
		v[i] *= inv
	}
}
