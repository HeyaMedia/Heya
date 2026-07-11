package sonicanalysis

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// AnalyzerVersion identifies the schema of the analyzer's output.
// Bump whenever a code change invalidates existing facets rows
// (preprocessing tweaks, model swaps, new fields). The scheduler
// re-picks any track whose track_facets.analyzer_version is below
// this constant. Not user-tunable — the model's behaviour is
// determined by the code, not by configuration.
const AnalyzerVersion int32 = 1

// MaxAnalysisDurationSeconds caps which tracks the sonic-analysis
// pipeline will pick up. Tracks longer than this (DJ sets, podcasts,
// lectures, mixes, full live recordings) get skipped — the
// embedding/key/BPM models are trained on song-length material and
// produce noisy facets on long-form content, and the GPU minutes are
// better spent on the long tail of unanalyzed songs. A track with
// duration=0 (unknown) is still picked, on the assumption that
// metadata just hasn't landed yet. Mirrored into the Tasks UI so the
// pending count and items list show only tracks we'd actually
// analyze.
const MaxAnalysisDurationSeconds int32 = 900

// AnalyzerState is the lifecycle phase of an Analyzer. Stored as
// int32 in atomic.Int32 for cheap concurrent state checks.
type AnalyzerState int32

const (
	StateUnloaded AnalyzerState = iota
	StateLoading
	StateReady
	StateUnloading
)

func (s AnalyzerState) String() string {
	switch s {
	case StateUnloaded:
		return "unloaded"
	case StateLoading:
		return "loading"
	case StateReady:
		return "ready"
	case StateUnloading:
		return "unloading"
	default:
		return "unknown"
	}
}

// ErrAnalyzerNotReady is returned by Analyze when the Analyzer's
// model bundle hasn't been loaded yet.
var ErrAnalyzerNotReady = errors.New("sonicanalysis: analyzer not in ready state")

// Config bundles the runtime knobs of an Analyzer. Models live in
// ModelsDir (the directory ModelFetcher writes into); see DESIGN.md
// section 18 for the layout.
//
// Only one Accelerator field — the runtime picks the right EP per
// model shape internally (see dynamicAccelerator()). The CoreML EP
// recompiles its graph for every batch shape, which is great for
// fixed-batch models (Discogs heads, CLAP audio) but ~8× SLOWER for
// dynamic-batch models (base EffNet, classifier heads). So when the
// caller picks CoreML, we silently use CPU for the dynamic-batch
// sessions.
type Config struct {
	ModelsDir   string
	Accelerator Accelerator
}

func (c Config) normalize() Config {
	if c.Accelerator == "" {
		c.Accelerator = AccelAuto
	}
	return c
}

// dynamicAccelerator returns the EP to use for dynamic-batch models
// given the user's primary choice. CoreML is replaced with CPU
// because of the per-call graph-recompile trap; everything else
// passes through.
func (c Config) dynamicAccelerator() Accelerator {
	if c.Accelerator == AccelCoreML {
		return AccelCPU
	}
	return c.Accelerator
}

// modelBundle holds every loaded ONNX session for one Analyzer
// lifetime. Owned exclusively by the Analyzer; never shared across
// instances.
type modelBundle struct {
	heads       *discogsHeadBank
	base        *effnetBaseSession
	classifiers *classifierBank
	clapAudio   *clapAudioSession
}

func (b *modelBundle) close() {
	if b == nil {
		return
	}
	if b.heads != nil {
		b.heads.Close()
	}
	if b.base != nil {
		b.base.Close()
	}
	if b.classifiers != nil {
		b.classifiers.Close()
	}
	if b.clapAudio != nil {
		b.clapAudio.Close()
	}
}

// Analyzer owns one set of loaded models and runs the per-track
// pipeline. Load/Unload toggle which ONNX sessions are alive; Analyze
// requires Ready state.
//
// Not safe for concurrent Analyze calls — the scheduler runs one
// track at a time anyway. State transitions ARE concurrent-safe
// (atomic.Int32) so a status endpoint can read state freely.
type Analyzer struct {
	cfg    Config
	bundle *modelBundle
	state  atomic.Int32
}

// NewAnalyzer constructs an Analyzer with no models loaded. Use
// Load() to actually open the ONNX sessions.
func NewAnalyzer(cfg Config) *Analyzer {
	return &Analyzer{cfg: cfg.normalize()}
}

// State returns the Analyzer's current lifecycle state.
func (a *Analyzer) State() AnalyzerState {
	return AnalyzerState(a.state.Load())
}

// ErrAnalyzerBusy is returned by Reconfigure when the analyzer isn't idle.
var ErrAnalyzerBusy = errors.New("sonicanalysis: analyzer busy; cannot reconfigure")

// Reconfigure swaps the analyzer's config (models dir / accelerator) for the
// next Load. It reserves the state machine with the same Unloaded→Loading CAS
// that Load uses, so it can never interleave with a concurrent Load writing
// a.cfg/a.bundle — reconfigure in place instead of swapping the *Analyzer
// pointer, which would race with concurrent readers of the pointer.
func (a *Analyzer) Reconfigure(cfg Config) error {
	if !a.state.CompareAndSwap(int32(StateUnloaded), int32(StateLoading)) {
		return ErrAnalyzerBusy
	}
	a.cfg = cfg.normalize()
	a.state.Store(int32(StateUnloaded))
	return nil
}

// IsReady is a convenience wrapper around State.
func (a *Analyzer) IsReady() bool {
	return a.State() == StateReady
}

// Load opens every analysis ONNX session in sequence. Idempotent:
// returns nil if already Ready. Cold-load on CoreML is 10-15 s due
// to graph compile; warm load is sub-second once ORT has its cache.
func (a *Analyzer) Load(ctx context.Context) error {
	if !a.state.CompareAndSwap(int32(StateUnloaded), int32(StateLoading)) {
		switch a.State() {
		case StateReady:
			return nil
		case StateLoading:
			return errors.New("sonicanalysis: load already in progress")
		case StateUnloading:
			return errors.New("sonicanalysis: cannot load while unloading")
		}
	}

	log.Info().Str("models_dir", a.cfg.ModelsDir).Msg("sonicanalysis: loading models")
	start := time.Now()

	bundle, err := a.loadBundle(ctx)
	if err != nil {
		a.state.Store(int32(StateUnloaded))
		return err
	}
	a.bundle = bundle
	a.state.Store(int32(StateReady))
	log.Info().Dur("elapsed", time.Since(start)).Msg("sonicanalysis: models ready")
	return nil
}

func (a *Analyzer) loadBundle(ctx context.Context) (*modelBundle, error) {
	bundle := &modelBundle{}
	headsToLoad := []string{HeadTrack, HeadArtist, HeadRelease}
	heads, err := newDiscogsHeadBank(a.cfg.ModelsDir, a.cfg.Accelerator, headsToLoad)
	if err != nil {
		return nil, fmt.Errorf("discogs head bank: %w", err)
	}
	bundle.heads = heads

	basePath := filepath.Join(a.cfg.ModelsDir, "discogs-effnet-bsdynamic-1.onnx")
	base, err := newEffnetBaseSession(basePath, a.cfg.dynamicAccelerator())
	if err != nil {
		bundle.close()
		return nil, fmt.Errorf("effnet base: %w", err)
	}
	bundle.base = base

	classifiers, err := newClassifierBank(filepath.Join(a.cfg.ModelsDir, "heads"), a.cfg.dynamicAccelerator())
	if err != nil {
		bundle.close()
		return nil, fmt.Errorf("classifier bank: %w", err)
	}
	bundle.classifiers = classifiers

	clapPath := filepath.Join(a.cfg.ModelsDir, "clap", "audio_model.onnx")
	clap, err := newClapAudioSession(clapPath, a.cfg.Accelerator)
	if err != nil {
		bundle.close()
		return nil, fmt.Errorf("clap audio: %w", err)
	}
	bundle.clapAudio = clap

	return bundle, nil
}

// Unload destroys every loaded session and frees ~700 MB of resident
// memory. Idempotent: returns immediately if already Unloaded.
func (a *Analyzer) Unload() {
	if !a.state.CompareAndSwap(int32(StateReady), int32(StateUnloading)) {
		return
	}
	log.Info().Msg("sonicanalysis: unloading models")
	a.bundle.close()
	a.bundle = nil
	a.state.Store(int32(StateUnloaded))
}

// AnalyzeStage names each step the pipeline runs through, in the order
// they're invoked. Pass a ProgressFunc to AnalyzeWithProgress to receive
// a callback as each stage starts — useful for UI live-status indicators.
type AnalyzeStage string

const (
	StageDecode16k       AnalyzeStage = "decode 16kHz"
	StageDiscogsHeads    AnalyzeStage = "Discogs embeddings"
	StageEffnetBase      AnalyzeStage = "EffNet base + genre"
	StageClassifierHeads AnalyzeStage = "classifier heads"
	StageBPMKey          AnalyzeStage = "BPM + key"
	StageDecode48k       AnalyzeStage = "decode 48kHz"
	StageClapAudio       AnalyzeStage = "CLAP audio embed"
	StageWaveform        AnalyzeStage = "waveform"
)

// ProgressFunc receives stage start notifications during Analyze.
// Implementations should be cheap — they run on the analysis goroutine
// and any latency stretches per-track wall time. Nil-safe (pass nil
// when no progress reporting is needed).
type ProgressFunc func(stage AnalyzeStage)

// Analyze is the legacy entry point — runs the full pipeline with no
// progress reporting. AnalyzeWithProgress is the same thing with a
// stage callback.
func (a *Analyzer) Analyze(ctx context.Context, audioPath string) (*Facets, error) {
	return a.AnalyzeWithProgress(ctx, audioPath, nil)
}

// AnalyzeOptions lets callers omit independently persisted cheap artifacts.
// The model stages still run normally; this only avoids a redundant decode.
type AnalyzeOptions struct {
	SkipWaveform bool
}

// AnalyzeWithProgress runs the full per-track pipeline, calling
// `progress` at the top of each stage. Returns ErrAnalyzerNotReady if
// state != Ready.
func (a *Analyzer) AnalyzeWithProgress(ctx context.Context, audioPath string, progress ProgressFunc) (*Facets, error) {
	return a.AnalyzeWithProgressOptions(ctx, audioPath, progress, AnalyzeOptions{})
}

// AnalyzeWithProgressOptions is AnalyzeWithProgress with controls for
// artifacts that may already have been generated by playback.
func (a *Analyzer) AnalyzeWithProgressOptions(ctx context.Context, audioPath string, progress ProgressFunc, opts AnalyzeOptions) (*Facets, error) {
	if a.State() != StateReady {
		return nil, ErrAnalyzerNotReady
	}
	start := time.Now()
	emit := func(s AnalyzeStage) {
		if progress != nil {
			progress(s)
		}
	}

	// Stage 1: 16 kHz decode + mel-spec — shared by Discogs heads,
	// base EffNet, classifiers, BPM, key.
	emit(StageDecode16k)
	pcm16, err := decodePCM(ctx, audioPath, melSampleRate)
	if err != nil {
		return nil, fmt.Errorf("decode 16k: %w", err)
	}
	spec, nFrames := melSpec(pcm16)
	patches, nPatches := slicePatches(spec, nFrames)
	if nPatches == 0 {
		return nil, fmt.Errorf("audio shorter than one analysis patch (~2 s)")
	}

	// Stage 2: Discogs specialized heads (track / artist / release).
	emit(StageDiscogsHeads)
	heads := map[string][]float32{}
	for _, h := range a.bundle.heads.Heads() {
		sess := a.bundle.heads.sessions[h]
		patchEmbeds, runErr := runBatched(sess, patches, nPatches)
		if runErr != nil {
			return nil, fmt.Errorf("%s head: %w", h, runErr)
		}
		heads[h] = meanPool(patchEmbeds, nPatches, discogsEmbedDim)
	}

	// Stage 3: base EffNet — gives genre softmax + 1280-dim embeddings
	// for the classifier heads.
	emit(StageEffnetBase)
	genre, embed, err := a.bundle.base.Run(patches, nPatches)
	if err != nil {
		return nil, fmt.Errorf("effnet base: %w", err)
	}
	topGenres := topGenresFromSoftmax(genre, nPatches, 5)

	emit(StageClassifierHeads)
	moodTags, err := a.bundle.classifiers.Tag(embed, nPatches)
	if err != nil {
		return nil, fmt.Errorf("classifier heads: %w", err)
	}

	// Stage 4: BPM + key share the same 16 kHz PCM (no second decode).
	emit(StageBPMKey)
	bpm, bpmConf, _ := detectBPMFromPCM(pcm16)
	keyRes, _ := detectKeyFromPCM(pcm16)

	// Stage 5: CLAP audio — needs its own 48 kHz decode.
	emit(StageDecode48k)
	pcm48, err := decodePCM(ctx, audioPath, clapSampleRate)
	if err != nil {
		return nil, fmt.Errorf("decode 48k: %w", err)
	}
	clapMel := clapMelSpec(pcm48)
	emit(StageClapAudio)
	clapEmbed, err := a.bundle.clapAudio.Embed(clapMel)
	if err != nil {
		return nil, fmt.Errorf("clap audio embed: %w", err)
	}

	// Stage 6: waveform — decode at 8 kHz fresh (cheap).
	//
	// Loudness used to be its own stage here; it now lives exclusively
	// in the track_files pipeline (ScanTrackLoudnessWorker) which runs
	// at probe time and is what the audio engine consumes for replay
	// gain. Skipping the duplicate ebur128 call saves ~1-2s per track.
	var waveform []float32
	if !opts.SkipWaveform {
		emit(StageWaveform)
		waveform, err = ComputeWaveform(ctx, audioPath)
		if err != nil {
			return nil, fmt.Errorf("waveform: %w", err)
		}
	}

	f := &Facets{
		TrackEmbed:    heads[HeadTrack],
		ArtistEmbed:   heads[HeadArtist],
		ReleaseEmbed:  heads[HeadRelease],
		TextEmbed:     clapEmbed,
		BPM:           bpm,
		BPMConfidence: bpmConf,
		TopGenres:     topGenres,
		MoodTags:      moodTags,
		Waveform:      waveform,
		ElapsedMs:     int(time.Since(start).Milliseconds()),
	}
	if keyRes != nil {
		f.Key = keyRes.Key
		f.KeyClarity = keyRes.Clarity
	}
	return f, nil
}

// topGenresFromSoftmax mean-pools per-patch softmaxes and returns
// the top-N (name, score) pairs in descending score order.
func topGenresFromSoftmax(genre []float32, nPatches, n int) []GenreScore {
	mean := make([]float32, effnetGenreDim)
	for p := 0; p < nPatches; p++ {
		off := p * effnetGenreDim
		for c := 0; c < effnetGenreDim; c++ {
			mean[c] += genre[off+c]
		}
	}
	inv := float32(1.0) / float32(nPatches)
	for c := range mean {
		mean[c] *= inv
	}
	type ranked struct {
		idx   int
		score float32
	}
	r := make([]ranked, effnetGenreDim)
	for i, s := range mean {
		r[i] = ranked{i, s}
	}
	sort.Slice(r, func(i, j int) bool { return r[i].score > r[j].score })
	if n > effnetGenreDim {
		n = effnetGenreDim
	}
	if n < 1 {
		n = 5
	}
	out := make([]GenreScore, n)
	for i := 0; i < n; i++ {
		out[i] = GenreScore{Name: GenreName(r[i].idx), Score: r[i].score}
	}
	return out
}
