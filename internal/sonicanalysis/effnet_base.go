package sonicanalysis

import (
	"fmt"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// EffNet base model — wraps `discogs-effnet-bsdynamic-1.onnx`.
//
// Same mel-spec input as our specialized `discogs_track_embeddings`
// model (batch, 128, 96), but exposes BOTH outputs from one forward
// pass:
//   - activations: (batch, 400)  — Discogs-400 genre softmax
//   - embeddings : (batch, 1280) — raw EffNet penultimate vector
//
// The 1280-dim embeddings feed into the small classifier heads
// (mood/danceability/voice_instrumental). Dynamic batch lets us
// process every patch in one inference call without padding.

const (
	effnetInputName   = "melspectrogram"
	effnetGenreOutput = "activations"
	effnetEmbedOutput = "embeddings"
	effnetGenreDim    = 400
	effnetEmbedDim    = 1280
)

type effnetBaseSession struct {
	session *ort.DynamicAdvancedSession
	usedEP  string
	mu      sync.Mutex
}

func newEffnetBaseSession(modelPath string, accel Accelerator) (*effnetBaseSession, error) {
	if err := initOnnx(); err != nil {
		return nil, fmt.Errorf("onnxruntime init: %w", err)
	}
	opts, epDesc, err := buildSessionOptions(accel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = opts.Destroy() }()

	sess, err := ort.NewDynamicAdvancedSession(
		modelPath,
		[]string{effnetInputName},
		[]string{effnetGenreOutput, effnetEmbedOutput},
		opts,
	)
	if err != nil {
		return nil, fmt.Errorf("load effnet base session (ep=%s): %w", epDesc, err)
	}
	return &effnetBaseSession{session: sess, usedEP: epDesc}, nil
}

func (e *effnetBaseSession) Close() {
	if e.session != nil {
		_ = e.session.Destroy()
	}
}

// Run feeds the full patches tensor (nPatches × 128 × 96 floats) in
// one go (dynamic batch), returns flat (nPatches, 400) genre probs
// and (nPatches, 1280) embeddings.
func (e *effnetBaseSession) Run(patches []float32, nPatches int) (genre, embed []float32, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	wantLen := nPatches * discogsTimeFrames * discogsMelBands
	if len(patches) != wantLen {
		return nil, nil, fmt.Errorf("expected %d input floats, got %d", wantLen, len(patches))
	}

	inputTensor, err := ort.NewTensor(
		ort.NewShape(int64(nPatches), discogsTimeFrames, discogsMelBands),
		patches,
	)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = inputTensor.Destroy() }()

	genreTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(int64(nPatches), effnetGenreDim))
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = genreTensor.Destroy() }()

	embedTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(int64(nPatches), effnetEmbedDim))
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = embedTensor.Destroy() }()

	if err := e.session.Run(
		[]ort.Value{inputTensor},
		[]ort.Value{genreTensor, embedTensor},
	); err != nil {
		return nil, nil, fmt.Errorf("effnet base run: %w", err)
	}

	gSrc := genreTensor.GetData()
	eSrc := embedTensor.GetData()
	genre = make([]float32, len(gSrc))
	embed = make([]float32, len(eSrc))
	copy(genre, gSrc)
	copy(embed, eSrc)
	return genre, embed, nil
}
