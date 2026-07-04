package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"

	"github.com/karbowiak/heya/internal/config"
)

// system_settings keys for the Jellyfin-compatible API. Only the enabled
// toggle is UI-editable; the server id is an internal persisted identity.
const (
	jfKeyEnabled  = "jellyfin.enabled"
	jfKeyServerID = "jellyfin.server_id"
)

// SaveJellyfinSettings persists the Jellyfin toggle to system_settings,
// refusing the write when the effective value is locked by env. Mirrors
// SaveTailscaleSettings: persistence + in-memory snapshot update only. No
// subsystem needs kicking — the jellyfin middleware checks the snapshot on
// every request, so the flip is live immediately.
func (a *App) SaveJellyfinSettings(ctx context.Context, enabled bool) error {
	cur := a.config.Jellyfin

	if err := errIfEnvLockedChanged(jfKeyEnabled, cur.Enabled, enabled); err != nil {
		return err
	}
	if cur.Enabled.Source != config.SourceEnv {
		if err := a.writeBoolSetting(ctx, jfKeyEnabled, enabled); err != nil {
			return err
		}
	}
	a.UpdateJellyfinConfig(enabled)
	return nil
}

// LoadJellyfinFromDB seeds the in-memory snapshot from system_settings.
// Called once from the jellyfin middleware constructor at boot — env-set
// fields retain their env provenance; only default-sourced fields get the
// DB overlay. Safe to call with no DB rows present.
func (a *App) LoadJellyfinFromDB(ctx context.Context) {
	if a.config.Jellyfin.Enabled.Source == config.SourceDefault {
		if v, ok := a.readBoolSetting(ctx, jfKeyEnabled); ok {
			a.config.Jellyfin.Enabled = config.Field[bool]{Value: v, Source: config.SourceDB}
		}
	}
}

// UpdateJellyfinConfig overlays the in-memory jellyfin snapshot after a
// settings update, preserving env provenance (the caller refuses env-locked
// writes before getting here).
func (a *App) UpdateJellyfinConfig(enabled bool) {
	if a.config.Jellyfin.Enabled.Source != config.SourceEnv {
		a.config.Jellyfin.Enabled = config.Field[bool]{Value: enabled, Source: config.SourceDB}
	}
}

// JellyfinEnabled is the per-request gate the jellyfin middleware consults.
func (a *App) JellyfinEnabled() bool {
	return a.config.Jellyfin.Enabled.Value
}

// Process-lifetime cache for the persisted server id. Package-level (not an
// App field) deliberately: App is a boot-time singleton and keeping this out
// of app.go keeps the jellyfin feature's footprint to its own files.
var (
	jfServerIDOnce sync.Once
	jfServerID     string
)

// JellyfinServerID returns the stable server GUID advertised to Jellyfin
// clients (System/Info Id, ServerId on every DTO). Clients key their local
// caches and saved-server lists on it, so it must survive restarts: minted
// once (32 hex chars, GUID-shaped), persisted to system_settings, then
// cached for the process lifetime.
func (a *App) JellyfinServerID(ctx context.Context) string {
	jfServerIDOnce.Do(func() {
		if v, ok := a.readStringSetting(ctx, jfKeyServerID); ok && len(v) == 32 {
			jfServerID = v
			return
		}
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			// Degenerate fallback — deterministic but valid; only reachable
			// if the kernel CSPRNG is broken.
			copy(buf, []byte("heya-jellyfin-id"))
		}
		id := hex.EncodeToString(buf)
		_ = a.writeStringSetting(ctx, jfKeyServerID, id)
		jfServerID = id
	})
	return jfServerID
}
