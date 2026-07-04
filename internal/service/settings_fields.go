package service

import (
	"context"
	"encoding/json"

	"github.com/karbowiak/heya/internal/config"
)

// Shared plumbing for UI-editable config fields persisted to system_settings.
// The provenance contract (CLAUDE.md "Config provenance"): env wins and locks
// the UI; DB values overlay defaults at boot; a write is refused when env
// holds a *different* value and silently skipped when env already matches —
// the DB row keeps whatever an earlier UI save stored, so removing the env
// var later reveals it again.
//
// Save methods validate every field with errIfEnvLockedChanged BEFORE
// persisting any of them (validate-all-then-write-all), so a mid-update lock
// error never leaves a partial write behind.

// writeSetting JSON-encodes v under key in system_settings.
func writeSetting[T any](a *App, ctx context.Context, key string, v T) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return a.SetSystemSetting(ctx, key, buf)
}

// readSetting fetches and decodes key; ok=false when missing or malformed.
func readSetting[T any](a *App, ctx context.Context, key string) (T, bool) {
	var v T
	raw, err := a.GetSystemSetting(ctx, key)
	if err != nil {
		return v, false
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, false
	}
	return v, true
}

// persistFieldSetting writes a validated new value for one field, skipping
// the write when env owns the field.
func persistFieldSetting[T any](a *App, ctx context.Context, key string, cur config.Field[T], next T) error {
	if cur.Source == config.SourceEnv {
		return nil
	}
	return writeSetting(a, ctx, key, next)
}

// persistAndOverlayField persists next and updates the in-memory snapshot
// field, both skipped when env owns it. For Save methods whose subsystem
// reads the snapshot directly (no dedicated Update*Config hook).
func persistAndOverlayField[T any](a *App, ctx context.Context, key string, field *config.Field[T], next T) error {
	if field.Source == config.SourceEnv {
		return nil
	}
	if err := writeSetting(a, ctx, key, next); err != nil {
		return err
	}
	*field = config.Field[T]{Value: next, Source: config.SourceDB}
	return nil
}

// overlayFieldFromDB replaces a default-sourced field with the persisted DB
// value when one exists and passes accept (nil accept = any decoded value).
// Env- and DB-sourced fields are left alone so env provenance survives boot.
func overlayFieldFromDB[T any](a *App, ctx context.Context, field *config.Field[T], key string, accept func(T) bool) {
	if field.Source != config.SourceDefault {
		return
	}
	v, ok := readSetting[T](a, ctx, key)
	if !ok || (accept != nil && !accept(v)) {
		return
	}
	*field = config.Field[T]{Value: v, Source: config.SourceDB}
}
