package sonicanalysis

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	discogsInputName  = "melspectrogram"
	discogsOutputName = "embeddings"
	discogsBatchSize  = 64
	discogsTimeFrames = 128
	discogsMelBands   = 96
	discogsEmbedDim   = 512
)

// Accelerator selects an ONNX Runtime execution provider. The `auto`
// path tries the best available EP for the host and falls back to CPU
// silently on any error. Specific EP names are honored strictly — if
// the requested EP isn't compiled into the ONNX Runtime build, you
// get an error rather than a silent demotion.
type Accelerator string

const (
	AccelAuto     Accelerator = "auto"
	AccelCPU      Accelerator = "cpu"
	AccelCoreML   Accelerator = "coreml"
	AccelCUDA     Accelerator = "cuda"
	AccelDirectML Accelerator = "directml"
	AccelROCm     Accelerator = "rocm"
)

// AcceleratorAvailability records what each candidate EP looks like
// on this host: present, not compiled in, or hidden by the platform.
type AcceleratorAvailability struct {
	Name      Accelerator `json:"name"`
	Label     string      `json:"label"`
	Available bool        `json:"available"`
	Reason    string      `json:"reason,omitempty"` // populated when Available=false
}

// AvailableAccelerators probes each known accelerator on the current
// host by attempting to attach it to a fresh SessionOptions. The
// probe is fast (no model load), so we can call it whenever the
// status endpoint runs without measurable overhead.
//
// CPU is always available (it's the ORT default when no EP is
// attached). The auto entry advertises which physical EP `auto`
// would resolve to right now.
func AvailableAccelerators() []AcceleratorAvailability {
	if err := initOnnx(); err != nil {
		// ORT itself isn't loadable — only CPU is meaningful, and even
		// that won't work. Return everything as unavailable except CPU
		// with a reason.
		return []AcceleratorAvailability{
			{Name: AccelCPU, Label: "CPU", Available: false, Reason: "onnxruntime not loadable: " + err.Error()},
		}
	}

	candidates := []struct {
		name  Accelerator
		label string
	}{
		{AccelCPU, "CPU"},
		{AccelCoreML, "CoreML (macOS)"},
		{AccelCUDA, "CUDA (NVIDIA)"},
		{AccelDirectML, "DirectML (Windows)"},
	}

	out := make([]AcceleratorAvailability, 0, len(candidates)+1)
	autoResolves := AccelCPU
	for _, c := range candidates {
		avail := AcceleratorAvailability{Name: c.name, Label: c.label}
		if c.name == AccelCPU {
			avail.Available = true
			out = append(out, avail)
			continue
		}
		// Skip OS-mismatched EPs early — keeps the dropdown clean and
		// avoids confusing "not compiled in" errors that are really
		// platform mismatches.
		if c.name == AccelCoreML && runtime.GOOS != "darwin" {
			avail.Reason = "macOS only"
			out = append(out, avail)
			continue
		}
		if c.name == AccelDirectML && runtime.GOOS != "windows" {
			avail.Reason = "Windows only"
			out = append(out, avail)
			continue
		}
		// Probe via a throwaway SessionOptions.
		_, _, err := buildSessionOptions(c.name)
		if err == nil {
			avail.Available = true
			if autoResolves == AccelCPU {
				autoResolves = c.name
			}
		} else {
			avail.Reason = err.Error()
		}
		out = append(out, avail)
	}

	// Prepend the synthetic "auto" entry so the UI can show which
	// physical EP auto-discovery picks.
	autoLabel := "Auto"
	for _, a := range out {
		if a.Name == autoResolves {
			autoLabel = "Auto (uses " + a.Label + ")"
			break
		}
	}
	out = append([]AcceleratorAvailability{
		{Name: AccelAuto, Label: autoLabel, Available: true},
	}, out...)

	return out
}

var (
	ortInitOnce sync.Once
	ortInitErr  error
)

// defaultOnnxLib returns the platform-appropriate path to the ONNX
// Runtime shared library. Override with $ONNXRUNTIME_LIB.
func defaultOnnxLib() string {
	if env := os.Getenv("ONNXRUNTIME_LIB"); env != "" {
		return env
	}
	switch runtime.GOOS {
	case "darwin":
		return "/opt/homebrew/lib/libonnxruntime.dylib"
	case "linux":
		return "/usr/lib/x86_64-linux-gnu/libonnxruntime.so"
	case "windows":
		return "onnxruntime.dll"
	}
	return "libonnxruntime.so"
}

// initOnnx initializes the ONNX Runtime environment exactly once for
// the process. Safe to call from multiple commands.
func initOnnx() error {
	ortInitOnce.Do(func() {
		ort.SetSharedLibraryPath(defaultOnnxLib())
		ortInitErr = ort.InitializeEnvironment()
	})
	return ortInitErr
}

// Names for the Discogs specialized embedding heads. Each shares the
// same EffNet backbone + same (64, 128, 96) mel-spec input, but is
// trained with a contrastive loss targeting a different aggregation
// level. Per Alonso et al., "Music Representation Learning Based on
// Editorial Metadata from Discogs" (ISMIR 2022).
const (
	HeadTrack   = "track"
	HeadArtist  = "artist"
	HeadRelease = "release"
)

var discogsHeadFiles = map[string]string{
	HeadTrack:   "discogs_track_embeddings-effnet-bs64-1.onnx",
	HeadArtist:  "discogs_artist_embeddings-effnet-bs64-1.onnx",
	HeadRelease: "discogs_release_embeddings-effnet-bs64-1.onnx",
}

// discogsSession wraps a loaded Discogs-EffNet track-embeddings ONNX
// session bound to fixed-size input/output tensors. Inference is
// single-threaded; callers must serialize InferBatch calls.
type discogsSession struct {
	session *ort.AdvancedSession
	input   *ort.Tensor[float32]
	output  *ort.Tensor[float32]
	usedEP  string
}

// buildSessionOptions wires the requested execution provider onto a
// fresh SessionOptions, returning the options + a human-readable
// description of what got attached.
func buildSessionOptions(accel Accelerator) (*ort.SessionOptions, string, error) {
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, "", err
	}
	switch accel {
	case "", AccelAuto:
		if runtime.GOOS == "darwin" {
			if err := opts.AppendExecutionProviderCoreML(0); err == nil {
				return opts, "coreml (auto)", nil
			}
		}
		return opts, "cpu (auto fallback)", nil
	case AccelCPU:
		return opts, "cpu", nil
	case AccelCoreML:
		if err := opts.AppendExecutionProviderCoreML(0); err != nil {
			_ = opts.Destroy()
			return nil, "", fmt.Errorf("coreml EP not available: %w", err)
		}
		return opts, "coreml", nil
	case AccelCUDA:
		cudaOpts, err := ort.NewCUDAProviderOptions()
		if err != nil {
			_ = opts.Destroy()
			return nil, "", err
		}
		defer func() { _ = cudaOpts.Destroy() }()
		if err := opts.AppendExecutionProviderCUDA(cudaOpts); err != nil {
			_ = opts.Destroy()
			return nil, "", fmt.Errorf("cuda EP not available: %w", err)
		}
		return opts, "cuda", nil
	case AccelDirectML:
		if err := opts.AppendExecutionProviderDirectML(0); err != nil {
			_ = opts.Destroy()
			return nil, "", fmt.Errorf("directml EP not available: %w", err)
		}
		return opts, "directml", nil
	default:
		_ = opts.Destroy()
		return nil, "", fmt.Errorf("unknown accelerator %q", accel)
	}
}

func newDiscogsSession(modelPath string, accel Accelerator) (*discogsSession, error) {
	if err := initOnnx(); err != nil {
		return nil, fmt.Errorf("onnxruntime init: %w", err)
	}
	opts, epDesc, err := buildSessionOptions(accel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = opts.Destroy() }()
	inShape := ort.NewShape(discogsBatchSize, discogsTimeFrames, discogsMelBands)
	input, err := ort.NewEmptyTensor[float32](inShape)
	if err != nil {
		return nil, fmt.Errorf("alloc input tensor: %w", err)
	}
	outShape := ort.NewShape(discogsBatchSize, discogsEmbedDim)
	output, err := ort.NewEmptyTensor[float32](outShape)
	if err != nil {
		_ = input.Destroy()
		return nil, fmt.Errorf("alloc output tensor: %w", err)
	}
	sess, err := ort.NewAdvancedSession(
		modelPath,
		[]string{discogsInputName},
		[]string{discogsOutputName},
		[]ort.Value{input},
		[]ort.Value{output},
		opts,
	)
	if err != nil {
		_ = input.Destroy()
		_ = output.Destroy()
		return nil, fmt.Errorf("load session %s (ep=%s): %w", modelPath, epDesc, err)
	}
	return &discogsSession{session: sess, input: input, output: output, usedEP: epDesc}, nil
}

func (d *discogsSession) Close() {
	if d.session != nil {
		_ = d.session.Destroy()
	}
	if d.input != nil {
		_ = d.input.Destroy()
	}
	if d.output != nil {
		_ = d.output.Destroy()
	}
}

// discogsHeadBank holds N Discogs specialized embedding sessions
// (track, artist, release) keyed by head name. All sessions share the
// same mel-spec input shape, so the caller can feed each one the same
// patches tensor in turn and collect their 512-dim outputs.
type discogsHeadBank struct {
	sessions map[string]*discogsSession
	usedEP   string
}

func newDiscogsHeadBank(modelDir string, accel Accelerator, heads []string) (*discogsHeadBank, error) {
	bank := &discogsHeadBank{sessions: make(map[string]*discogsSession, len(heads))}
	for _, h := range heads {
		file, ok := discogsHeadFiles[h]
		if !ok {
			bank.Close()
			return nil, fmt.Errorf("unknown discogs head %q", h)
		}
		sess, err := newDiscogsSession(filepath.Join(modelDir, file), accel)
		if err != nil {
			bank.Close()
			return nil, fmt.Errorf("load %s head: %w", h, err)
		}
		bank.sessions[h] = sess
		bank.usedEP = sess.usedEP
	}
	return bank, nil
}

func (b *discogsHeadBank) Close() {
	for _, s := range b.sessions {
		s.Close()
	}
}

// Heads returns the head names in a stable order (track, artist,
// release) for consistent iteration.
func (b *discogsHeadBank) Heads() []string {
	order := []string{HeadTrack, HeadArtist, HeadRelease}
	out := make([]string, 0, len(b.sessions))
	for _, h := range order {
		if _, ok := b.sessions[h]; ok {
			out = append(out, h)
		}
	}
	return out
}

// InferBatch copies one batch of mel-spec patches into the input
// tensor, runs the model, and returns a copy of the batch's
// (batchSize, embedDim) output. patches must be exactly
// batchSize*timeFrames*melBands floats laid out row-major.
func (d *discogsSession) InferBatch(patches []float32) ([]float32, error) {
	wantLen := discogsBatchSize * discogsTimeFrames * discogsMelBands
	if len(patches) != wantLen {
		return nil, fmt.Errorf("expected %d input floats, got %d", wantLen, len(patches))
	}
	copy(d.input.GetData(), patches)
	if err := d.session.Run(); err != nil {
		return nil, fmt.Errorf("session.Run: %w", err)
	}
	src := d.output.GetData()
	out := make([]float32, len(src))
	copy(out, src)
	return out, nil
}
