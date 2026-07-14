package imagegen

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DownloadState string

const (
	DownloadIdle        DownloadState = "idle"
	DownloadDownloading DownloadState = "downloading"
	DownloadReady       DownloadState = "ready"
	DownloadFailed      DownloadState = "failed"
)

type ImageDownloadProgress struct {
	CurrentFile string    `json:"current_file,omitempty"`
	BytesDone   int64     `json:"bytes_done"`
	BytesTotal  int64     `json:"bytes_total"`
	StartedAt   time.Time `json:"started_at"`
}

type ArtifactStatus struct {
	Role    string `json:"role"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Present bool   `json:"present"`
	Shared  bool   `json:"shared"`
}

// ComputeDevice is a compute target reported by stable-diffusion.cpp. Name is the
// stable token accepted by sd-cli's --backend option (for example Vulkan0).
type ComputeDevice struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Runtime struct {
	dir         string
	mu          sync.RWMutex
	generateMu  sync.Mutex
	devicesMu   sync.Mutex
	devices     map[string][]ComputeDevice
	state       DownloadState
	progress    ImageDownloadProgress
	downloadErr string
}

func NewRuntime(dir string) *Runtime {
	if absolute, err := filepath.Abs(dir); err == nil {
		dir = absolute
	}
	return &Runtime{dir: dir, state: DownloadIdle, devices: make(map[string][]ComputeDevice)}
}
func (r *Runtime) modelPath(a ModelArtifact) string { return filepath.Join(r.dir, "models", a.Name) }
func (r *Runtime) artifactPath(a ModelArtifact) string {
	local := r.modelPath(a)
	if regularSize(local, a.Size) {
		return local
	}
	if a.SharedLLMFile != "" {
		shared := filepath.Join(filepath.Dir(r.dir), "llm", "models", a.SharedLLMFile)
		if regularSize(shared, a.Size) {
			return shared
		}
	}
	return local
}
func (r *Runtime) runtimeDir(a Artifact) string {
	return filepath.Join(r.dir, "runtime", strings.TrimSuffix(a.Name, ".zip"))
}
func (r *Runtime) binaryPath(a Artifact) string {
	name := "sd-cli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(r.runtimeDir(a), name)
}
func regularSize(path string, size int64) bool {
	st, e := os.Stat(path)
	return e == nil && st.Mode().IsRegular() && st.Size() == size
}
func (r *Runtime) ModelPresent(id string) bool {
	m, ok := ModelByID(id)
	if !ok {
		return false
	}
	for _, a := range m.Artifacts {
		if !regularSize(r.artifactPath(a), a.Size) {
			return false
		}
	}
	return true
}
func (r *Runtime) ModelArtifactStatus(id string) ([]ArtifactStatus, int64) {
	m, ok := ModelByID(id)
	if !ok {
		return nil, 0
	}
	items := make([]ArtifactStatus, 0, len(m.Artifacts))
	var missing int64
	for _, a := range m.Artifacts {
		path := r.artifactPath(a)
		present := regularSize(path, a.Size)
		shared := present && a.SharedLLMFile != "" && path != r.modelPath(a)
		items = append(items, ArtifactStatus{Role: a.Role, Name: a.Name, Size: a.Size, Present: present, Shared: shared})
		if !present {
			missing += a.Size
		}
	}
	return items, missing
}
func (r *Runtime) RuntimePresent(backend string) bool {
	a, e := RuntimeArtifactFor(backend)
	if e != nil {
		return false
	}
	st, e := os.Stat(r.binaryPath(a))
	return e == nil && st.Mode().IsRegular()
}

// Devices asks the installed runtime for its own device names. This avoids
// trying to infer Vulkan/CUDA numbering from PCI data, which can disagree with
// the order exposed by ggml. Successful probes are cached because status is
// polled frequently by the settings page.
func (r *Runtime) Devices(backend string) ([]ComputeDevice, error) {
	a, err := RuntimeArtifactFor(backend)
	if err != nil {
		return nil, err
	}
	if !r.RuntimePresent(backend) {
		return nil, nil
	}
	key := ResolveBackend(backend)
	r.devicesMu.Lock()
	defer r.devicesMu.Unlock()
	if cached, ok := r.devices[key]; ok {
		return append([]ComputeDevice(nil), cached...), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.binaryPath(a), "--list-devices") //nolint:gosec // pinned server-controlled binary
	cmd.Dir = r.runtimeDir(a)
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+r.runtimeDir(a))
	out, err := cmd.Output()
	if err != nil {
		detail := ""
		if exitErr, ok := err.(*exec.ExitError); ok {
			detail = strings.TrimSpace(string(exitErr.Stderr))
		}
		if detail != "" {
			return nil, fmt.Errorf("imagegen: list devices: %w: %s", err, detail)
		}
		return nil, fmt.Errorf("imagegen: list devices: %w", err)
	}
	devices := parseDevices(out)
	if len(devices) == 0 {
		return nil, fmt.Errorf("imagegen: sd-cli reported no compute devices")
	}
	r.devices[key] = devices
	return append([]ComputeDevice(nil), devices...), nil
}

func parseDevices(out []byte) []ComputeDevice {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	devices := make([]ComputeDevice, 0, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		description := name
		if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
			description = strings.TrimSpace(parts[1])
		}
		devices = append(devices, ComputeDevice{Name: name, Description: description})
	}
	return devices
}
func (r *Runtime) DownloadStatus() (DownloadState, *ImageDownloadProgress, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p := r.progress
	if p.StartedAt.IsZero() {
		return r.state, nil, r.downloadErr
	}
	return r.state, &p, r.downloadErr
}

// Download is the only network-capable method in this package. Presence,
// status, catalog selection and Generate never fetch implicitly.
func (r *Runtime) Download(ctx context.Context, modelID, backend string) error {
	r.mu.Lock()
	if r.state == DownloadDownloading {
		r.mu.Unlock()
		return fmt.Errorf("imagegen: download already running")
	}
	r.state = DownloadDownloading
	r.downloadErr = ""
	r.mu.Unlock()
	m, ok := ModelByID(modelID)
	if !ok {
		return r.fail(fmt.Errorf("imagegen: unknown model %q", modelID))
	}
	a, err := RuntimeArtifactFor(backend)
	if err != nil {
		return r.fail(err)
	}
	total := int64(0)
	if !r.RuntimePresent(backend) {
		total += a.Size
	}
	for _, item := range m.Artifacts {
		if !regularSize(r.artifactPath(item), item.Size) {
			total += item.Size
		}
	}
	r.mu.Lock()
	r.progress = ImageDownloadProgress{BytesTotal: total, StartedAt: time.Now()}
	r.mu.Unlock()
	done := int64(0)
	if !r.RuntimePresent(backend) {
		archive := filepath.Join(r.dir, "runtime", a.Name)
		r.setProgress(a.Name, done)
		if err = downloadFile(ctx, a, archive, func(n int64) { r.setProgress(a.Name, done+n) }); err == nil {
			err = extractZipFlatten(archive, r.runtimeDir(a))
			_ = os.Remove(archive)
		}
		if err != nil {
			return r.fail(err)
		}
		done += a.Size
	}
	for _, item := range m.Artifacts {
		if regularSize(r.artifactPath(item), item.Size) {
			continue
		}
		r.setProgress(item.Name, done)
		if err = downloadFile(ctx, item.Artifact, r.modelPath(item), func(n int64) { r.setProgress(item.Name, done+n) }); err != nil {
			return r.fail(err)
		}
		done += item.Size
	}
	r.mu.Lock()
	r.state = DownloadReady
	r.progress.BytesDone = total
	r.progress.CurrentFile = ""
	r.mu.Unlock()
	return nil
}
func (r *Runtime) fail(err error) error {
	r.mu.Lock()
	r.state = DownloadFailed
	r.downloadErr = err.Error()
	r.mu.Unlock()
	return err
}
func (r *Runtime) setProgress(file string, n int64) {
	r.mu.Lock()
	r.progress.CurrentFile = file
	r.progress.BytesDone = n
	r.mu.Unlock()
}

func downloadFile(ctx context.Context, a Artifact, dest string, progress func(int64)) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	_ = os.Remove(tmp)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s from %s: %w", a.Name, a.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s from %s: HTTP %d", a.Name, a.URL, resp.StatusCode)
	}
	f, err := os.Create(tmp) //nolint:gosec // destination is built from the server-controlled data dir and pinned artifact name
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	buf := make([]byte, 4<<20)
	var n int64
	for {
		k, e := resp.Body.Read(buf)
		if k > 0 {
			n += int64(k)
			_, _ = h.Write(buf[:k])
			if _, err = f.Write(buf[:k]); err != nil {
				return err
			}
			progress(n)
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
	}
	if n != a.Size {
		return fmt.Errorf("size mismatch for %s: got %d want %d", a.Name, n, a.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
		return fmt.Errorf("sha256 mismatch for %s: got %s", a.Name, got)
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func extractZipFlatten(src, dest string) error {
	z, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer func() { _ = z.Close() }()
	tmp := dest + ".tmp"
	_ = os.RemoveAll(tmp)
	if err = os.MkdirAll(tmp, 0750); err != nil {
		return err
	}
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(f.Name)
		if base == "." || strings.Contains(base, "..") {
			continue
		}
		in, e := f.Open()
		if e != nil {
			return e
		}
		mode := os.FileMode(0640)
		if strings.HasSuffix(base, "sd-cli") || strings.HasSuffix(base, "sd-cli.exe") {
			mode = 0750
		}
		out, e := os.OpenFile(filepath.Join(tmp, base), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode) //nolint:gosec // base is sanitized above
		if e == nil {
			_, e = io.Copy(out, io.LimitReader(in, 4<<30)) //nolint:gosec // pinned checksum-verified runtime archive
		}
		_ = out.Close()
		_ = in.Close()
		if e != nil {
			return e
		}
	}
	name := "sd-cli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if _, err = os.Stat(filepath.Join(tmp, name)); err != nil {
		return fmt.Errorf("runtime archive contains no %s", name)
	}
	_ = os.RemoveAll(dest)
	return os.Rename(tmp, dest)
}

type Request struct {
	ModelID        string  `json:"model_id,omitempty"`
	Backend        string  `json:"backend,omitempty"`
	Device         string  `json:"device,omitempty" maxLength:"128"`
	MemoryMode     string  `json:"memory_mode,omitempty" enum:",auto,low_vram"`
	Prompt         string  `json:"prompt" minLength:"1" maxLength:"4000"`
	NegativePrompt string  `json:"negative_prompt,omitempty" maxLength:"2000"`
	Output         string  `json:"-"`
	Width          int     `json:"width,omitempty" minimum:"0" maximum:"2048"`
	Height         int     `json:"height,omitempty" minimum:"0" maximum:"2048"`
	Steps          int     `json:"steps,omitempty" minimum:"0" maximum:"50"`
	CFG            float64 `json:"cfg,omitempty" minimum:"0" maximum:"30"`
	Seed           int64   `json:"seed,omitempty"`
}
type Result struct {
	Path       string `json:"path"`
	Model      string `json:"model"`
	Seed       int64  `json:"seed"`
	DurationMs int64  `json:"duration_ms"`
}

func (r *Runtime) Generate(ctx context.Context, in Request) (Result, error) {
	r.generateMu.Lock()
	defer r.generateMu.Unlock()
	m, ok := ModelByID(in.ModelID)
	if !ok {
		return Result{}, fmt.Errorf("imagegen: unknown model %q", in.ModelID)
	}
	a, err := RuntimeArtifactFor(in.Backend)
	if err != nil {
		return Result{}, err
	}
	if !r.RuntimePresent(in.Backend) || !r.ModelPresent(in.ModelID) {
		return Result{}, fmt.Errorf("imagegen: runtime or model is not downloaded; run `heya image fetch` explicitly")
	}
	devices, err := r.Devices(in.Backend)
	if err != nil {
		return Result{}, err
	}
	memoryMode := in.MemoryMode
	if memoryMode == "" {
		memoryMode = m.DefaultMemoryMode
	}
	deviceArgs, err := generationDeviceArgs(in.Device, memoryMode, devices)
	if err != nil {
		return Result{}, err
	}
	if in.Width == 0 {
		in.Width = m.DefaultWidth
	}
	if in.Height == 0 {
		in.Height = m.DefaultHeight
	}
	if in.Steps == 0 {
		in.Steps = m.DefaultSteps
	}
	if in.CFG == 0 {
		in.CFG = m.DefaultCFG
	}
	if in.Seed == 0 {
		in.Seed = -1
	}
	if in.Output == "" {
		if err = os.MkdirAll(filepath.Join(r.dir, "output"), 0750); err != nil {
			return Result{}, err
		}
		in.Output = filepath.Join(r.dir, "output", fmt.Sprintf("image-%d.png", time.Now().UnixNano()))
	}
	if abs, e := filepath.Abs(in.Output); e == nil {
		in.Output = abs
	}
	args := []string{"-p", in.Prompt, "-o", in.Output, "-W", strconv.Itoa(in.Width), "-H", strconv.Itoa(in.Height), "--steps", strconv.Itoa(in.Steps), "--cfg-scale", strconv.FormatFloat(in.CFG, 'f', -1, 64), "--seed", strconv.FormatInt(in.Seed, 10), "--diffusion-fa", "--vae-tiling", "--vae-conv-direct"}
	args = append(args, deviceArgs...)
	for _, item := range m.Artifacts {
		flag := map[string]string{"model": "--model", "diffusion": "--diffusion-model", "llm": "--llm", "vae": "--vae"}[item.Role]
		if flag == "" {
			return Result{}, fmt.Errorf("imagegen: unsupported artifact role %q", item.Role)
		}
		args = append(args, flag, r.artifactPath(item))
	}
	if m.SamplingMethod != "" {
		args = append(args, "--sampling-method", m.SamplingMethod)
	}
	if m.Scheduler != "" {
		args = append(args, "--scheduler", m.Scheduler)
	}
	if m.FlowShift != 0 {
		args = append(args, "--flow-shift", strconv.FormatFloat(m.FlowShift, 'f', -1, 64))
	}
	if in.NegativePrompt != "" {
		args = append(args, "-n", in.NegativePrompt)
	}
	start := time.Now()
	cmd := exec.CommandContext(ctx, r.binaryPath(a), args...) //nolint:gosec // binary and model paths are pinned server-controlled artifacts
	cmd.Dir = r.runtimeDir(a)
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+r.runtimeDir(a))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{}, fmt.Errorf("imagegen: sd-cli: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if _, err = os.Stat(in.Output); err != nil {
		return Result{}, fmt.Errorf("imagegen: sd-cli produced no image: %w", err)
	}
	return Result{Path: in.Output, Model: m.ID, Seed: in.Seed, DurationMs: time.Since(start).Milliseconds()}, nil
}

const (
	MemoryModeAuto    = "auto"
	MemoryModeLowVRAM = "low_vram"
)

func generationDeviceArgs(requested, memoryMode string, devices []ComputeDevice) ([]string, error) {
	requested = strings.TrimSpace(requested)
	memoryMode = strings.TrimSpace(strings.ToLower(memoryMode))
	if memoryMode == "" {
		memoryMode = MemoryModeAuto
	}
	if memoryMode != MemoryModeAuto && memoryMode != MemoryModeLowVRAM {
		return nil, fmt.Errorf("imagegen: unknown memory mode %q (available: auto, low_vram)", memoryMode)
	}
	if (requested == "" || strings.EqualFold(requested, "auto")) && memoryMode == MemoryModeAuto {
		// The pinned runtime's auto-fit mode measures current free memory and
		// places/splits model components accordingly, falling back to CPU when a
		// GPU cannot safely hold a component and its compute reserve.
		return []string{"--auto-fit"}, nil
	}
	args := make([]string, 0, 6)
	if memoryMode == MemoryModeLowVRAM {
		// Keep parameters in system RAM and stage them to the selected compute
		// device as needed. Graph-cut streaming reserves 1 GiB of currently free
		// VRAM and segments graphs/layers to stay within the remainder. These are
		// stable-diffusion.cpp's native low-memory controls; they cannot be
		// combined with --auto-fit because auto-fit replaces parameter placement.
		args = append(args, "--offload-to-cpu", "--max-vram", "-1", "--stream-layers")
	}
	if requested == "" || strings.EqualFold(requested, "auto") {
		return args, nil
	}
	for _, device := range devices {
		if strings.EqualFold(requested, device.Name) {
			return append(args, "--backend", device.Name), nil
		}
	}
	available := make([]string, 0, len(devices))
	for _, device := range devices {
		available = append(available, device.Name)
	}
	return nil, fmt.Errorf("imagegen: unknown compute device %q (available: %s)", requested, strings.Join(available, ", "))
}

func (r *Runtime) OutputPath(name string) (string, bool) {
	if name == "" || filepath.Base(name) != name || !strings.HasSuffix(strings.ToLower(name), ".png") {
		return "", false
	}
	path := filepath.Join(r.dir, "output", name)
	st, err := os.Stat(path)
	return path, err == nil && st.Mode().IsRegular()
}
