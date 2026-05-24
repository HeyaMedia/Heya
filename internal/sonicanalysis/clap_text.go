package sonicanalysis

import (
	"fmt"

	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
	ort "github.com/yalue/onnxruntime_go"
)

// CLAP text encoder — wraps `text_model.onnx` (RoBERTa-base projected
// to 512-dim) from Xenova/clap-htsat-unfused, plus the matching
// tokenizer.json (RoBERTa byte-level BPE).
//
// Text embeddings live in the SAME 512-dim space as audio embeddings,
// so cosine(text_embed, audio_embed) is a direct "does this audio
// match this prompt" score.

const (
	clapTextInputName  = "input_ids"
	clapTextOutputName = "text_embeds"
	clapTextMaxLen     = 77 // CLIP-style cap CLAP was trained with
)

type clapTextSession struct {
	tk      *tokenizer.Tokenizer
	session *ort.DynamicAdvancedSession
	usedEP  string
}

func newClapTextSession(modelPath, tokenizerPath string, accel Accelerator) (*clapTextSession, error) {
	if err := initOnnx(); err != nil {
		return nil, fmt.Errorf("onnxruntime init: %w", err)
	}
	opts, epDesc, err := buildSessionOptions(accel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = opts.Destroy() }()

	tk, err := pretrained.FromFile(tokenizerPath)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer %s: %w", tokenizerPath, err)
	}

	sess, err := ort.NewDynamicAdvancedSession(
		modelPath,
		[]string{clapTextInputName},
		[]string{clapTextOutputName},
		opts,
	)
	if err != nil {
		return nil, fmt.Errorf("load CLAP text session (ep=%s): %w", epDesc, err)
	}
	return &clapTextSession{tk: tk, session: sess, usedEP: epDesc}, nil
}

func (t *clapTextSession) Close() {
	if t.session != nil {
		_ = t.session.Destroy()
	}
}

// Embed tokenizes `text` (BOS/EOS added by the tokenizer config),
// runs the text encoder, and returns the L2-normalized 512-dim
// embedding. Capped at clapTextMaxLen tokens.
func (t *clapTextSession) Embed(text string) ([]float32, error) {
	enc, err := t.tk.EncodeSingle(text, true)
	if err != nil {
		return nil, fmt.Errorf("tokenize: %w", err)
	}
	ids := enc.Ids
	if len(ids) > clapTextMaxLen {
		ids = ids[:clapTextMaxLen]
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("tokenizer returned zero tokens")
	}

	inputIds := make([]int64, len(ids))
	for i, id := range ids {
		inputIds[i] = int64(id)
	}

	inputTensor, err := ort.NewTensor(ort.NewShape(1, int64(len(inputIds))), inputIds)
	if err != nil {
		return nil, fmt.Errorf("alloc input tensor: %w", err)
	}
	defer func() { _ = inputTensor.Destroy() }()

	outputTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(clapEmbedDim)))
	if err != nil {
		return nil, fmt.Errorf("alloc output tensor: %w", err)
	}
	defer func() { _ = outputTensor.Destroy() }()

	if err := t.session.Run(
		[]ort.Value{inputTensor},
		[]ort.Value{outputTensor},
	); err != nil {
		return nil, fmt.Errorf("text session.Run: %w", err)
	}

	src := outputTensor.GetData()
	out := make([]float32, len(src))
	copy(out, src)
	l2Normalize(out)
	return out, nil
}
