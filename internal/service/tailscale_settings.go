package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/config"
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

	if cur.Enabled.Source == config.SourceEnv && cur.Enabled.Value != u.Enabled {
		return &ErrFieldLockedByEnv{Field: "tailscale.enabled", EnvVar: cur.Enabled.EnvVar}
	}
	if cur.HTTPS.Source == config.SourceEnv && cur.HTTPS.Value != u.HTTPS {
		return &ErrFieldLockedByEnv{Field: "tailscale.https", EnvVar: cur.HTTPS.EnvVar}
	}
	if cur.Funnel.Source == config.SourceEnv && cur.Funnel.Value != u.Funnel {
		return &ErrFieldLockedByEnv{Field: "tailscale.funnel", EnvVar: cur.Funnel.EnvVar}
	}
	if cur.Hostname.Source == config.SourceEnv && u.Hostname != "" && cur.Hostname.Value != u.Hostname {
		return &ErrFieldLockedByEnv{Field: "tailscale.hostname", EnvVar: cur.Hostname.EnvVar}
	}

	if cur.Enabled.Source != config.SourceEnv {
		if err := a.writeBoolSetting(ctx, tsKeyEnabled, u.Enabled); err != nil {
			return err
		}
	}
	if cur.HTTPS.Source != config.SourceEnv {
		if err := a.writeBoolSetting(ctx, tsKeyHTTPS, u.HTTPS); err != nil {
			return err
		}
	}
	if cur.Funnel.Source != config.SourceEnv {
		if err := a.writeBoolSetting(ctx, tsKeyFunnel, u.Funnel); err != nil {
			return err
		}
	}
	if cur.Hostname.Source != config.SourceEnv && u.Hostname != "" {
		if err := a.writeStringSetting(ctx, tsKeyHostname, u.Hostname); err != nil {
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
	if a.config.Tailscale.Enabled.Source == config.SourceDefault {
		if v, ok := a.readBoolSetting(ctx, tsKeyEnabled); ok {
			a.config.Tailscale.Enabled = config.Field[bool]{Value: v, Source: config.SourceDB}
		}
	}
	if a.config.Tailscale.HTTPS.Source == config.SourceDefault {
		if v, ok := a.readBoolSetting(ctx, tsKeyHTTPS); ok {
			a.config.Tailscale.HTTPS = config.Field[bool]{Value: v, Source: config.SourceDB}
		}
	}
	if a.config.Tailscale.Funnel.Source == config.SourceDefault {
		if v, ok := a.readBoolSetting(ctx, tsKeyFunnel); ok {
			a.config.Tailscale.Funnel = config.Field[bool]{Value: v, Source: config.SourceDB}
		}
	}
	if a.config.Tailscale.Hostname.Source == config.SourceDefault {
		if v, ok := a.readStringSetting(ctx, tsKeyHostname); ok && v != "" {
			a.config.Tailscale.Hostname = config.Field[string]{Value: v, Source: config.SourceDB}
		}
	}
}

func (a *App) writeBoolSetting(ctx context.Context, key string, v bool) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return a.SetSystemSetting(ctx, key, buf)
}

func (a *App) writeStringSetting(ctx context.Context, key, v string) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return a.SetSystemSetting(ctx, key, buf)
}

func (a *App) readBoolSetting(ctx context.Context, key string) (bool, bool) {
	raw, err := a.GetSystemSetting(ctx, key)
	if err != nil {
		return false, false
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, false
	}
	return v, true
}

func (a *App) readStringSetting(ctx context.Context, key string) (string, bool) {
	raw, err := a.GetSystemSetting(ctx, key)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", false
		}
		return "", false
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", false
	}
	return v, true
}
