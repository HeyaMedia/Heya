package transcoder

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// HwAccelProvider lazily resolves the active hardware-accel config.
//
// Why lazy: on macOS Tahoe (26.x), Apple's Network.framework registers a
// pthread_atfork child handler (nw_settings_child_has_forked) that infinite-
// loops in _os_log_preferences_refresh. tsnet pulls Network.framework in
// transitively. Any fork() while tsnet is loaded can spawn a child that
// wedges before reaching exec() — and inherits all parent FDs, including the
// :8080 listening socket, so it locks the port even after the parent exits.
//
// The single highest-volume forker in our binary was DetectHardwareAccel at
// service.New time: every air rebuild ran multiple `ffmpeg -version`-style
// probes. Moving probing out of startup (and caching the result to disk so
// subsequent boots never probe at all) eliminates that path entirely.
type HwAccelProvider struct {
	configured HwAccelType // "auto", "none", or an explicit type
	cacheFile  string

	mu       sync.Mutex
	resolved *HwAccelConfig
}

type hwAccelCache struct {
	Type       HwAccelType `json:"type"`
	DetectedAt time.Time   `json:"detected_at"`
}

// NewHwAccelProvider builds the provider but does NOT probe yet. The first
// call to Get() will:
//   - return the explicit type if cfg.HWAccel is anything other than "auto"
//   - return cached probe result if the cache file is present
//   - probe ffmpeg (the only fork path) and persist the result otherwise
//
// `dataDir` is the heya data dir; the cache lives at
// <dataDir>/transcoder/hwaccel.json.
func NewHwAccelProvider(dataDir, configured string) *HwAccelProvider {
	t := HwAccelType(configured)
	if t == "" {
		t = HwAccelAuto
	}
	return &HwAccelProvider{
		configured: t,
		cacheFile:  filepath.Join(dataDir, "transcoder", "hwaccel.json"),
	}
}

// Get resolves the config, probing exactly once per cache-file lifetime.
// Cheap and safe to call from any goroutine.
func (p *HwAccelProvider) Get() HwAccelConfig {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.resolved != nil {
		return *p.resolved
	}

	// Explicit override — no probe needed.
	if p.configured != HwAccelAuto {
		cfg := BuildHwAccelConfig(p.configured)
		p.resolved = &cfg
		log.Info().Str("hwaccel", string(p.configured)).Msg("hardware acceleration forced from config")
		return cfg
	}

	// Try the cache before forking. The cache is invalidated when ffmpeg
	// disappears from PATH (e.g., uninstall) — IsFFmpegAvailable uses
	// LookPath which never forks.
	if t, ok := p.readCache(); ok {
		if !IsFFmpegAvailable() {
			log.Warn().Msg("hwaccel cache hit but ffmpeg not on PATH; using software fallback")
			t = HwAccelNone
		} else {
			log.Info().Str("hwaccel", string(t)).Msg("hardware acceleration loaded from cache")
		}
		cfg := BuildHwAccelConfig(t)
		p.resolved = &cfg
		return cfg
	}

	// First-time probe. This is the only fork; happens once per data dir.
	t := probeHardwareAccel()
	p.writeCache(t)
	cfg := BuildHwAccelConfig(t)
	p.resolved = &cfg
	return cfg
}

// Reset clears the cached result (in-memory only — the file stays so
// concurrent probes don't race). Useful after the user re-installs ffmpeg
// or switches GPUs; callers can expose this behind a Settings UI later.
func (p *HwAccelProvider) Reset() {
	p.mu.Lock()
	p.resolved = nil
	p.mu.Unlock()
}

// Configure changes the mode used by future transcode sessions and clears the
// in-memory resolution. Existing sessions retain the HwAccelConfig copied into
// their options and can finish undisturbed.
func (p *HwAccelProvider) Configure(configured string) {
	t := HwAccelType(configured)
	if t == "" {
		t = HwAccelAuto
	}
	p.mu.Lock()
	p.configured = t
	p.resolved = nil
	p.mu.Unlock()
}

func (p *HwAccelProvider) readCache() (HwAccelType, bool) {
	data, err := os.ReadFile(p.cacheFile)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Debug().Err(err).Str("path", p.cacheFile).Msg("hwaccel cache read failed")
		}
		return "", false
	}
	var c hwAccelCache
	if err := json.Unmarshal(data, &c); err != nil {
		log.Warn().Err(err).Str("path", p.cacheFile).Msg("hwaccel cache parse failed; will re-probe")
		return "", false
	}
	if c.Type == "" {
		return "", false
	}
	return c.Type, true
}

func (p *HwAccelProvider) writeCache(t HwAccelType) {
	if err := os.MkdirAll(filepath.Dir(p.cacheFile), 0o750); err != nil {
		log.Warn().Err(err).Msg("hwaccel cache dir create failed")
		return
	}
	data, err := json.MarshalIndent(hwAccelCache{
		Type:       t,
		DetectedAt: time.Now().UTC(),
	}, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(p.cacheFile, data, 0o600); err != nil {
		log.Warn().Err(err).Str("path", p.cacheFile).Msg("hwaccel cache write failed")
	}
}
