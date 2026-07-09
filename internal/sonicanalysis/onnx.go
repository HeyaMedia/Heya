package sonicanalysis

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	AccelOpenVINO Accelerator = "openvino"
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
		{AccelOpenVINO, "OpenVINO (Intel)"},
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
		// OpenVINO EP exists for Linux + Windows (Intel CPU/iGPU/Arc); not
		// shipped on macOS, where CoreML is the Intel/Apple path instead.
		if c.name == AccelOpenVINO && runtime.GOOS == "darwin" {
			avail.Reason = "not on macOS"
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
		// Debian multiarch dir is arch-specific. The container image
		// (see Dockerfile runtime stage) drops libonnxruntime.so into the
		// triplet dir matching the build's TARGETARCH, so the path differs
		// between the amd64 and arm64 images.
		if runtime.GOARCH == "arm64" {
			return "/usr/lib/aarch64-linux-gnu/libonnxruntime.so"
		}
		return "/usr/lib/x86_64-linux-gnu/libonnxruntime.so"
	case "windows":
		return "onnxruntime.dll"
	}
	return "libonnxruntime.so"
}

// providerLibFiles maps each execution provider that ONNX Runtime loads
// from a *separate* Linux shared object (co-located with libonnxruntime.so)
// to that object's filename. ORT loads these lazily inside the
// AppendExecutionProvider* call and, when the file is absent, prints a
// misleading
//
//	[E:onnxruntime:...] provider_bridge_ort.cc ... Failed to load library libonnxruntime_providers_*.so
//
// line to stderr before returning the error. The CPU-only base image
// legitimately ships none of these, so on `auto` (and on the status
// endpoint's probe) the CUDA/OpenVINO attempts would each emit that scary
// log even though CPU fallback works fine. We stat the file first and skip
// the append when it's missing — silencing the log and a doomed dlopen.
// CoreML (macOS) and DirectML (Windows) are baked into the core library /
// provided by the OS rather than gated by a sibling .so, so they're
// intentionally omitted here and never pre-gated.
var providerLibFiles = map[Accelerator]string{
	AccelCUDA:     "libonnxruntime_providers_cuda.so",
	AccelOpenVINO: "libonnxruntime_providers_openvino.so",
}

// gatedProviderDir returns the directory a provider shared object must sit
// in for the pre-append gate to fire, or "" to disable the gate entirely.
//
// The gate exists for one purpose: sparing the default CPU container image
// ORT's misleading "Failed to load library libonnxruntime_providers_*.so"
// log when `auto` (or the status probe) tries a GPU EP the image doesn't
// ship. It must never veto a provider an operator wired up themselves, so it
// fires only where the layout is guaranteed: on Linux, with no
// ONNXRUNTIME_LIB override, where the vendor images place the provider next
// to libonnxruntime.so in the system multiarch dir. A custom ONNXRUNTIME_LIB
// means the operator manages ORT and may resolve providers via
// LD_LIBRARY_PATH / ldconfig / rpath from somewhere we can't see — defer to
// ORT. Same on non-Linux (Windows providers are DLLs on the loader search
// path; macOS has no CUDA/OpenVINO build). A package var so tests can point
// it at a scratch dir without abusing the env override.
var gatedProviderDir = func() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	if os.Getenv("ONNXRUNTIME_LIB") != "" {
		return ""
	}
	return filepath.Dir(defaultOnnxLib())
}

// providerLibInstalled reports whether accel's provider shared object is
// present in the gated directory. Accelerators not loaded from a sibling
// .so (CPU, CoreML, DirectML) and any context where the gate is disabled
// (see gatedProviderDir) report true, so ORT decides exactly as it did
// before the gate existed.
func providerLibInstalled(accel Accelerator) bool {
	file, ok := providerLibFiles[accel]
	if !ok {
		return true
	}
	dir := gatedProviderDir()
	if dir == "" {
		return true
	}
	_, err := os.Stat(filepath.Join(dir, file))
	return err == nil
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

// EnsureONNX initializes the shared ONNX Runtime environment (once per process).
// Exposed so sibling subsystems — e.g. the recommendation text-embedder in
// internal/textembed — reuse this environment instead of double-initializing it
// (ort.InitializeEnvironment must not be called twice).
func EnsureONNX() error { return initOnnx() }

// BuildSessionOptions exposes buildSessionOptions for out-of-package session
// construction, sharing the exact accelerator/execution-provider logic.
func BuildSessionOptions(accel Accelerator) (*ort.SessionOptions, string, error) {
	return buildSessionOptions(accel)
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
// description of what got attached. Each EP case allocates its own
// SessionOptions so a failed attempt never leaves a half-configured
// object behind — that matters for the auto path, which tries several.
func buildSessionOptions(accel Accelerator) (*ort.SessionOptions, string, error) {
	switch accel {
	case "", AccelAuto:
		return autoSessionOptions()
	case AccelCPU:
		opts, err := ort.NewSessionOptions()
		if err != nil {
			return nil, "", err
		}
		return opts, "cpu", nil
	case AccelCoreML:
		opts, err := ort.NewSessionOptions()
		if err != nil {
			return nil, "", err
		}
		if err := opts.AppendExecutionProviderCoreML(0); err != nil {
			_ = opts.Destroy()
			return nil, "", fmt.Errorf("coreml EP not available: %w", err)
		}
		return opts, "coreml", nil
	case AccelCUDA:
		if !providerLibInstalled(AccelCUDA) {
			return nil, "", fmt.Errorf("cuda EP not available: provider library %s not installed", providerLibFiles[AccelCUDA])
		}
		opts, err := ort.NewSessionOptions()
		if err != nil {
			return nil, "", err
		}
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
	case AccelOpenVINO:
		if !providerLibInstalled(AccelOpenVINO) {
			return nil, "", fmt.Errorf("openvino EP not available: provider library %s not installed", providerLibFiles[AccelOpenVINO])
		}
		opts, err := ort.NewSessionOptions()
		if err != nil {
			return nil, "", err
		}
		// device_type picks the OpenVINO target — "GPU" binds the Intel
		// iGPU/Arc, "CPU" the OpenVINO CPU plugin, "AUTO" lets OpenVINO
		// choose. Defaults to GPU (the reason to use this EP) and is
		// overridable via HEYA_SONIC_OPENVINO_DEVICE.
		dev := openvinoDevice()
		ovOpts := map[string]string{"device_type": dev}
		// cache_dir persists OpenVINO's compiled kernels (the GPU plugin JITs
		// each model graph on first inference — tens of seconds across our ~14
		// models). With a cache, that cost is paid once and reused across
		// process restarts, so cold-start model load drops dramatically. Off
		// unless HEYA_SONIC_OPENVINO_CACHE_DIR is set (the openvino image sets
		// it to a path under the data volume).
		if cacheDir := strings.TrimSpace(os.Getenv("HEYA_SONIC_OPENVINO_CACHE_DIR")); cacheDir != "" {
			ovOpts["cache_dir"] = cacheDir
		}
		if err := opts.AppendExecutionProviderOpenVINO(ovOpts); err != nil {
			_ = opts.Destroy()
			return nil, "", fmt.Errorf("openvino EP not available: %w", err)
		}
		return opts, "openvino (" + dev + ")", nil
	case AccelDirectML:
		opts, err := ort.NewSessionOptions()
		if err != nil {
			return nil, "", err
		}
		if err := opts.AppendExecutionProviderDirectML(0); err != nil {
			_ = opts.Destroy()
			return nil, "", fmt.Errorf("directml EP not available: %w", err)
		}
		return opts, "directml", nil
	default:
		return nil, "", fmt.Errorf("unknown accelerator %q", accel)
	}
}

// autoSessionOptions resolves AccelAuto to the best EP actually present in
// this build's onnxruntime. On macOS that's CoreML; on Linux we try the GPU
// providers a vendor image may have compiled in (CUDA, then OpenVINO), each
// of which errors cleanly when its provider lib is absent, before falling
// back to CPU. This makes the per-vendor images self-configure even if
// HEYA_SONIC_ACCELERATOR is left at the default.
func autoSessionOptions() (*ort.SessionOptions, string, error) {
	var order []Accelerator
	switch runtime.GOOS {
	case "darwin":
		order = []Accelerator{AccelCoreML}
	case "linux":
		order = []Accelerator{AccelCUDA, AccelOpenVINO}
	}
	for _, c := range order {
		if opts, desc, err := buildSessionOptions(c); err == nil {
			return opts, desc + " (auto)", nil
		}
	}
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, "", err
	}
	return opts, "cpu (auto fallback)", nil
}

// openvinoDevice is the OpenVINO device_type the OpenVINO EP binds to.
// Defaults to GPU (the Intel iGPU/Arc), overridable for CPU/AUTO/GPU.1 etc.
func openvinoDevice() string {
	if v := strings.TrimSpace(os.Getenv("HEYA_SONIC_OPENVINO_DEVICE")); v != "" {
		return v
	}
	return "GPU"
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
