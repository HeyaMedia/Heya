package service

import (
	"context"
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
		if err := errIfEnvLockedChanged(transcoderKeyHWAccel, a.config.HWAccel, hwAccel); err != nil {
			return err
		}
	}
	if cacheMaxGB > 0 {
		if err := errIfEnvLockedChanged(transcoderKeyCacheMaxGB, a.config.TranscodeCacheMaxGB, cacheMaxGB); err != nil {
			return err
		}
	}

	if hwAccel != "" {
		if err := persistAndOverlayField(a, ctx, transcoderKeyHWAccel, &a.config.HWAccel, hwAccel); err != nil {
			return err
		}
	}
	if cacheMaxGB > 0 {
		if err := persistAndOverlayField(a, ctx, transcoderKeyCacheMaxGB, &a.config.TranscodeCacheMaxGB, cacheMaxGB); err != nil {
			return err
		}
	}
	return nil
}

// LoadTranscoderFromDB overlays the in-memory snapshot with persisted UI
// values for any field that wasn't already env-set. Called once at boot.
func (a *App) LoadTranscoderFromDB(ctx context.Context) {
	overlayFieldFromDB(a, ctx, &a.config.HWAccel, transcoderKeyHWAccel, func(v string) bool { return v != "" })
	overlayFieldFromDB(a, ctx, &a.config.TranscodeCacheMaxGB, transcoderKeyCacheMaxGB, func(v int) bool { return v > 0 })
}
