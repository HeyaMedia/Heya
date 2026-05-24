package service

import (
	"context"
	"encoding/json"

	"github.com/karbowiak/heya/internal/config"
)

const (
	transcoderKeyHWAccel    = "transcoder.hwaccel"
	transcoderKeyCacheMaxGB = "transcoder.cache_max_gb"
)

// SaveTranscoderSettings persists the two UI-editable transcoder fields
// (HWAccel + CacheMaxGB) to system_settings, refusing fields locked by env.
// Updates the in-memory snapshot so /api/transcode/status reflects the new
// value without a server restart.
//
// hwAccel="" means "don't change", cacheMaxGB<=0 means "don't change" — UI
// PATCH semantics where only the dirty fields are sent.
func (a *App) SaveTranscoderSettings(ctx context.Context, hwAccel string, cacheMaxGB int) error {
	if hwAccel != "" {
		if a.config.HWAccel.Source == config.SourceEnv && a.config.HWAccel.Value != hwAccel {
			return &ErrFieldLockedByEnv{Field: "transcoder.hwaccel", EnvVar: a.config.HWAccel.EnvVar}
		}
		if a.config.HWAccel.Source != config.SourceEnv {
			buf, _ := json.Marshal(hwAccel)
			if err := a.SetSystemSetting(ctx, transcoderKeyHWAccel, buf); err != nil {
				return err
			}
			a.config.HWAccel = config.Field[string]{Value: hwAccel, Source: config.SourceDB}
		}
	}
	if cacheMaxGB > 0 {
		if a.config.TranscodeCacheMaxGB.Source == config.SourceEnv && a.config.TranscodeCacheMaxGB.Value != cacheMaxGB {
			return &ErrFieldLockedByEnv{Field: "transcoder.cache_max_gb", EnvVar: a.config.TranscodeCacheMaxGB.EnvVar}
		}
		if a.config.TranscodeCacheMaxGB.Source != config.SourceEnv {
			buf, _ := json.Marshal(cacheMaxGB)
			if err := a.SetSystemSetting(ctx, transcoderKeyCacheMaxGB, buf); err != nil {
				return err
			}
			a.config.TranscodeCacheMaxGB = config.Field[int]{Value: cacheMaxGB, Source: config.SourceDB}
		}
	}
	return nil
}

// LoadTranscoderFromDB overlays the in-memory snapshot with persisted UI
// values for any field that wasn't already env-set. Called once at boot.
func (a *App) LoadTranscoderFromDB(ctx context.Context) {
	if a.config.HWAccel.Source == config.SourceDefault {
		if v, ok := a.readStringSetting(ctx, transcoderKeyHWAccel); ok && v != "" {
			a.config.HWAccel = config.Field[string]{Value: v, Source: config.SourceDB}
		}
	}
	if a.config.TranscodeCacheMaxGB.Source == config.SourceDefault {
		raw, err := a.GetSystemSetting(ctx, transcoderKeyCacheMaxGB)
		if err == nil {
			var v int
			if json.Unmarshal(raw, &v) == nil && v > 0 {
				a.config.TranscodeCacheMaxGB = config.Field[int]{Value: v, Source: config.SourceDB}
			}
		}
	}
}
