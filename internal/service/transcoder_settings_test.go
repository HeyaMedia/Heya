package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/karbowiak/heya/internal/transcoder"
)

func TestSaveTranscoderSettingsAppliesLiveAndPersistsUnlimited(t *testing.T) {
	ctx := context.Background()
	pool := testutil.SetupDB(t)
	_, _ = pool.Exec(ctx, "DELETE FROM system_settings WHERE key IN ($1, $2)", transcoderKeyHWAccel, transcoderKeyCacheMaxGB)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM system_settings WHERE key IN ($1, $2)", transcoderKeyHWAccel, transcoderKeyCacheMaxGB)
	})

	cfg := &config.Config{
		HWAccel:             config.Field[string]{Value: string(transcoder.HwAccelNone), Source: config.SourceDefault},
		TranscodeCacheMaxGB: config.Field[int]{Value: 1, Source: config.SourceDefault},
	}
	cache := transcoder.NewCacheManager(t.TempDir(), 1)
	manager := transcoder.NewSessionManager(
		cache,
		transcoder.NewHwAccelProvider(t.TempDir(), string(transcoder.HwAccelNone)),
		transcoder.NewFFmpegBuilder(),
	)
	t.Cleanup(manager.Close)
	app := &App{db: pool, config: cfg, transcodeCache: cache, transcoder: manager}

	if err := app.SaveTranscoderSettings(ctx, string(transcoder.HwAccelVideoToolbox), 0); err != nil {
		t.Fatalf("SaveTranscoderSettings: %v", err)
	}
	if got := manager.HWAccel().Type; got != transcoder.HwAccelVideoToolbox {
		t.Fatalf("live hardware acceleration = %s, want %s", got, transcoder.HwAccelVideoToolbox)
	}
	if got := cache.Stats().MaxSizeGB; got != 0 {
		t.Fatalf("live cache max = %d, want unlimited (0)", got)
	}

	raw, err := sqlc.New(pool).GetSystemSetting(ctx, transcoderKeyCacheMaxGB)
	if err != nil {
		t.Fatalf("read persisted cache setting: %v", err)
	}
	var persisted int
	if err := json.Unmarshal(raw, &persisted); err != nil || persisted != 0 {
		t.Fatalf("persisted cache setting = %s (err %v), want 0", raw, err)
	}
}

func TestLoadTranscoderConfigFromDBAcceptsUnlimited(t *testing.T) {
	ctx := context.Background()
	pool := testutil.SetupDB(t)
	_, _ = pool.Exec(ctx, `INSERT INTO system_settings (key, value) VALUES ($1, '0'::jsonb)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, transcoderKeyCacheMaxGB)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM system_settings WHERE key = $1", transcoderKeyCacheMaxGB)
	})
	cfg := &config.Config{TranscodeCacheMaxGB: config.Field[int]{Value: 50, Source: config.SourceDefault}}

	loadTranscoderConfigFromDB(ctx, pool, cfg)

	if cfg.TranscodeCacheMaxGB.Value != 0 || cfg.TranscodeCacheMaxGB.Source != config.SourceDB {
		t.Fatalf("loaded cache config = %+v, want DB-sourced unlimited", cfg.TranscodeCacheMaxGB)
	}
}

func TestSaveTranscoderSettingsRejectsInvalidValues(t *testing.T) {
	app := &App{config: &config.Config{
		HWAccel:             config.Field[string]{Value: string(transcoder.HwAccelNone), Source: config.SourceDefault},
		TranscodeCacheMaxGB: config.Field[int]{Value: 50, Source: config.SourceDefault},
	}}

	if err := app.SaveTranscoderSettings(context.Background(), "definitely-not-an-accelerator", 50); err == nil {
		t.Fatal("invalid hardware accelerator was accepted")
	}
	if err := app.SaveTranscoderSettings(context.Background(), "", -1); err == nil {
		t.Fatal("negative cache size was accepted")
	}
}

func TestLoadTranscoderConfigFromDBRejectsInvalidHardwareMode(t *testing.T) {
	ctx := context.Background()
	pool := testutil.SetupDB(t)
	_, _ = pool.Exec(ctx, `INSERT INTO system_settings (key, value) VALUES ($1, '"bogus"'::jsonb)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, transcoderKeyHWAccel)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM system_settings WHERE key = $1", transcoderKeyHWAccel)
	})
	cfg := &config.Config{HWAccel: config.Field[string]{Value: string(transcoder.HwAccelAuto), Source: config.SourceDefault}}

	loadTranscoderConfigFromDB(ctx, pool, cfg)

	if cfg.HWAccel.Value != string(transcoder.HwAccelAuto) || cfg.HWAccel.Source != config.SourceDefault {
		t.Fatalf("invalid DB hardware mode changed config: %+v", cfg.HWAccel)
	}
}
