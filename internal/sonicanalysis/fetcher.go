package sonicanalysis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/karbowiak/heya/internal/artifactdownload"
	"github.com/rs/zerolog/log"
)

// FetcherState is the lifecycle of a ModelFetcher. Stored atomically
// so HTTP status endpoints can poll without locking.
type FetcherState int32

const (
	FetcherIdle FetcherState = iota
	FetcherChecking
	FetcherFetching
	FetcherReady
	FetcherFailed
)

func (s FetcherState) String() string {
	switch s {
	case FetcherIdle:
		return "idle"
	case FetcherChecking:
		return "checking"
	case FetcherFetching:
		return "fetching"
	case FetcherReady:
		return "ready"
	case FetcherFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// ModelFile describes one file in the fetcher manifest. SHA256 is
// optional; when set, the fetcher refuses to accept downloads that
// don't match.
type ModelFile struct {
	Name   string // path relative to ModelsDir
	URL    string // absolute download URL
	SHA256 string // hex-encoded; "" disables integrity check
	Size   int64  // approximate bytes for progress UI; not an exact-size assertion
	// MaxBytes optionally supplies a hard download cap. Zero derives a
	// conservative cap from Size; use this for files whose expected size is
	// absent or whose upstream is known to vary beyond the default tolerance.
	MaxBytes int64
}

const (
	modelFetchTimeout    = 30 * time.Minute
	modelSizeMinHeadroom = 8 << 20
	modelSizeMaxHeadroom = 256 << 20
	unknownModelMaxBytes = 2 << 30
)

// FetchProgress is a snapshot of the in-flight download state, safe
// to read concurrently.
type FetchProgress struct {
	CurrentFile string
	BytesDone   int64
	BytesTotal  int64
	FilesDone   int
	FilesTotal  int
	StartedAt   time.Time
}

// ModelFetcher downloads ML model files into ModelsDir at server
// startup. Missing files are downloaded; existing files with a
// matching SHA256 (or non-zero size when SHA256 is unset) are kept.
type ModelFetcher struct {
	targetDir string
	urlBase   string
	manifest  []ModelFile

	state    atomic.Int32
	progress atomic.Pointer[FetchProgress]
	lastErr  atomic.Pointer[error]
	client   *http.Client
}

// NewModelFetcher constructs a fetcher pointing at the given target
// directory. URLBase is unused right now (the manifest carries fully-
// qualified URLs) but reserved for future mirror support.
func NewModelFetcher(targetDir, urlBase string) *ModelFetcher {
	return NewModelFetcherWithManifest(targetDir, urlBase, DefaultManifest())
}

// NewModelFetcherWithManifest is NewModelFetcher with a caller-supplied manifest,
// for model sets other than sonic-analysis (e.g. the recommendation text-embedder).
func NewModelFetcherWithManifest(targetDir, urlBase string, manifest []ModelFile) *ModelFetcher {
	return &ModelFetcher{
		targetDir: targetDir,
		urlBase:   urlBase,
		manifest:  manifest,
		client:    artifactdownload.NewClient(modelFetchTimeout),
	}
}

// State returns the fetcher's current lifecycle state.
func (f *ModelFetcher) State() FetcherState {
	return FetcherState(f.state.Load())
}

// Progress returns the latest progress snapshot. Returns nil before
// the first byte is fetched.
func (f *ModelFetcher) Progress() *FetchProgress {
	return f.progress.Load()
}

// LastError returns the most recent error, or nil.
func (f *ModelFetcher) LastError() error {
	p := f.lastErr.Load()
	if p == nil {
		return nil
	}
	return *p
}

// AllPresent walks the manifest once and reports true only if every
// file exists locally. Doesn't update state; safe to call at any
// time.
func (f *ModelFetcher) AllPresent() bool {
	for _, m := range f.manifest {
		path := filepath.Join(f.targetDir, m.Name)
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			return false
		}
	}
	return true
}

// ManifestFileStatus is the per-file status snapshot returned by
// Manifest(). Suitable for serialization to the admin UI.
type ManifestFileStatus struct {
	Name         string `json:"name"`
	Present      bool   `json:"present"`
	ExpectedSize int64  `json:"expected_size"`
	ActualSize   int64  `json:"actual_size"`
	Category     string `json:"category"`
}

// Manifest returns the per-file presence + size status, computed on
// the fly. Categorises each file so the UI can group rows: discogs,
// effnet_base, head, clap, clap_aux.
func (f *ModelFetcher) Manifest() []ManifestFileStatus {
	out := make([]ManifestFileStatus, 0, len(f.manifest))
	for _, m := range f.manifest {
		path := filepath.Join(f.targetDir, m.Name)
		st, err := os.Stat(path)
		status := ManifestFileStatus{
			Name:         m.Name,
			ExpectedSize: m.Size,
			Category:     classifyModelFile(m.Name),
		}
		if err == nil && !st.IsDir() {
			status.Present = true
			status.ActualSize = st.Size()
		}
		out = append(out, status)
	}
	return out
}

// MissingCount returns how many manifest entries aren't present on
// disk. Useful for "X of Y models missing" status copy.
func (f *ModelFetcher) MissingCount() int {
	missing := 0
	for _, m := range f.manifest {
		path := filepath.Join(f.targetDir, m.Name)
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			missing++
		}
	}
	return missing
}

// TotalSize sums the manifest's expected sizes (for "X / Y MB" copy).
func (f *ModelFetcher) TotalSize() int64 {
	var total int64
	for _, m := range f.manifest {
		total += m.Size
	}
	return total
}

func classifyModelFile(name string) string {
	switch {
	case strings.HasPrefix(name, "discogs_track_") ||
		strings.HasPrefix(name, "discogs_artist_") ||
		strings.HasPrefix(name, "discogs_release_"):
		return "discogs"
	case strings.HasPrefix(name, "discogs-effnet-bsdynamic"):
		return "effnet_base"
	case strings.HasPrefix(name, "heads/"):
		return "head"
	case strings.HasPrefix(name, "clap/") && strings.HasSuffix(name, ".onnx"):
		return "clap"
	case strings.HasPrefix(name, "clap/"):
		return "clap_aux"
	default:
		return "other"
	}
}

// Run downloads missing manifest files into targetDir. Idempotent:
// files that already exist + pass verification are skipped. Called
// in a background goroutine at server startup; tracks state via
// State()/Progress()/LastError().
func (f *ModelFetcher) Run(ctx context.Context) error {
	for {
		state := f.State()
		switch state {
		case FetcherIdle, FetcherReady, FetcherFailed:
			if !f.state.CompareAndSwap(int32(state), int32(FetcherChecking)) {
				continue
			}
		default:
			return fmt.Errorf("sonicanalysis: fetcher already running (state=%s)", state)
		}
		break
	}
	f.progress.Store(nil)
	f.lastErr.Store(nil)

	if err := os.MkdirAll(f.targetDir, 0o750); err != nil {
		f.fail(fmt.Errorf("mkdir target: %w", err))
		return err
	}

	// First pass: figure out what's missing.
	missing := make([]int, 0, len(f.manifest))
	var totalBytes int64
	for i, m := range f.manifest {
		path := filepath.Join(f.targetDir, m.Name)
		ok, err := verifyFile(path, m)
		if err != nil {
			log.Warn().Err(err).Str("file", m.Name).Msg("sonicanalysis: existing model failed verification, re-fetching")
			ok = false
		}
		if !ok {
			missing = append(missing, i)
			totalBytes += m.Size
		}
	}
	if len(missing) == 0 {
		f.state.Store(int32(FetcherReady))
		log.Info().Int("files", len(f.manifest)).Msg("sonicanalysis: all models already present")
		return nil
	}
	log.Info().
		Int("missing", len(missing)).
		Int64("bytes", totalBytes).
		Msg("sonicanalysis: fetching missing models")

	prog := &FetchProgress{
		FilesTotal: len(missing),
		BytesTotal: totalBytes,
		StartedAt:  time.Now(),
	}
	f.progress.Store(prog)
	f.state.Store(int32(FetcherFetching))

	for done, idx := range missing {
		m := f.manifest[idx]
		path := filepath.Join(f.targetDir, m.Name)
		next := *prog
		next.CurrentFile = m.Name
		next.FilesDone = done
		f.progress.Store(&next)

		if err := f.fetchOne(ctx, m, path); err != nil {
			f.fail(fmt.Errorf("fetch %s: %w", m.Name, err))
			return err
		}
		log.Info().Str("file", m.Name).Msg("sonicanalysis: fetched")
	}

	finished := *prog
	finished.CurrentFile = ""
	finished.FilesDone = len(missing)
	finished.BytesDone = totalBytes
	f.progress.Store(&finished)
	f.state.Store(int32(FetcherReady))
	log.Info().Int("files", len(missing)).Msg("sonicanalysis: model fetch complete")
	return nil
}

// fail records the error and transitions to FetcherFailed.
func (f *ModelFetcher) fail(err error) {
	f.lastErr.Store(&err)
	f.state.Store(int32(FetcherFailed))
	log.Err(err).Msg("sonicanalysis: model fetch failed")
}

// fetchOne downloads one manifest file to a uniquely named temporary sibling
// and atomically renames on success. The unique name matters when the API and
// worker processes share a model directory and both start the same download.
// Verifies SHA256 before renaming.
func (f *ModelFetcher) fetchOne(ctx context.Context, m ModelFile, finalPath string) error {
	written, err := artifactdownload.Fetch(ctx, f.client, artifactdownload.Spec{
		URL:         m.URL,
		Destination: finalPath,
		MaxBytes:    modelDownloadLimit(m),
		SHA256:      m.SHA256,
		Mode:        0o640,
	})
	if err != nil {
		// Close the publication race between processes. A peer's valid final
		// artifact means this fetch succeeded; our unique temporary is gone.
		if ok, verifyErr := verifyFile(finalPath, m); verifyErr == nil && ok {
			return nil
		}
		return err
	}

	// Progress accounting: bump BytesDone by this file's bytes.
	if p := f.progress.Load(); p != nil {
		next := *p
		next.BytesDone += written
		f.progress.Store(&next)
	}

	return nil
}

// modelDownloadLimit turns the manifest's approximate progress size into a
// hard safety bound. Upstreams currently do not publish stable checksums for
// the sonic catalog and may revise files in place, so exact-size enforcement
// would reject legitimate updates. The derived cap allows 50% headroom, clamped
// to 8-256 MiB, and never permits an undeclared artifact above 2 GiB. A
// manifest may set MaxBytes when it needs a different explicit ceiling.
func modelDownloadLimit(model ModelFile) int64 {
	if model.MaxBytes > 0 {
		return model.MaxBytes
	}
	if model.Size <= 0 || model.Size >= unknownModelMaxBytes {
		return unknownModelMaxBytes
	}
	headroom := model.Size / 2
	if headroom < modelSizeMinHeadroom {
		headroom = modelSizeMinHeadroom
	}
	if headroom > modelSizeMaxHeadroom {
		headroom = modelSizeMaxHeadroom
	}
	if model.Size > unknownModelMaxBytes-headroom {
		return unknownModelMaxBytes
	}
	return model.Size + headroom
}

// verifyFile reports whether `path` exists and matches the manifest
// entry. When SHA256 is set, the file is hashed. Otherwise we trust
// the declared Size (or any non-empty file if Size is 0).
func verifyFile(path string, m ModelFile) (bool, error) {
	st, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if st.IsDir() {
		return false, nil
	}
	if m.SHA256 == "" {
		// No hash configured. Size sanity check: anything with a
		// non-zero size is good enough.
		if st.Size() == 0 {
			return false, nil
		}
		if m.Size > 0 && st.Size() < m.Size/2 {
			return false, fmt.Errorf("size %d well below expected %d", st.Size(), m.Size)
		}
		return true, nil
	}
	// G304: same as os.Create above — path is built from server-controlled
	// targetDir + the model's declared filename.
	f, err := os.Open(path) //nolint:gosec // G304: server-built path
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return false, err
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	return got == m.SHA256, nil
}

// DefaultManifest returns the canonical list of model files Heya's
// sonic-analysis pipeline depends on. URLs are the upstream sources
// validated during the PoC. SHA256s aren't pinned yet — the upstream
// hosts don't publish hashes alongside the files, and copying our
// own would risk diverging from upstream silently when they update.
// We rely on the size sanity check + the runtime "does this model
// load" assertion in the Analyzer.
func DefaultManifest() []ModelFile {
	const essentiaEffNet = "https://essentia.upf.edu/models/feature-extractors/discogs-effnet/"
	const essentiaHeads = "https://essentia.upf.edu/models/classification-heads/"
	const clapBase = "https://huggingface.co/Xenova/clap-htsat-unfused/resolve/main/"

	return []ModelFile{
		// Discogs specialized embedding heads (fixed batch 64).
		{Name: "discogs_track_embeddings-effnet-bs64-1.onnx", URL: essentiaEffNet + "discogs_track_embeddings-effnet-bs64-1.onnx", Size: 19_000_000},
		{Name: "discogs_artist_embeddings-effnet-bs64-1.onnx", URL: essentiaEffNet + "discogs_artist_embeddings-effnet-bs64-1.onnx", Size: 19_000_000},
		{Name: "discogs_release_embeddings-effnet-bs64-1.onnx", URL: essentiaEffNet + "discogs_release_embeddings-effnet-bs64-1.onnx", Size: 19_000_000},

		// Base EffNet (dynamic batch) → genre softmax + 1280-dim embeddings.
		{Name: "discogs-effnet-bsdynamic-1.onnx", URL: essentiaEffNet + "discogs-effnet-bsdynamic-1.onnx", Size: 18_000_000},

		// 9× classifier heads.
		{Name: "heads/danceability-discogs-effnet-1.onnx", URL: essentiaHeads + "danceability/danceability-discogs-effnet-1.onnx", Size: 514_000},
		{Name: "heads/voice_instrumental-discogs-effnet-1.onnx", URL: essentiaHeads + "voice_instrumental/voice_instrumental-discogs-effnet-1.onnx", Size: 514_000},
		{Name: "heads/mood_happy-discogs-effnet-1.onnx", URL: essentiaHeads + "mood_happy/mood_happy-discogs-effnet-1.onnx", Size: 514_000},
		{Name: "heads/mood_sad-discogs-effnet-1.onnx", URL: essentiaHeads + "mood_sad/mood_sad-discogs-effnet-1.onnx", Size: 514_000},
		{Name: "heads/mood_aggressive-discogs-effnet-1.onnx", URL: essentiaHeads + "mood_aggressive/mood_aggressive-discogs-effnet-1.onnx", Size: 514_000},
		{Name: "heads/mood_relaxed-discogs-effnet-1.onnx", URL: essentiaHeads + "mood_relaxed/mood_relaxed-discogs-effnet-1.onnx", Size: 514_000},
		{Name: "heads/mood_party-discogs-effnet-1.onnx", URL: essentiaHeads + "mood_party/mood_party-discogs-effnet-1.onnx", Size: 514_000},
		{Name: "heads/mood_electronic-discogs-effnet-1.onnx", URL: essentiaHeads + "mood_electronic/mood_electronic-discogs-effnet-1.onnx", Size: 514_000},
		{Name: "heads/mood_acoustic-discogs-effnet-1.onnx", URL: essentiaHeads + "mood_acoustic/mood_acoustic-discogs-effnet-1.onnx", Size: 514_000},

		// CLAP HTSAT (audio + text).
		{Name: "clap/audio_model.onnx", URL: clapBase + "onnx/audio_model.onnx", Size: 118_000_000},
		{Name: "clap/text_model.onnx", URL: clapBase + "onnx/text_model.onnx", Size: 502_000_000},
		{Name: "clap/tokenizer.json", URL: clapBase + "tokenizer.json", Size: 2_100_000},
		{Name: "clap/merges.txt", URL: clapBase + "merges.txt", Size: 456_000},
		{Name: "clap/vocab.json", URL: clapBase + "vocab.json", Size: 798_000},
		{Name: "clap/special_tokens_map.json", URL: clapBase + "special_tokens_map.json", Size: 280},
	}
}
