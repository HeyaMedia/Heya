package service

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/config"
)

// ConfigSources is the flat provenance map returned by /api/config/sources.
// Keys use dotted paths matching the layout in `.env.example`:
//
//	infra.database_url
//	transcoder.hwaccel
//	tailscale.enabled
//	sonic_analysis.enabled
//	library.<id>.name        (only present for env-managed libraries)
type ConfigSources map[string]config.SourceEntry

// ConfigSources walks every UI-relevant setting and reports where its
// effective value came from. Used by the frontend `useConfigSources()`
// composable to grey out any control whose source is "env".
//
// The "DB" source means "currently persisted in system_settings or
// libraries — the UI may edit it freely". "default" means neither env
// nor DB has a value yet; UI edits go straight to DB on first save.
func (a *App) ConfigSources(ctx context.Context) ConfigSources {
	out := make(ConfigSources, 32)

	// Infra + transcoder + tailscale — config.Sources() handles those
	// since they're tracked on the Field[T]s loaded at boot. After
	// LoadTailscaleFromDB / LoadTranscoderFromDB ran, the Source on
	// each field is either env / db / default.
	for k, v := range a.ConfigSnapshot().Sources() {
		out[k] = v
	}

	// Sonic analysis: env wins per-field, then DB if the blob exists,
	// then default. The UI greys out exactly the fields env locked.
	enabledEnv, acceleratorEnv := a.SonicEnvLock()
	out["sonic_analysis.enabled"] = a.sonicAnalysisSource(ctx, enabledEnv)
	out["sonic_analysis.accelerator"] = a.sonicAnalysisSource(ctx, acceleratorEnv)

	// AI subsystem: same per-field env-wins contract as sonic analysis.
	aiLocks := aiEnvLocks()
	for _, field := range []string{"mode", "provider", "api_key", "model", "base_url", "local_model", "local_backend", "context_size"} {
		out["ai."+field] = a.aiSource(ctx, aiLocks[field])
	}

	// Libraries: only env-managed rows appear here, with all three
	// identity fields. DB-managed libraries return no entries — the
	// frontend treats absence as "editable".
	for libID, env := range a.envLibraries {
		base := fmt.Sprintf("library.%d", libID)
		out[base+".name"] = config.SourceEntry{Source: config.SourceEnv, EnvVar: env.NameEnv}
		out[base+".paths"] = config.SourceEntry{Source: config.SourceEnv, EnvVar: env.PathsEnv}
		out[base+".media_type"] = config.SourceEntry{Source: config.SourceEnv, EnvVar: env.TypeEnv}
	}

	return out
}

func (a *App) sonicAnalysisSource(ctx context.Context, envVar string) config.SourceEntry {
	if envVar != "" {
		return config.SourceEntry{Source: config.SourceEnv, EnvVar: envVar}
	}
	if _, err := a.GetSystemSetting(ctx, sonicSettingsKey); err == nil {
		return config.SourceEntry{Source: config.SourceDB}
	}
	return config.SourceEntry{Source: config.SourceDefault}
}

func (a *App) aiSource(ctx context.Context, envVar string) config.SourceEntry {
	if envVar != "" {
		return config.SourceEntry{Source: config.SourceEnv, EnvVar: envVar}
	}
	if _, err := a.GetSystemSetting(ctx, aiSettingsKey); err == nil {
		return config.SourceEntry{Source: config.SourceDB}
	}
	return config.SourceEntry{Source: config.SourceDefault}
}
