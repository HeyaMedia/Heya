package sonicanalysis

import (
	"fmt"
	"math"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// CLAP audio encoder — wraps `audio_model.onnx` from
// Xenova/clap-htsat-unfused. Inference takes a single 10 s clip's
// log-mel spectrogram (shape [1, 1, 1001, 64]) and returns a 512-dim
// audio embedding L2-normalized for cosine similarity.

const (
	clapAudioInputName  = "input_features"
	clapAudioOutputName = "audio_embeds"
	clapEmbedDim        = 512
)

type clapAudioSession struct {
	session *ort.AdvancedSession
	input   *ort.Tensor[float32]
	output  *ort.Tensor[float32]
	usedEP  string
	mu      sync.Mutex
}

func newClapAudioSession(modelPath string, accel Accelerator) (*clapAudioSession, error) {
	if err := initOnnx(); err != nil {
		return nil, fmt.Errorf("onnxruntime init: %w", err)
	}
	opts, epDesc, err := buildSessionOptions(accel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = opts.Destroy() }()

	inShape := ort.NewShape(1, 1, int64(clapNumFrames), int64(clapNumBands))
	input, err := ort.NewEmptyTensor[float32](inShape)
	if err != nil {
		return nil, fmt.Errorf("alloc CLAP input tensor: %w", err)
	}
	outShape := ort.NewShape(1, int64(clapEmbedDim))
	output, err := ort.NewEmptyTensor[float32](outShape)
	if err != nil {
		_ = input.Destroy()
		return nil, fmt.Errorf("alloc CLAP output tensor: %w", err)
	}
	sess, err := ort.NewAdvancedSession(
		modelPath,
		[]string{clapAudioInputName},
		[]string{clapAudioOutputName},
		[]ort.Value{input},
		[]ort.Value{output},
		opts,
	)
	if err != nil {
		_ = input.Destroy()
		_ = output.Destroy()
		return nil, fmt.Errorf("load CLAP audio session (ep=%s): %w", epDesc, err)
	}
	return &clapAudioSession{session: sess, input: input, output: output, usedEP: epDesc}, nil
}

func (c *clapAudioSession) Close() {
	if c.session != nil {
		_ = c.session.Destroy()
	}
	if c.input != nil {
		_ = c.input.Destroy()
	}
	if c.output != nil {
		_ = c.output.Destroy()
	}
}

// Embed copies one (clapNumFrames * clapNumBands) mel-spectrogram
// into the input tensor, runs the audio encoder, and returns a copy
// of the L2-normalized 512-dim output vector.
func (c *clapAudioSession) Embed(melSpec []float32) ([]float32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	want := clapNumFrames * clapNumBands
	if len(melSpec) != want {
		return nil, fmt.Errorf("expected %d input floats, got %d", want, len(melSpec))
	}
	copy(c.input.GetData(), melSpec)
	if err := c.session.Run(); err != nil {
		return nil, fmt.Errorf("CLAP audio session.Run: %w", err)
	}
	src := c.output.GetData()
	out := make([]float32, len(src))
	copy(out, src)
	l2Normalize(out)
	return out, nil
}

// l2Normalize divides a vector by its L2 norm in-place. Used so that
// cosine similarity reduces to a dot product downstream.
func l2Normalize(v []float32) {
	var sumSq float64
	for _, x := range v {
		sumSq += float64(x) * float64(x)
	}
	if sumSq == 0 {
		return
	}
	inv := float32(1.0 / math.Sqrt(sumSq))
	for i := range v {
		v[i] *= inv
	}
}
