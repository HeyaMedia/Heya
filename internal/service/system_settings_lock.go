package service

import "github.com/karbowiak/heya/internal/config"

// SystemSettingEnvLock reports whether the generic /api/system-settings/{key}
// endpoint should refuse a write to the given key because its underlying
// value (or any field within a JSON blob value) is locked by env.
//
// Returns the env var name and ok=true when locked. ok=false means the
// caller can proceed with the upsert.
//
// This guard exists because the per-domain typed endpoints
// (/api/tailscale/config, /api/admin/sonicanalysis/settings,
// /api/transcode/settings) already enforce field-level locks. The generic
// KV endpoint would otherwise let an admin bypass the lock by writing the
// raw key directly. For keys outside the env-managed namespace
// (opensubtitles credentials, etc.) the write proceeds normally.
func (a *App) SystemSettingEnvLock(key string) (envVar string, locked bool) {
	switch key {
	case tsKeyEnabled:
		if a.config.Tailscale.Enabled.Source == config.SourceEnv {
			return a.config.Tailscale.Enabled.EnvVar, true
		}
	case tsKeyHTTPS:
		if a.config.Tailscale.HTTPS.Source == config.SourceEnv {
			return a.config.Tailscale.HTTPS.EnvVar, true
		}
	case tsKeyFunnel:
		if a.config.Tailscale.Funnel.Source == config.SourceEnv {
			return a.config.Tailscale.Funnel.EnvVar, true
		}
	case tsKeyHostname:
		if a.config.Tailscale.Hostname.Source == config.SourceEnv {
			return a.config.Tailscale.Hostname.EnvVar, true
		}
	case transcoderKeyHWAccel:
		if a.config.HWAccel.Source == config.SourceEnv {
			return a.config.HWAccel.EnvVar, true
		}
	case transcoderKeyCacheMaxGB:
		if a.config.TranscodeCacheMaxGB.Source == config.SourceEnv {
			return a.config.TranscodeCacheMaxGB.EnvVar, true
		}
	case sonicSettingsKey:
		// Sonic is a multi-field blob — refuse the generic write if ANY
		// field inside is env-locked. Caller must go through the typed
		// endpoint which enforces per-field locks.
		enabled, accel := a.SonicEnvLock()
		if enabled != "" {
			return enabled, true
		}
		if accel != "" {
			return accel, true
		}
	}
	return "", false
}
