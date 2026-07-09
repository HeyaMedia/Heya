package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/karbowiak/heya/internal/textembed"
)

// The optional embedding recommendation engine. Gated exactly like sonic-
// analysis: a master env/DB switch, off by default, that on enable downloads a
// text-embedding model and lights up the semantic-search + embedding-scorer
// paths. The non-ML engine (foryou.go) always works regardless.

const (
	recMLEnvEnabled     = "HEYA_RECOMMENDATIONS_ML_ENABLED"
	recMLEnvAccelerator = "HEYA_RECOMMENDATIONS_ML_ACCELERATOR"
	recMLSettingsKey    = "recommendations_ml"
)

// recommendationsMLManifest is BGE-large-en-v1.5 (quantized ONNX) + its
// WordPiece tokenizer, pulled from the Xenova HF mirror when enabled.
func recommendationsMLManifest() []sonicanalysis.ModelFile {
	const base = "https://huggingface.co/Xenova/bge-large-en-v1.5/resolve/main/"
	return []sonicanalysis.ModelFile{
		{Name: textembed.ModelFile, URL: base + "onnx/" + textembed.ModelFile, Size: 336_983_162},
		{Name: textembed.TokenizerFile, URL: base + textembed.TokenizerFile, Size: 711_396},
	}
}

// RecommendationsMLSettings is the user-tunable part of the embedding engine,
// stored as one JSON blob in system_settings. Enabled defaults false so a fresh
// install never downloads the ~340 MB model; flipping it on kicks a fetch.
type RecommendationsMLSettings struct {
	Enabled     bool   `json:"enabled"`
	Accelerator string `json:"accelerator"` // auto|cpu|coreml|cuda|openvino|directml
}

func DefaultRecommendationsMLSettings() RecommendationsMLSettings {
	return RecommendationsMLSettings{Enabled: false, Accelerator: "auto"}
}

func recMLEnabledFromEnv() (bool, bool) {
	v, ok := os.LookupEnv(recMLEnvEnabled)
	if !ok {
		return false, false
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return false, false
	}
	return b, true
}

func recMLAcceleratorFromEnv() (string, bool) {
	v, ok := os.LookupEnv(recMLEnvAccelerator)
	if !ok {
		return "", false
	}
	if v = strings.TrimSpace(v); v == "" {
		return "", false
	}
	return v, true
}

// RecommendationsMLEnvLock reports which fields are env-locked (for the UI +
// to translate locked writes to ErrFieldLockedByEnv).
func (a *App) RecommendationsMLEnvLock() (enabledVar, acceleratorVar string) {
	if _, ok := recMLEnabledFromEnv(); ok {
		enabledVar = recMLEnvEnabled
	}
	if _, ok := recMLAcceleratorFromEnv(); ok {
		acceleratorVar = recMLEnvAccelerator
	}
	return
}

// RecommendationsMLSettings reads persisted settings; env-sourced fields overlay
// the DB blob (env wins).
func (a *App) RecommendationsMLSettings(ctx context.Context) RecommendationsMLSettings {
	s := a.recMLSettingsFromDB(ctx)
	if v, ok := recMLEnabledFromEnv(); ok {
		s.Enabled = v
	}
	if v, ok := recMLAcceleratorFromEnv(); ok {
		s.Accelerator = v
	}
	return s
}

func (a *App) recMLSettingsFromDB(ctx context.Context) RecommendationsMLSettings {
	s := DefaultRecommendationsMLSettings()
	if raw, err := a.GetSystemSetting(ctx, recMLSettingsKey); err == nil {
		var p RecommendationsMLSettings
		if json.Unmarshal(raw, &p) == nil {
			s = p
			if s.Accelerator == "" {
				s.Accelerator = DefaultRecommendationsMLSettings().Accelerator
			}
		}
	}
	return s
}

// RecommendationsMLEnabled is a cheap boolean accessor.
func (a *App) RecommendationsMLEnabled(ctx context.Context) bool {
	return a.RecommendationsMLSettings(ctx).Enabled
}

// SetRecommendationsMLSettings persists settings, drops the warm embedder on an
// accelerator/disable change, and kicks a background fetch on false→true.
func (a *App) SetRecommendationsMLSettings(ctx context.Context, s RecommendationsMLSettings) error {
	switch s.Accelerator {
	case "auto", "cpu", "coreml", "cuda", "openvino", "directml":
	default:
		return fmt.Errorf("invalid accelerator %q", s.Accelerator)
	}
	if v, ok := recMLEnabledFromEnv(); ok && v != s.Enabled {
		return &ErrFieldLockedByEnv{Field: "recommendations_ml.enabled", EnvVar: recMLEnvEnabled}
	}
	if v, ok := recMLAcceleratorFromEnv(); ok && v != s.Accelerator {
		return &ErrFieldLockedByEnv{Field: "recommendations_ml.accelerator", EnvVar: recMLEnvAccelerator}
	}

	prev := a.RecommendationsMLSettings(ctx)
	persisted := a.recMLSettingsFromDB(ctx)
	persistable := RecommendationsMLSettings{Enabled: s.Enabled, Accelerator: s.Accelerator}
	if _, ok := recMLEnabledFromEnv(); ok {
		persistable.Enabled = persisted.Enabled
	}
	if _, ok := recMLAcceleratorFromEnv(); ok {
		persistable.Accelerator = persisted.Accelerator
	}
	buf, err := json.Marshal(persistable)
	if err != nil {
		return err
	}
	if err := a.SetSystemSetting(ctx, recMLSettingsKey, buf); err != nil {
		return err
	}

	if prev.Accelerator != s.Accelerator || !s.Enabled {
		a.resetRecEmbedder()
	}
	if s.Enabled && !prev.Enabled && a.recFetcher != nil {
		go a.fetchThenBackfill(a.LifetimeContext())
	}
	return nil
}

// StartRecommendationsML fetches the model at boot when the engine is enabled,
// then backfills any un-embedded items.
func (a *App) StartRecommendationsML(ctx context.Context) {
	if a.recFetcher == nil || !a.RecommendationsMLEnabled(ctx) {
		return
	}
	go a.fetchThenBackfill(ctx)
}

// TriggerRecommendationsMLFetch re-runs the download + backfill in the background
// (settings-page "download / re-verify" button).
func (a *App) TriggerRecommendationsMLFetch(ctx context.Context) {
	go a.fetchThenBackfill(ctx)
}

// fetchThenBackfill downloads the model (idempotent) then embeds any items
// missing a current-version embedding. Fire-and-forget; both steps take minutes.
func (a *App) fetchThenBackfill(ctx context.Context) {
	if a.recFetcher == nil {
		return
	}
	if err := a.recFetcher.Run(ctx); err != nil {
		return
	}
	_, _, _ = a.BackfillVideoEmbeddings(ctx, false)
}

// RecFetcher exposes the model fetcher for the settings status endpoint.
func (a *App) RecFetcher() *sonicanalysis.ModelFetcher { return a.recFetcher }

func (a *App) resetRecEmbedder() {
	a.recEmbedderMu.Lock()
	defer a.recEmbedderMu.Unlock()
	if a.recEmbedder != nil {
		a.recEmbedder.Close()
		a.recEmbedder = nil
	}
}

// recEmbedderInstance lazily loads the BGE embedder when ML is enabled and the
// model is present. Returns (nil, nil) when the engine is disabled so callers
// cleanly fall back to the non-ML path.
func (a *App) recEmbedderInstance(ctx context.Context) (*textembed.Embedder, error) {
	if !a.RecommendationsMLEnabled(ctx) {
		return nil, nil
	}
	// Enabled but the model isn't on disk yet (still downloading, or a failed/
	// pending fetch) → treat as not-ready, NOT an error. Callers surface this as
	// ml_ready=false rather than a 500. Without this guard, textembed.New would
	// fail on the missing files and every semantic search would 500 mid-download.
	if a.recFetcher != nil && !a.recFetcher.AllPresent() {
		return nil, nil
	}
	a.recEmbedderMu.Lock()
	defer a.recEmbedderMu.Unlock()
	if a.recEmbedder != nil {
		return a.recEmbedder, nil
	}
	accel := sonicanalysis.Accelerator(a.RecommendationsMLSettings(ctx).Accelerator)
	e, err := textembed.New(a.recModelsDir, accel)
	if err != nil {
		return nil, err
	}
	a.recEmbedder = e
	return e, nil
}
