package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Env vars that overlay the two UI-tunable sonic-analysis fields. When set,
// they win over the DB blob and lock the corresponding control in the UI.
const (
	sonicEnvEnabled     = "HEYA_SONIC_ENABLED"
	sonicEnvAccelerator = "HEYA_SONIC_ACCELERATOR"
)

// sonicEnabledFromEnv returns the env-sourced override for Enabled, if any.
// Returns ok=false when the env var is unset or unparseable.
func sonicEnabledFromEnv() (bool, bool) {
	v, ok := os.LookupEnv(sonicEnvEnabled)
	if !ok {
		return false, false
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return false, false
	}
	return b, true
}

// sonicAcceleratorFromEnv returns the env-sourced override for Accelerator.
// Returns ok=false when the env var is unset or empty.
func sonicAcceleratorFromEnv() (string, bool) {
	v, ok := os.LookupEnv(sonicEnvAccelerator)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	return v, true
}

// SonicEnvLock reports per-field env-lock state for sonic settings. Used by
// the API handler to translate writes to ErrFieldLockedByEnv and by
// ConfigSources to surface provenance to the UI.
func (a *App) SonicEnvLock() (enabledVar, acceleratorVar string) {
	if _, ok := sonicEnabledFromEnv(); ok {
		enabledVar = sonicEnvEnabled
	}
	if _, ok := sonicAcceleratorFromEnv(); ok {
		acceleratorVar = sonicEnvAccelerator
	}
	return
}

// SonicAnalysisSettings is the user-tunable portion of the
// sonic-analysis pipeline. Stored as a single JSON blob in
// system_settings (key=sonicanalysis); the window + schedule
// enablement live in scheduled_tasks (id=analyze_music_facets).
//
// Enabled is the master switch. Defaults to false so a fresh install
// doesn't download ~720 MB of models on first boot. Flipping it on
// kicks off an immediate model fetch and unlocks the scheduler task.
//
// The path bits (ModelsDir) are server-level so they stay derived
// from cfg.DataDir; the analyzer version is a code constant
// (sonicanalysis.AnalyzerVersion) bumped on schema-breaking changes,
// not user-tunable. The dynamic-batch accelerator is chosen
// internally based on the primary Accelerator (see Config.dynamicAccelerator).
type SonicAnalysisSettings struct {
	Enabled     bool   `json:"enabled"`
	Accelerator string `json:"accelerator"` // auto|cpu|coreml|cuda|openvino|directml
}

// DefaultSonicAnalysisSettings returns the fallback applied when no
// system_settings row exists yet (first boot, or migration didn't
// seed one).
func DefaultSonicAnalysisSettings() SonicAnalysisSettings {
	return SonicAnalysisSettings{
		Enabled:     false,
		Accelerator: "auto",
	}
}

const sonicSettingsKey = "sonicanalysis"

// SonicAnalysisSettings reads the persisted settings, falling back to defaults
// when no row exists. Env-sourced fields (HEYA_SONIC_ENABLED /
// HEYA_SONIC_ACCELERATOR) overlay the DB blob — env wins.
func (a *App) SonicAnalysisSettings(ctx context.Context) SonicAnalysisSettings {
	return effectiveSonicAnalysisSettings(ctx, a.GetSystemSetting)
}

type sonicSettingGetter func(context.Context, string) (json.RawMessage, error)

func effectiveSonicAnalysisSettings(ctx context.Context, get sonicSettingGetter) SonicAnalysisSettings {
	s := sonicAnalysisSettingsFromGetter(ctx, get)
	return overlaySonicAnalysisEnv(s)
}

func overlaySonicAnalysisEnv(s SonicAnalysisSettings) SonicAnalysisSettings {
	if v, ok := sonicEnabledFromEnv(); ok {
		s.Enabled = v
	}
	if v, ok := sonicAcceleratorFromEnv(); ok {
		s.Accelerator = v
	}
	return s
}

func effectiveSonicAnalysisSettingsStrict(ctx context.Context, get sonicSettingGetter) (SonicAnalysisSettings, error) {
	s, err := readSonicAnalysisSettings(ctx, get)
	if err != nil {
		return SonicAnalysisSettings{}, err
	}
	return overlaySonicAnalysisEnv(s), nil
}

func effectiveSonicAnalysisSettingsFromDB(ctx context.Context, db *pgxpool.Pool) SonicAnalysisSettings {
	q := sqlc.New(db)
	return effectiveSonicAnalysisSettings(ctx, func(ctx context.Context, key string) (json.RawMessage, error) {
		raw, err := q.GetSystemSetting(ctx, key)
		return json.RawMessage(raw), err
	})
}

func (a *App) sonicAnalysisSettingsFromDB(ctx context.Context) SonicAnalysisSettings {
	return sonicAnalysisSettingsFromGetter(ctx, a.GetSystemSetting)
}

func sonicAnalysisSettingsFromGetter(ctx context.Context, get sonicSettingGetter) SonicAnalysisSettings {
	s, _ := readSonicAnalysisSettings(ctx, get)
	// Any DB error here (including the expected pgx.ErrNoRows on first boot)
	// soft-falls back to defaults — we'd rather show a configurable form than
	// crash the settings page.
	return s
}

func readSonicAnalysisSettings(ctx context.Context, get sonicSettingGetter) (SonicAnalysisSettings, error) {
	s := DefaultSonicAnalysisSettings()
	if get == nil {
		return s, nil
	}
	raw, err := get(ctx, sonicSettingsKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var persisted SonicAnalysisSettings
	if err := json.Unmarshal(raw, &persisted); err != nil {
		return s, fmt.Errorf("decode sonic-analysis settings: %w", err)
	}
	if persisted.Accelerator == "" {
		persisted.Accelerator = s.Accelerator
	}
	if err := validateSonicAccelerator(persisted.Accelerator); err != nil {
		return s, err
	}
	return persisted, nil
}

func validateSonicAccelerator(accelerator string) error {
	switch accelerator {
	case "auto", "cpu", "coreml", "cuda", "openvino", "directml":
		return nil
	default:
		return fmt.Errorf("invalid accelerator %q", accelerator)
	}
}

// SetSonicAnalysisSettings persists the new settings. When the
// caller flips Enabled false→true, this also kicks off a background
// model fetch immediately (no server restart needed). Active loaded
// models are not re-loaded automatically — that would require
// destroying a running batch.
func (a *App) SetSonicAnalysisSettings(ctx context.Context, s SonicAnalysisSettings) error {
	if err := validateSonicAccelerator(s.Accelerator); err != nil {
		return err
	}
	if v, ok := sonicEnabledFromEnv(); ok && v != s.Enabled {
		return &ErrFieldLockedByEnv{Field: "sonic_analysis.enabled", EnvVar: sonicEnvEnabled}
	}
	if v, ok := sonicAcceleratorFromEnv(); ok && v != s.Accelerator {
		return &ErrFieldLockedByEnv{Field: "sonic_analysis.accelerator", EnvVar: sonicEnvAccelerator}
	}
	prev := a.SonicAnalysisSettings(ctx)
	persisted := a.sonicAnalysisSettingsFromDB(ctx)
	// Persist only the DB-owned fields. When env locks one, ignore whatever
	// the caller sent for it — the field stays untouched on disk so removing
	// the env var later reveals the previously-saved DB value.
	persistable := SonicAnalysisSettings{Enabled: s.Enabled, Accelerator: s.Accelerator}
	if _, ok := sonicEnabledFromEnv(); ok {
		persistable.Enabled = persisted.Enabled
	}
	if _, ok := sonicAcceleratorFromEnv(); ok {
		persistable.Accelerator = persisted.Accelerator
	}
	buf, err := json.Marshal(persistable)
	if err != nil {
		return err
	}
	if err := a.SetSystemSetting(ctx, sonicSettingsKey, buf); err != nil {
		return err
	}
	// Just turned on → kick off model fetch in the background. Safe
	// to call when models are already present (Run is idempotent and
	// short-circuits when AllPresent). Detach from the request context
	// (fetches take minutes) but bind to app lifetime so a graceful
	// shutdown can still cancel an in-flight download.
	if s.Enabled && !prev.Enabled && a.modelFetcher != nil {
		a.startBackground(func() {
			_ = a.modelFetcher.Run(a.LifetimeContext())
		})
	}
	return nil
}
