// Package textembed wraps the multilingual BGE-M3 text-embedding model (ONNX) for the
// optional ML recommendation engine: text → 1024-dim L2-normalized vector. It
// reuses the shared ONNX Runtime environment + accelerator logic from
// internal/sonicanalysis so the two ML subsystems don't fight over ORT init.
package textembed

import (
	"fmt"
	"math"
	"path/filepath"
	"sync"

	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
	ort "github.com/yalue/onnxruntime_go"
)

const (
	// Dim is BGE-M3's dense embedding dimension.
	Dim = 1024
	// BGE-M3 supports much longer documents, but Heya's deliberately compact
	// metadata documents do not need them. Keeping the cap modest makes the
	// background catalog sweep predictable on CPU-only servers.
	maxLen = 512

	// Version stamps every stored embedding. Bump it whenever the model or the
	// composed doc changes so the backfill re-embeds the whole catalog.
	Version = 2

	// Version the on-disk paths as well as Version. Otherwise an upgrade can see
	// the old files as "present" before the fetcher's verification pass and try
	// to open an incompatible graph.
	ModelFile = "bge-m3/model_quantized.onnx"
	// TokenizerFile is a local model asset path, not a credential.
	TokenizerFile = "bge-m3/tokenizer.json" //nolint:gosec
)

// Embedder wraps BGE-M3 (ONNX) + its XLM-R tokenizer. Always-warm and
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
	// ORT's OpenVINO provider defaults GPU inference to FP16. BGE-family
	// transformer activations overflow at that precision on Intel Arc, yielding
	// non-finite embeddings. Keep the GPU provider, but compile this model in
	// FP32; sonic-analysis models continue using their existing defaults.
	opts, ep, err := sonicanalysis.BuildSessionOptionsWithOpenVINOPrecision(accel, "FP32")
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
		[]string{"input_ids", "attention_mask"},
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
	// Keep the panic guard around the third-party tokenizer, but pass Unicode
	// through untouched: multilingual retrieval is the reason BGE-M3 replaced
	// the former English-only model.
	defer func() {
		if r := recover(); r != nil {
			vec, err = nil, fmt.Errorf("tokenizer panic: %v", r)
		}
	}()

	enc, err := e.tk.EncodeSingle(text, true)
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
	// XLM-R/BGE-M3 has no token_type_ids input.
	for i, id := range ids {
		iid[i], am[i] = int64(id), 1
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
	out, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(n), Dim))
	if err != nil {
		return nil, err
	}
	defer func() { _ = out.Destroy() }()

	if err := e.session.Run([]ort.Value{ti, ta}, []ort.Value{out}); err != nil {
		return nil, err
	}
	cls := make([]float32, Dim)
	copy(cls, out.GetData()[:Dim]) // [CLS] token embedding
	if err := l2norm(cls); err != nil {
		return nil, err
	}
	return cls, nil
}

func l2norm(v []float32) error {
	var s float64
	for i, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return fmt.Errorf("non-finite model output at dimension %d", i)
		}
		s += float64(x) * float64(x)
	}
	if s == 0 {
		return fmt.Errorf("zero-norm model output")
	}
	inv := float32(1 / math.Sqrt(s))
	for i := range v {
		v[i] *= inv
	}
	return nil
}
