package service

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
	cfg := a.ConfigSnapshot()
	switch key {
	case jfKeyEnabled:
		return cfg.Jellyfin.Enabled.EnvLock()
	case tsKeyEnabled:
		return cfg.Tailscale.Enabled.EnvLock()
	case tsKeyHTTPS:
		return cfg.Tailscale.HTTPS.EnvLock()
	case tsKeyFunnel:
		return cfg.Tailscale.Funnel.EnvLock()
	case tsKeyHostname:
		return cfg.Tailscale.Hostname.EnvLock()
	case transcoderKeyHWAccel:
		return cfg.HWAccel.EnvLock()
	case transcoderKeyCacheMaxGB:
		return cfg.TranscodeCacheMaxGB.EnvLock()
	case "lastfm":
		// Whole-blob lock: env key presence forces env provenance for the pair.
		if v, locked := cfg.LastfmAPIKey.EnvLock(); locked {
			return v, true
		}
		return cfg.LastfmSecret.EnvLock()
	case "podcast_index":
		if v, locked := cfg.PodcastIndexKey.EnvLock(); locked {
			return v, true
		}
		return cfg.PodcastIndexSecret.EnvLock()
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
