package sonicanalysis

import (
	"fmt"
	"path/filepath"

	ort "github.com/yalue/onnxruntime_go"
)

// classifier heads — small MLPs that take a 1280-dim Discogs-EffNet
// embedding and produce a 2-class softmax probability. Used for
// mood/danceability/voice-instrumental tagging.

const (
	headInputName  = "embeddings"
	headOutputName = "activations"
	headOutDim     = 2
)

// headSpec describes one classifier head. `PosIndex` is the index in
// the model's 2-class softmax that corresponds to the "positive"
// sense of the tag (e.g. "danceable" for danceability). Explicitly
// tracked per head because Essentia's class order isn't consistent.
//
// Verified against each model's .json `classes` metadata:
//
//	danceability       : [danceable,     not_danceable]    → 0
//	mood_acoustic      : [acoustic,      non_acoustic]     → 0
//	mood_aggressive    : [aggressive,    not_aggressive]   → 0
//	mood_electronic    : [electronic,    non_electronic]   → 0
//	mood_happy         : [happy,         non_happy]        → 0
//	mood_party         : [non_party,     party]            → 1
//	mood_relaxed       : [non_relaxed,   relaxed]          → 1
//	mood_sad           : [non_sad,       sad]              → 1
//	voice_instrumental : [instrumental,  voice]            → 1
type headSpec struct {
	Name     string // tag display name (matches positive sense)
	File     string // filename inside the heads dir
	PosIndex int    // 0 or 1 — index of the positive class in softmax
}

var classifierHeadSpecs = []headSpec{
	{string(MoodDanceability), "danceability-discogs-effnet-1.onnx", 0},
	{string(MoodVoice), "voice_instrumental-discogs-effnet-1.onnx", 1},
	{string(MoodHappy), "mood_happy-discogs-effnet-1.onnx", 0},
	{string(MoodSad), "mood_sad-discogs-effnet-1.onnx", 1},
	{string(MoodAggressive), "mood_aggressive-discogs-effnet-1.onnx", 0},
	{string(MoodRelaxed), "mood_relaxed-discogs-effnet-1.onnx", 1},
	{string(MoodParty), "mood_party-discogs-effnet-1.onnx", 1},
	{string(MoodElectronic), "mood_electronic-discogs-effnet-1.onnx", 0},
	{string(MoodAcoustic), "mood_acoustic-discogs-effnet-1.onnx", 0},
}

type classifierHead struct {
	spec    headSpec
	session *ort.DynamicAdvancedSession
}

type classifierBank struct {
	heads  []*classifierHead
	usedEP string
}

func newClassifierBank(headsDir string, accel Accelerator) (*classifierBank, error) {
	if err := initOnnx(); err != nil {
		return nil, fmt.Errorf("onnxruntime init: %w", err)
	}
	opts, epDesc, err := buildSessionOptions(accel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = opts.Destroy() }()

	bank := &classifierBank{usedEP: epDesc, heads: make([]*classifierHead, 0, len(classifierHeadSpecs))}
	for _, spec := range classifierHeadSpecs {
		path := filepath.Join(headsDir, spec.File)
		sess, err := ort.NewDynamicAdvancedSession(
			path,
			[]string{headInputName},
			[]string{headOutputName},
			opts,
		)
		if err != nil {
			bank.Close()
			return nil, fmt.Errorf("load head %s: %w", spec.Name, err)
		}
		bank.heads = append(bank.heads, &classifierHead{spec: spec, session: sess})
	}
	return bank, nil
}

func (c *classifierBank) Close() {
	for _, h := range c.heads {
		if h.session != nil {
			_ = h.session.Destroy()
		}
	}
}

// Tag runs every classifier head against the (nPatches, 1280)
// embedding tensor, mean-pools per-patch softmaxes, and returns
// P(positive class) per head — using the explicit PosIndex from
// each head's spec.
func (c *classifierBank) Tag(embeddings []float32, nPatches int) (MoodScores, error) {
	if len(embeddings) != nPatches*effnetEmbedDim {
		return nil, fmt.Errorf("expected %d embedding floats, got %d",
			nPatches*effnetEmbedDim, len(embeddings))
	}

	inShape := ort.NewShape(int64(nPatches), int64(effnetEmbedDim))
	inputTensor, err := ort.NewTensor(inShape, embeddings)
	if err != nil {
		return nil, err
	}
	defer func() { _ = inputTensor.Destroy() }()

	outShape := ort.NewShape(int64(nPatches), int64(headOutDim))
	out := make(MoodScores, len(c.heads))
	for _, h := range c.heads {
		outputTensor, err := ort.NewEmptyTensor[float32](outShape)
		if err != nil {
			return nil, err
		}
		err = h.session.Run([]ort.Value{inputTensor}, []ort.Value{outputTensor})
		if err != nil {
			_ = outputTensor.Destroy()
			return nil, fmt.Errorf("head %s: %w", h.spec.Name, err)
		}
		probs := outputTensor.GetData()
		var sum float64
		for p := 0; p < nPatches; p++ {
			sum += float64(probs[p*headOutDim+h.spec.PosIndex])
		}
		_ = outputTensor.Destroy()
		out[MoodTagName(h.spec.Name)] = float32(sum / float64(nPatches))
	}
	return out, nil
}
