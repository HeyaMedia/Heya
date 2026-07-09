package service

import (
	"context"

	"github.com/karbowiak/heya/internal/config"
)

// system_settings key for the Subsonic-compatible API. Mirrors the Jellyfin
// toggle: UI-editable unless env-locked, checked per-request by the
// middleware so flips are live immediately.
const subsonicKeyEnabled = "subsonic.enabled"

// SaveSubsonicSettings persists the Subsonic toggle to system_settings,
// refusing the write when the effective value is locked by env.
func (a *App) SaveSubsonicSettings(ctx context.Context, enabled bool) error {
	cur := a.config.Subsonic

	if err := errIfEnvLockedChanged(subsonicKeyEnabled, cur.Enabled, enabled); err != nil {
		return err
	}
	if err := persistFieldSetting(a, ctx, subsonicKeyEnabled, cur.Enabled, enabled); err != nil {
		return err
	}
	a.UpdateSubsonicConfig(enabled)
	return nil
}

// LoadSubsonicFromDB seeds the in-memory snapshot from system_settings.
// Called once from the subsonic middleware constructor at boot — env-set
// fields retain their env provenance; only default-sourced fields get the
// DB overlay. Safe to call with no DB rows present.
func (a *App) LoadSubsonicFromDB(ctx context.Context) {
	if a.db == nil {
		return // spec-dump / test construction without a database
	}
	overlayFieldFromDB(a, ctx, &a.config.Subsonic.Enabled, subsonicKeyEnabled, nil)
}

// UpdateSubsonicConfig overlays the in-memory subsonic snapshot after a
// settings update, preserving env provenance.
func (a *App) UpdateSubsonicConfig(enabled bool) {
	if a.config.Subsonic.Enabled.Source != config.SourceEnv {
		a.config.Subsonic.Enabled = config.Field[bool]{Value: enabled, Source: config.SourceDB}
	}
}

// SubsonicEnabled is the per-request gate the subsonic middleware consults.
func (a *App) SubsonicEnabled() bool {
	return a.config.Subsonic.Enabled.Value
}
