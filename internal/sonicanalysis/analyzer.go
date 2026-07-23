package sonicanalysis

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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

// CurrentCLAPWindows is the persisted coverage level of the CLAP audio
// embedding. Version 1 rows used only the center ten seconds. Current rows
// mean-pool deterministic windows centered at 20%, 50%, and 80%.
const CurrentCLAPWindows int16 = 3

// CLAPEmbeddingDimensions is exported for persistence adapters that need to
// validate legacy pgvector values before attempting an incremental upgrade.
const CLAPEmbeddingDimensions = clapEmbedDim

var (
	clapTrackPositions      = []float64{0.2, 0.5, 0.8}
	clapAdditionalPositions = []float64{0.2, 0.8}
)

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

const (
	// DefaultPreprocessAhead keeps enough CPU-prepared tracks queued to cover
	// the decode/DSP gaps visible between short GPU inference bursts without
	// monopolising a smaller host. The Sonic settings page can raise this on
	// machines with more CPU and RAM.
	DefaultPreprocessAhead = 4
	DefaultGPUWorkers      = 1
	MaxPreprocessAhead     = 32
	MaxGPUWorkers          = 8
)

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
	ModelsDir       string
	Accelerator     Accelerator
	PreprocessAhead int
	GPUWorkers      int
}

func (c Config) normalize() Config {
	if c.Accelerator == "" {
		c.Accelerator = AccelAuto
	}
	if c.PreprocessAhead <= 0 {
		c.PreprocessAhead = DefaultPreprocessAhead
	}
	if c.PreprocessAhead > MaxPreprocessAhead {
		c.PreprocessAhead = MaxPreprocessAhead
	}
	if c.GPUWorkers <= 0 {
		c.GPUWorkers = DefaultGPUWorkers
	}
	if c.GPUWorkers > MaxGPUWorkers {
		c.GPUWorkers = MaxGPUWorkers
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
	heads              *discogsHeadBank
	base               *effnetBaseSession
	classifiers        *classifierBank
	clapAudio          *clapAudioSession
	serializeInference bool
}

// OpenVINO's execution provider crashed inside ONNX Runtime when separate
// sessions inferred concurrently. This lock is package-wide—not analyzer
// scoped—because the independently-lived CLAP text search session must not
// overlap the scheduled audio analyzer either.
var openVINOInferenceMu sync.Mutex

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

// Analyzer owns one set of loaded models and runs the per-track pipeline.
// CPU preprocessing and GPU inference have independent bounded lanes:
// PreprocessAhead tracks may prepare in parallel and wait behind GPUWorkers
// inference lanes. All callers share this one model bundle; per-session locks
// protect ONNX sessions and their reusable tensors.
type Analyzer struct {
	cfg             Config
	bundle          *modelBundle
	state           atomic.Int32
	pipelineSlots   chan struct{}
	preprocessSlots chan struct{}
	gpuSlots        chan struct{}
	configMu        sync.RWMutex
}

// NewAnalyzer constructs an Analyzer with no models loaded. Use
// Load() to actually open the ONNX sessions.
func NewAnalyzer(cfg Config) *Analyzer {
	a := &Analyzer{}
	a.applyConfig(cfg.normalize())
	return a
}

func (a *Analyzer) applyConfig(cfg Config) {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	a.cfg = cfg
	a.pipelineSlots = make(chan struct{}, cfg.PreprocessAhead+cfg.GPUWorkers)
	a.preprocessSlots = make(chan struct{}, cfg.PreprocessAhead)
	a.gpuSlots = make(chan struct{}, cfg.GPUWorkers)
}

// PipelineWorkers is the River queue width needed to keep every configured
// CPU/GPU lane occupied without starting surplus jobs that merely wait.
func (a *Analyzer) PipelineWorkers() int {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return a.cfg.PreprocessAhead + a.cfg.GPUWorkers
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
	a.applyConfig(cfg.normalize())
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
	bundle.serializeInference = strings.Contains(heads.usedEP, "openvino") ||
		strings.Contains(base.usedEP, "openvino") ||
		strings.Contains(classifiers.usedEP, "openvino") ||
		strings.Contains(clap.usedEP, "openvino")
	if bundle.serializeInference {
		log.Info().Msg("sonicanalysis: OpenVINO detected; serializing model inference for runtime safety")
	}

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
	SkipWaveform   bool
	SkipBoundaries bool
}

type preparedAnalysis struct {
	patches       []float32
	nPatches      int
	bpm           float64
	bpmConfidence float64
	key           *KeyResult
	clapMels      [][]float32
	waveform      []float32
	boundaries    *Boundaries
}

// asyncPreparation lets the two model-input builders unblock GPU inference
// while independent BPM/key/envelope work is still finishing on the shared
// 16 kHz PCM.
type asyncPreparation struct {
	prepared  *preparedAnalysis
	modelDone chan struct{}
	allDone   chan struct{}

	errMu sync.Mutex
	err   error
}

func (p *asyncPreparation) recordError(err error) {
	if err == nil {
		return
	}
	p.errMu.Lock()
	if p.err == nil {
		p.err = err
	}
	p.errMu.Unlock()
}

func (p *asyncPreparation) currentError() error {
	p.errMu.Lock()
	defer p.errMu.Unlock()
	return p.err
}

func (p *asyncPreparation) wait(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return p.currentError()
	case <-ctx.Done():
		return ctx.Err()
	}
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
	a.configMu.RLock()
	pipelineSlots := a.pipelineSlots
	preprocessSlots := a.preprocessSlots
	gpuSlots := a.gpuSlots
	a.configMu.RUnlock()

	if err := acquireAnalysisSlot(ctx, pipelineSlots); err != nil {
		return nil, err
	}
	defer releaseAnalysisSlot(pipelineSlots)

	emit := func(s AnalyzeStage) {
		if progress != nil {
			progress(s)
		}
	}

	if err := acquireAnalysisSlot(ctx, preprocessSlots); err != nil {
		return nil, err
	}
	preparation, err := startAnalysisPreparation(ctx, audioPath, emit, opts)
	if err != nil {
		releaseAnalysisSlot(preprocessSlots)
		return nil, err
	}
	preprocessReleased := make(chan struct{})
	go func() {
		<-preparation.allDone
		releaseAnalysisSlot(preprocessSlots)
		close(preprocessReleased)
	}()
	defer func() { <-preprocessReleased }()

	if err := preparation.wait(ctx, preparation.modelDone); err != nil {
		return nil, err
	}

	if err := acquireAnalysisSlot(ctx, gpuSlots); err != nil {
		return nil, err
	}
	facets, err := a.inferPrepared(preparation.prepared, emit)
	releaseAnalysisSlot(gpuSlots)
	if err != nil {
		return nil, err
	}
	if err := preparation.wait(ctx, preparation.allDone); err != nil {
		return nil, err
	}
	facets.BPM = preparation.prepared.bpm
	facets.BPMConfidence = preparation.prepared.bpmConfidence
	facets.Waveform = preparation.prepared.waveform
	facets.Boundaries = preparation.prepared.boundaries
	if preparation.prepared.key != nil {
		facets.Key = preparation.prepared.key.Key
		facets.KeyClarity = preparation.prepared.key.Clarity
	}
	facets.ElapsedMs = int(time.Since(start).Milliseconds())
	return facets, nil
}

func acquireAnalysisSlot(ctx context.Context, slots chan struct{}) error {
	select {
	case slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseAnalysisSlot(slots chan struct{}) {
	<-slots
}

func startAnalysisPreparation(ctx context.Context, audioPath string, emit func(AnalyzeStage), opts AnalyzeOptions) (*asyncPreparation, error) {
	emit(StageDecode16k)
	emit(StageDecode48k)
	decoded, err := decodeAnalysisAudio(ctx, audioPath, clapTrackPositions, true)
	if err != nil {
		return nil, fmt.Errorf("shared audio decode: %w", err)
	}
	emit(StageBPMKey)
	if !opts.SkipWaveform || !opts.SkipBoundaries {
		emit(StageWaveform)
	}

	preparation := &asyncPreparation{
		prepared:  &preparedAnalysis{},
		modelDone: make(chan struct{}),
		allDone:   make(chan struct{}),
	}
	var modelWG sync.WaitGroup
	var allWG sync.WaitGroup
	startTask := func(modelInput bool, fn func() error) {
		allWG.Add(1)
		if modelInput {
			modelWG.Add(1)
		}
		go func() {
			defer allWG.Done()
			if modelInput {
				defer modelWG.Done()
			}
			preparation.recordError(fn())
		}()
	}

	startTask(true, func() error {
		spec, nFrames := melSpec(decoded.PCM16)
		patches, nPatches := slicePatches(spec, nFrames)
		if nPatches == 0 {
			return fmt.Errorf("audio shorter than one analysis patch (~2 s)")
		}
		preparation.prepared.patches = patches
		preparation.prepared.nPatches = nPatches
		return nil
	})
	startTask(true, func() error {
		preparation.prepared.clapMels = prepareCLAPMels(decoded.CLAPClips)
		return nil
	})
	startTask(false, func() error {
		preparation.prepared.bpm, preparation.prepared.bpmConfidence, _ = detectBPMFromPCM(decoded.PCM16)
		return nil
	})
	startTask(false, func() error {
		preparation.prepared.key, _ = detectKeyFromPCM(decoded.PCM16)
		return nil
	})
	if !opts.SkipWaveform || !opts.SkipBoundaries {
		startTask(false, func() error {
			if !opts.SkipWaveform {
				waveform, waveformErr := waveformFromPCM(decoded.PCM16, waveformDefaultN)
				if waveformErr != nil {
					return fmt.Errorf("waveform: %w", waveformErr)
				}
				preparation.prepared.waveform = waveform
			}
			if !opts.SkipBoundaries {
				preparation.prepared.boundaries = boundariesFromPCM(decoded.PCM16, melSampleRate)
			}
			return nil
		})
	}

	go func() {
		modelWG.Wait()
		close(preparation.modelDone)
	}()
	go func() {
		allWG.Wait()
		close(preparation.allDone)
	}()
	return preparation, nil
}

func (a *Analyzer) inferPrepared(prepared *preparedAnalysis, emit func(AnalyzeStage)) (*Facets, error) {
	if a.bundle.serializeInference {
		openVINOInferenceMu.Lock()
		defer openVINOInferenceMu.Unlock()
	}

	emit(StageDiscogsHeads)
	heads := map[string][]float32{}
	for _, h := range a.bundle.heads.Heads() {
		sess := a.bundle.heads.sessions[h]
		patchEmbeds, runErr := runBatched(sess, prepared.patches, prepared.nPatches)
		if runErr != nil {
			return nil, fmt.Errorf("%s head: %w", h, runErr)
		}
		heads[h] = meanPool(patchEmbeds, prepared.nPatches, discogsEmbedDim)
	}

	emit(StageEffnetBase)
	genre, embed, err := a.bundle.base.Run(prepared.patches, prepared.nPatches)
	if err != nil {
		return nil, fmt.Errorf("effnet base: %w", err)
	}
	topGenres := topGenresFromSoftmax(genre, prepared.nPatches, 5)

	emit(StageClassifierHeads)
	moodTags, err := a.bundle.classifiers.Tag(embed, prepared.nPatches)
	if err != nil {
		return nil, fmt.Errorf("classifier heads: %w", err)
	}

	emit(StageClapAudio)
	clapEmbeds := make([][]float32, 0, len(prepared.clapMels))
	for i, mel := range prepared.clapMels {
		clapEmbed, embedErr := a.bundle.clapAudio.Embed(mel)
		if embedErr != nil {
			return nil, fmt.Errorf("clap audio window %d: %w", i+1, embedErr)
		}
		clapEmbeds = append(clapEmbeds, clapEmbed)
	}
	clapEmbed, err := meanCLAPEmbeddings(clapEmbeds)
	if err != nil {
		return nil, err
	}

	return &Facets{
		TrackEmbed:   heads[HeadTrack],
		ArtistEmbed:  heads[HeadArtist],
		ReleaseEmbed: heads[HeadRelease],
		TextEmbed:    clapEmbed,
		TopGenres:    topGenres,
		MoodTags:     moodTags,
	}, nil
}

// AugmentCLAPWithProgress upgrades a legacy center-only embedding by adding
// deterministic windows at 20% and 80%. It exercises only the CLAP path; the
// existing Discogs/BPM/key facets remain untouched.
func (a *Analyzer) AugmentCLAPWithProgress(
	ctx context.Context,
	audioPath string,
	existingCenter []float32,
	progress ProgressFunc,
) ([]float32, error) {
	if a.State() != StateReady {
		return nil, ErrAnalyzerNotReady
	}
	if len(existingCenter) != clapEmbedDim {
		return nil, fmt.Errorf("legacy CLAP embedding has %d dimensions, want %d", len(existingCenter), clapEmbedDim)
	}
	center := append([]float32(nil), existingCenter...)
	l2Normalize(center)

	a.configMu.RLock()
	pipelineSlots := a.pipelineSlots
	preprocessSlots := a.preprocessSlots
	gpuSlots := a.gpuSlots
	a.configMu.RUnlock()
	if err := acquireAnalysisSlot(ctx, pipelineSlots); err != nil {
		return nil, err
	}
	defer releaseAnalysisSlot(pipelineSlots)
	if err := acquireAnalysisSlot(ctx, preprocessSlots); err != nil {
		return nil, err
	}

	if progress != nil {
		progress(StageDecode48k)
	}
	decoded, err := decodeAnalysisAudio(ctx, audioPath, clapAdditionalPositions, false)
	if err == nil {
		mels := prepareCLAPMels(decoded.CLAPClips)
		releaseAnalysisSlot(preprocessSlots)
		if err = acquireAnalysisSlot(ctx, gpuSlots); err == nil {
			if a.bundle.serializeInference {
				openVINOInferenceMu.Lock()
			}
			if progress != nil {
				progress(StageClapAudio)
			}
			embeds := make([][]float32, 0, len(mels)+1)
			embeds = append(embeds, center)
			for i, mel := range mels {
				var embed []float32
				embed, err = a.bundle.clapAudio.Embed(mel)
				if err != nil {
					err = fmt.Errorf("clap audio window %d: %w", i+1, err)
					break
				}
				embeds = append(embeds, embed)
			}
			if a.bundle.serializeInference {
				openVINOInferenceMu.Unlock()
			}
			releaseAnalysisSlot(gpuSlots)
			if err == nil {
				return meanCLAPEmbeddings(embeds)
			}
		}
	} else {
		releaseAnalysisSlot(preprocessSlots)
	}
	return nil, err
}

func prepareCLAPMels(clips [][]float32) [][]float32 {
	out := make([][]float32, len(clips))
	jobs := make(chan int)
	var wg sync.WaitGroup
	workers := min(2, len(clips))
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				out[i] = clapMelSpec(clips[i])
			}
		}()
	}
	for i := range clips {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return out
}

func meanCLAPEmbeddings(embeds [][]float32) ([]float32, error) {
	if len(embeds) == 0 {
		return nil, fmt.Errorf("no CLAP embeddings to aggregate")
	}
	out := make([]float32, clapEmbedDim)
	for i, embed := range embeds {
		if len(embed) != clapEmbedDim {
			return nil, fmt.Errorf("CLAP embedding %d has %d dimensions, want %d", i, len(embed), clapEmbedDim)
		}
		for j, value := range embed {
			out[j] += value
		}
	}
	l2Normalize(out)
	return out, nil
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
