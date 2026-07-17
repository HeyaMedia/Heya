package llm

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// LocalRuntime owns the managed llama-server subprocess: artifact downloads,
// on-demand spawn, health gating, idle shutdown, and crash containment. It is
// deliberately a *subprocess* (not in-process bindings): a ggml OOM or
// segfault on a low-power box must never take Heya down, and killing the
// process is the cheapest possible "unload model, reclaim RAM".
//
// The design keeps the runtime an implementation detail behind the same
// OpenAI-compatible Client used for external providers — if we ever switch to
// purego bindings, only this file changes.
type LocalRuntime struct {
	dir    string          // <dataDir>/llm
	appCtx context.Context // app lifetime; bound via Bind() post-construction

	mu       sync.Mutex
	cmd      *exec.Cmd
	cancel   context.CancelFunc
	baseURL  string
	modelID  string
	ctxSize  int
	procDone chan struct{}

	lastUsed atomic.Int64 // unix seconds

	// pubRunning/pubModel mirror the running-state under mu for lock-free
	// polling: Running() backs the Settings status endpoint, which is polled
	// exactly while Ensure holds mu for a whole spawn + health wait (up to
	// minutes on a cold model load) — taking mu there froze the status page
	// at the moment the user wants to watch progress.
	pubRunning atomic.Bool
	pubModel   atomic.Pointer[string]

	dlBusy     atomic.Bool
	dlState    atomic.Pointer[DownloadState]
	dlProgress atomic.Pointer[DownloadProgress]
	dlErr      atomic.Pointer[string]

	// idleTimeout is how long the server may sit unused before being killed
	// to reclaim RAM. The next Ensure() respawns it (model reload takes a few
	// seconds — acceptable for a daily curator job or sporadic chat).
	idleTimeout time.Duration

	// healthTimeout bounds model load at spawn. Big GGUFs on spinning rust
	// take a while; 3 minutes is generous without masking a hung process.
	healthTimeout time.Duration
}

// NewLocalRuntime creates the runtime rooted at dir (conventionally
// <dataDir>/llm). Call Bind() with the app lifetime context before use.
func NewLocalRuntime(dir string) *LocalRuntime {
	idle := DownloadIdle
	r := &LocalRuntime{
		dir:           dir,
		appCtx:        context.Background(),
		idleTimeout:   10 * time.Minute,
		healthTimeout: 3 * time.Minute,
	}
	r.dlState.Store(&idle)
	return r
}

// Bind attaches the app lifetime context; a graceful shutdown kills the
// subprocess and cancels in-flight downloads.
func (r *LocalRuntime) Bind(ctx context.Context) { r.appCtx = ctx }

// --- paths & presence ------------------------------------------------------

func (r *LocalRuntime) serverDir(backend string) (string, error) {
	asset, err := ServerAssetFor(backend)
	if err != nil {
		return "", err
	}
	return filepath.Join(r.dir, "server", strings.TrimSuffix(asset.Name, ".tar.gz")), nil
}

func (r *LocalRuntime) serverBinary(backend string) (string, error) {
	dir, err := r.serverDir(backend)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "llama-server"), nil
}

func (r *LocalRuntime) modelPath(m LocalModel) string {
	return filepath.Join(r.dir, "models", m.File)
}

// ServerPresent reports whether the pinned llama-server build is installed.
func (r *LocalRuntime) ServerPresent(backend string) bool {
	bin, err := r.serverBinary(backend)
	if err != nil {
		return false
	}
	st, err := os.Stat(bin)
	return err == nil && st.Mode().IsRegular()
}

// ModelPresent reports whether the catalog model's GGUF is fully downloaded.
func (r *LocalRuntime) ModelPresent(modelID string) bool {
	m, ok := LocalModelByID(modelID)
	if !ok {
		return false
	}
	return fileSizeMatches(r.modelPath(m), m.Size)
}

// --- download ---------------------------------------------------------------

// Download fetches whatever is missing for (modelID, backend): the server
// bundle and/or the GGUF. Blocking; callers that want fire-and-forget run it
// on the app lifetime context in a goroutine. Concurrent calls coalesce into
// an error for the loser — the status endpoint reports progress either way.
func (r *LocalRuntime) Download(ctx context.Context, modelID, backend string) error {
	if !r.dlBusy.CompareAndSwap(false, true) {
		return fmt.Errorf("llm: a download is already running")
	}
	defer r.dlBusy.Store(false)

	model, ok := LocalModelByID(modelID)
	if !ok {
		return fmt.Errorf("llm: unknown local model %q", modelID)
	}
	asset, err := ServerAssetFor(backend)
	if err != nil {
		return err
	}

	type item struct {
		label string
		size  int64
		fetch func(onProgress func(int64)) error
	}
	var items []item

	if !r.ServerPresent(backend) {
		serverDest, dirErr := r.serverDir(backend)
		if dirErr != nil {
			return dirErr
		}
		items = append(items, item{
			label: asset.Name,
			size:  asset.Size,
			fetch: func(onProgress func(int64)) error {
				archive := filepath.Join(r.dir, "server", asset.Name)
				if err := fetchFile(ctx, asset.URL, asset.SHA256, archive, onProgress); err != nil {
					return err
				}
				defer func() { _ = os.Remove(archive) }()
				return extractServerBundle(archive, serverDest)
			},
		})
	}
	if !r.ModelPresent(modelID) {
		items = append(items, item{
			label: model.File,
			size:  model.Size,
			fetch: func(onProgress func(int64)) error {
				return fetchFile(ctx, model.URL, model.SHA256, r.modelPath(model), onProgress)
			},
		})
	}

	if len(items) == 0 {
		ready := DownloadReady
		r.dlState.Store(&ready)
		return nil
	}

	var total int64
	for _, it := range items {
		total += it.size
	}
	prog := &DownloadProgress{BytesTotal: total, StartedAt: time.Now()}
	r.dlProgress.Store(prog)
	downloading := DownloadDownloading
	r.dlState.Store(&downloading)
	r.dlErr.Store(nil)
	log.Info().Int("files", len(items)).Int64("bytes", total).Msg("llm: downloading local runtime artifacts")

	var doneBytes int64
	for _, it := range items {
		next := *prog
		next.CurrentFile = it.label
		next.BytesDone = doneBytes
		r.dlProgress.Store(&next)

		base := doneBytes
		if err := it.fetch(func(cum int64) {
			p := *prog
			p.CurrentFile = it.label
			p.BytesDone = base + cum
			r.dlProgress.Store(&p)
		}); err != nil {
			failed := DownloadFailed
			msg := err.Error()
			r.dlState.Store(&failed)
			r.dlErr.Store(&msg)
			log.Err(err).Str("file", it.label).Msg("llm: artifact download failed")
			return fmt.Errorf("llm: download %s: %w", it.label, err)
		}
		doneBytes += it.size
		log.Info().Str("file", it.label).Msg("llm: fetched")
	}

	final := *prog
	final.CurrentFile = ""
	final.BytesDone = total
	r.dlProgress.Store(&final)
	ready := DownloadReady
	r.dlState.Store(&ready)
	log.Info().Msg("llm: local runtime artifacts ready")
	return nil
}

// --- lifecycle ---------------------------------------------------------------

// Ensure returns the base URL of a healthy llama-server for (modelID,
// ctxSize), spawning or respawning as needed. Callers should treat the URL as
// valid for the duration of one request — a later call may return a new one.
func (r *LocalRuntime) Ensure(ctx context.Context, modelID, backend string, ctxSize int) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running() && r.modelID == modelID && r.ctxSize == ctxSize {
		r.Touch()
		return r.baseURL, nil
	}
	r.stopLocked()

	model, ok := LocalModelByID(modelID)
	if !ok {
		return "", fmt.Errorf("llm: unknown local model %q", modelID)
	}
	bin, err := r.serverBinary(backend)
	if err != nil {
		return "", err
	}
	if !r.ServerPresent(backend) || !r.ModelPresent(modelID) {
		return "", fmt.Errorf("llm: local runtime not downloaded yet (server or model missing) — trigger a download first")
	}

	port, err := freePort()
	if err != nil {
		return "", fmt.Errorf("llm: allocate port: %w", err)
	}

	args := []string{
		"--model", r.modelPath(model),
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"--ctx-size", strconv.Itoa(ctxSize),
		"--jinja",               // use the GGUF's embedded chat template
		"--no-webui",            // API only; Heya is the UI
		"--n-gpu-layers", "999", // offload everything the backend supports; no-op on CPU builds
	}
	args = append(args, model.extraArgs...)

	procCtx, cancel := context.WithCancel(r.appCtx)
	cmd := exec.CommandContext(procCtx, bin, args...) //nolint:gosec // G204: binary + args are server-controlled (pinned artifact paths)
	if runtime.GOOS == "linux" {
		// The bundles carry rpath=$ORIGIN, but belt-and-braces for the shared
		// ggml/backend libs co-located next to the binary.
		cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+filepath.Dir(bin))
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return "", err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return "", fmt.Errorf("llm: start llama-server: %w", err)
	}

	tail := newLogTail(40)
	var pipesDone sync.WaitGroup
	pipesDone.Add(2)
	go func() { defer pipesDone.Done(); pipeToLog(stderr, tail) }()
	go func() { defer pipesDone.Done(); pipeToLog(stdout, tail) }()

	procDone := make(chan struct{})
	go func() {
		// os/exec contract: Wait closes the pipes, so it must not run until
		// both pipe readers hit EOF — racing them loses the final log lines,
		// which are exactly what waitHealthy's error message surfaces.
		pipesDone.Wait()
		err := cmd.Wait()
		close(procDone)
		r.mu.Lock()
		if r.procDone == procDone { // still the current process
			r.cmd = nil
			r.baseURL = ""
			r.pubRunning.Store(false)
		}
		r.mu.Unlock()
		if err != nil && procCtx.Err() == nil {
			log.Warn().Err(err).Msg("llm: llama-server exited unexpectedly")
		}
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	if err := waitHealthy(ctx, procCtx, healthURL, procDone, r.healthTimeout); err != nil {
		cancel()
		<-procDone
		return "", fmt.Errorf("llm: llama-server failed to become healthy: %w (last log: %s)", err, tail.String())
	}

	r.cmd = cmd
	r.cancel = cancel
	r.baseURL = baseURL
	r.modelID = modelID
	r.ctxSize = ctxSize
	r.procDone = procDone
	r.pubRunning.Store(true)
	r.pubModel.Store(&modelID)
	r.Touch()
	go r.idleReaper(procDone)

	log.Info().Str("model", modelID).Int("port", port).Int("ctx", ctxSize).Msg("llm: llama-server ready")
	return baseURL, nil
}

// Touch marks the runtime as recently used, deferring the idle reaper.
func (r *LocalRuntime) Touch() { r.lastUsed.Store(time.Now().Unix()) }

// Stop kills the subprocess if running. Safe to call at any time.
func (r *LocalRuntime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
}

func (r *LocalRuntime) stopLocked() {
	r.pubRunning.Store(false)
	r.pubModel.Store(nil)
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.procDone != nil {
		<-r.procDone
		r.procDone = nil
	}
	r.cmd = nil
	r.baseURL = ""
	r.modelID = ""
}

func (r *LocalRuntime) running() bool {
	if r.cmd == nil || r.procDone == nil {
		return false
	}
	select {
	case <-r.procDone:
		return false
	default:
		return true
	}
}

// Running reports whether a llama-server subprocess is currently serving, and
// which catalog model it has loaded. Reads only the published atomics — never
// r.mu — so status polls stay responsive while Ensure holds the lock through
// a multi-minute model load.
func (r *LocalRuntime) Running() (bool, string) {
	if !r.pubRunning.Load() {
		return false, ""
	}
	model := ""
	if m := r.pubModel.Load(); m != nil {
		model = *m
	}
	return true, model
}

func (r *LocalRuntime) idleReaper(procDone chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-procDone:
			return
		case <-r.appCtx.Done():
			return
		case <-ticker.C:
			idleFor := time.Since(time.Unix(r.lastUsed.Load(), 0))
			if idleFor >= r.idleTimeout {
				log.Info().Dur("idle", idleFor).Msg("llm: idle timeout — stopping llama-server to reclaim RAM")
				r.Stop()
				return
			}
		}
	}
}

// DownloadStatus returns the current artifact-download snapshot.
func (r *LocalRuntime) DownloadStatus() (DownloadState, *DownloadProgress, string) {
	state := DownloadIdle
	if s := r.dlState.Load(); s != nil {
		state = *s
	}
	errMsg := ""
	if e := r.dlErr.Load(); e != nil {
		errMsg = *e
	}
	return state, r.dlProgress.Load(), errMsg
}

// --- helpers -----------------------------------------------------------------

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port, nil
}

// waitHealthy polls /health until 200, the process dies, or the deadline
// passes. llama-server answers 503 while the model streams into memory.
func waitHealthy(reqCtx, procCtx context.Context, url string, procDone chan struct{}, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		select {
		case <-procDone:
			return fmt.Errorf("process exited during startup")
		case <-reqCtx.Done():
			return reqCtx.Err()
		case <-procCtx.Done():
			return procCtx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s", timeout)
		}
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			continue // not listening yet
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
	}
}

// logTail keeps the last N lines of subprocess output for error reporting.
type logTail struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func newLogTail(max int) *logTail { return &logTail{max: max} }

func (t *logTail) add(line string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lines = append(t.lines, line)
	if len(t.lines) > t.max {
		t.lines = t.lines[len(t.lines)-t.max:]
	}
}

func (t *logTail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.lines) == 0 {
		return "(no output)"
	}
	n := len(t.lines)
	if n > 5 {
		n = 5
	}
	return strings.Join(t.lines[len(t.lines)-n:], " | ")
}

// pipeToLog drains a subprocess pipe into the debug log + tail buffer.
// Scanner buffer is enlarged per the house rule — default 64 KiB caps
// silently deadlock child pipes on long lines.
func pipeToLog(pipe interface{ Read([]byte) (int, error) }, tail *logTail) {
	s := bufio.NewScanner(pipe)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	for s.Scan() {
		line := s.Text()
		tail.add(line)
		log.Debug().Str("proc", "llama-server").Msg(line)
	}
}
