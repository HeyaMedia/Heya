package service

import (
	"context"
	"errors"

	"github.com/karbowiak/heya/internal/config"
	tsnetwrap "github.com/karbowiak/heya/internal/tailscale"
	"github.com/rs/zerolog/log"
)

// TailscaleUpdate is the DTO accepted by the API for runtime tailscale
// changes. Hostname is optional in the persisted form — when empty the
// default ("heya") stays in effect.
type TailscaleUpdate struct {
	Enabled  bool
	HTTPS    bool
	Funnel   bool
	Hostname string
}

// system_settings keys for the four UI-editable tailscale fields. AuthKey
// and StateDir intentionally are NOT here — they're env-only.
const (
	tsKeyEnabled  = "tailscale.enabled"
	tsKeyHTTPS    = "tailscale.https"
	tsKeyFunnel   = "tailscale.funnel"
	tsKeyHostname = "tailscale.hostname"
)

// SaveTailscaleSettings persists each tailscale field to system_settings,
// refusing any field whose effective value is locked by an env var. The
// caller (handler) is responsible for triggering the tsnet hot-toggle
// after this returns successfully — this method only handles persistence
// and the in-memory snapshot update.
func (a *App) SaveTailscaleSettings(ctx context.Context, u TailscaleUpdate) error {
	a.configMu.Lock()
	defer a.configMu.Unlock()

	cur := a.config.Tailscale

	if err := errIfEnvLockedChanged(tsKeyEnabled, cur.Enabled, u.Enabled); err != nil {
		return err
	}
	if err := errIfEnvLockedChanged(tsKeyHTTPS, cur.HTTPS, u.HTTPS); err != nil {
		return err
	}
	if err := errIfEnvLockedChanged(tsKeyFunnel, cur.Funnel, u.Funnel); err != nil {
		return err
	}
	if u.Hostname != "" {
		if err := errIfEnvLockedChanged(tsKeyHostname, cur.Hostname, u.Hostname); err != nil {
			return err
		}
	}

	if err := persistFieldSetting(a, ctx, tsKeyEnabled, cur.Enabled, u.Enabled); err != nil {
		return err
	}
	if err := persistFieldSetting(a, ctx, tsKeyHTTPS, cur.HTTPS, u.HTTPS); err != nil {
		return err
	}
	if err := persistFieldSetting(a, ctx, tsKeyFunnel, cur.Funnel, u.Funnel); err != nil {
		return err
	}
	if u.Hostname != "" {
		if err := persistFieldSetting(a, ctx, tsKeyHostname, cur.Hostname, u.Hostname); err != nil {
			return err
		}
	}

	hostname := cur.Hostname.Value
	if u.Hostname != "" {
		hostname = u.Hostname
	}
	a.updateTailscaleConfigLocked(u.Enabled, u.HTTPS, u.Funnel, hostname)
	return nil
}

// SaveAndApplyTailscaleSettings orders persistence and the asynchronous live
// transition as one service operation. Concurrent admin requests therefore
// cannot save B after A but accidentally apply A last.
func (a *App) SaveAndApplyTailscaleSettings(ctx context.Context, update TailscaleUpdate) (config.TailscaleConfig, error) {
	a.tailscaleSettingsMu.Lock()
	defer a.tailscaleSettingsMu.Unlock()
	if err := a.SaveTailscaleSettings(ctx, update); err != nil {
		return config.TailscaleConfig{}, err
	}
	return a.applyTailscaleRuntimeLocked()
}

// ApplyTailscaleRuntime applies the already-loaded effective config (used at
// server boot). The transition is asynchronous and App-owned.
func (a *App) ApplyTailscaleRuntime() error {
	a.tailscaleSettingsMu.Lock()
	defer a.tailscaleSettingsMu.Unlock()
	_, err := a.applyTailscaleRuntimeLocked()
	return err
}

func (a *App) applyTailscaleRuntimeLocked() (config.TailscaleConfig, error) {
	snapshot := a.ConfigSnapshot()
	if snapshot == nil {
		return config.TailscaleConfig{}, errors.New("tailscale config is unavailable")
	}
	manager := a.Tailscale()
	if manager == nil {
		return config.TailscaleConfig{}, errors.New("tailscale manager not initialized")
	}
	cur := snapshot.Tailscale
	started := a.tailscaleTransition.Start(a, func(ctx context.Context) {
		var err error
		if cur.Enabled.Value {
			err = manager.Enable(ctx, tsnetwrap.Config{
				Enabled:  true,
				Hostname: cur.Hostname.Value,
				AuthKey:  cur.AuthKey.Value,
				StateDir: cur.StateDir.Value,
				HTTPS:    cur.HTTPS.Value,
				Funnel:   cur.Funnel.Value,
			})
		} else {
			err = manager.Disable()
		}
		if err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Msg("tailscale live config transition failed")
		}
	})
	if !started {
		return config.TailscaleConfig{}, errAppClosing
	}
	return cur, nil
}

// SaveAndLogoutTailscale persists the disabled state and orders Logout behind
// any in-flight live transition. A newer config save cancels/supersedes it.
func (a *App) SaveAndLogoutTailscale(ctx context.Context, update TailscaleUpdate) error {
	a.tailscaleSettingsMu.Lock()
	defer a.tailscaleSettingsMu.Unlock()
	if err := a.SaveTailscaleSettings(ctx, update); err != nil {
		return err
	}
	manager := a.Tailscale()
	if manager == nil {
		return errors.New("tailscale manager not initialized")
	}
	if !a.tailscaleTransition.Start(a, func(workCtx context.Context) {
		if err := manager.Logout(workCtx); err != nil && workCtx.Err() == nil {
			log.Warn().Err(err).Msg("tailscale logout failed")
		}
	}) {
		return errAppClosing
	}
	return nil
}

// LoadTailscaleFromDB seeds the in-memory snapshot from system_settings.
// Called once at boot, after config.Load() — env-set fields retain their
// env provenance; only fields that fell through to defaults get overlaid
// with whatever the DB has persisted from prior UI changes.
func (a *App) LoadTailscaleFromDB(ctx context.Context) {
	a.configMu.Lock()
	defer a.configMu.Unlock()

	overlayFieldFromDB(a, ctx, &a.config.Tailscale.Enabled, tsKeyEnabled, nil)
	overlayFieldFromDB(a, ctx, &a.config.Tailscale.HTTPS, tsKeyHTTPS, nil)
	overlayFieldFromDB(a, ctx, &a.config.Tailscale.Funnel, tsKeyFunnel, nil)
	overlayFieldFromDB(a, ctx, &a.config.Tailscale.Hostname, tsKeyHostname, func(v string) bool { return v != "" })
}
