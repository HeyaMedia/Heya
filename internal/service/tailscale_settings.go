package service

import (
	"context"
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
	a.UpdateTailscaleConfig(u.Enabled, u.HTTPS, u.Funnel, hostname)
	return nil
}

// LoadTailscaleFromDB seeds the in-memory snapshot from system_settings.
// Called once at boot, after config.Load() — env-set fields retain their
// env provenance; only fields that fell through to defaults get overlaid
// with whatever the DB has persisted from prior UI changes.
func (a *App) LoadTailscaleFromDB(ctx context.Context) {
	overlayFieldFromDB(a, ctx, &a.config.Tailscale.Enabled, tsKeyEnabled, nil)
	overlayFieldFromDB(a, ctx, &a.config.Tailscale.HTTPS, tsKeyHTTPS, nil)
	overlayFieldFromDB(a, ctx, &a.config.Tailscale.Funnel, tsKeyFunnel, nil)
	overlayFieldFromDB(a, ctx, &a.config.Tailscale.Hostname, tsKeyHostname, func(v string) bool { return v != "" })
}
