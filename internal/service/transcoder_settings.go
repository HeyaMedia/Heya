package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/transcoder"
)

const (
	transcoderKeyHWAccel    = "transcoder.hwaccel"
	transcoderKeyCacheMaxGB = "transcoder.cache_max_gb"
)

// SaveTranscoderSettings atomically persists the UI-editable transcoder
// fields, refusing values locked by env, then applies the effective values to
// new sessions in the running API process. hwAccel="" leaves that field
// unchanged. cacheMaxGB=0 is a real value and means unlimited.
func (a *App) SaveTranscoderSettings(ctx context.Context, hwAccel string, cacheMaxGB int) error {
	if cacheMaxGB < 0 {
		return errors.New("transcode cache size must be non-negative")
	}
	if hwAccel != "" && !transcoder.IsValidHWAccelMode(hwAccel) {
		return fmt.Errorf("unsupported hardware acceleration mode %q", hwAccel)
	}

	// Serialize persistence and live application. Without one critical section,
	// concurrent saves can commit A then B but apply their in-memory values B
	// then A, leaving runtime state inconsistent with the database.
	a.configMu.Lock()
	defer a.configMu.Unlock()
	if hwAccel != "" {
		if err := errIfEnvLockedChanged(transcoderKeyHWAccel, a.config.HWAccel, hwAccel); err != nil {
			return err
		}
	}
	if err := errIfEnvLockedChanged(transcoderKeyCacheMaxGB, a.config.TranscodeCacheMaxGB, cacheMaxGB); err != nil {
		return err
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := sqlc.New(tx)
	if hwAccel != "" && a.config.HWAccel.Source != config.SourceEnv {
		value, _ := json.Marshal(hwAccel)
		if err := q.UpsertSystemSetting(ctx, sqlc.UpsertSystemSettingParams{Key: transcoderKeyHWAccel, Value: value}); err != nil {
			return err
		}
	}
	if a.config.TranscodeCacheMaxGB.Source != config.SourceEnv {
		value, _ := json.Marshal(cacheMaxGB)
		if err := q.UpsertSystemSetting(ctx, sqlc.UpsertSystemSettingParams{Key: transcoderKeyCacheMaxGB, Value: value}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	if hwAccel != "" && a.config.HWAccel.Source != config.SourceEnv {
		a.config.HWAccel = config.Field[string]{Value: hwAccel, Source: config.SourceDB}
	}
	if a.config.TranscodeCacheMaxGB.Source != config.SourceEnv {
		a.config.TranscodeCacheMaxGB = config.Field[int]{Value: cacheMaxGB, Source: config.SourceDB}
	}
	if a.transcoder != nil {
		a.transcoder.ConfigureHWAccel(a.config.HWAccel.Value)
	}
	if a.transcodeCache != nil {
		a.transcodeCache.SetMaxSizeGB(a.config.TranscodeCacheMaxGB.Value)
	}
	return nil
}

// LoadTranscoderFromDB overlays the in-memory snapshot with persisted UI
// values for any field that wasn't already env-set. Called once at boot.
func (a *App) LoadTranscoderFromDB(ctx context.Context) {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	loadTranscoderConfigFromDB(ctx, a.db, a.config)
}

// loadTranscoderConfigFromDB is deliberately App-independent so boot can
// apply persisted settings before constructing the cache and hardware
// provider that consume them. Loading them after those constructors made the
// config response look correct while the live runtime kept using defaults.
func loadTranscoderConfigFromDB(ctx context.Context, db *pgxpool.Pool, cfg *config.Config) {
	q := sqlc.New(db)
	overlayBootField(ctx, q, &cfg.HWAccel, transcoderKeyHWAccel, transcoder.IsValidHWAccelMode)
	overlayBootField(ctx, q, &cfg.TranscodeCacheMaxGB, transcoderKeyCacheMaxGB, func(v int) bool { return v >= 0 })
}

func overlayBootField[T any](ctx context.Context, q *sqlc.Queries, field *config.Field[T], key string, accept func(T) bool) {
	if field.Source != config.SourceDefault {
		return
	}
	raw, err := q.GetSystemSetting(ctx, key)
	if err != nil {
		return
	}
	var value T
	if json.Unmarshal(raw, &value) != nil || !accept(value) {
		return
	}
	*field = config.Field[T]{Value: value, Source: config.SourceDB}
}
