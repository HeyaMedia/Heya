package service

import (
	"context"
	"sync"
	"testing"

	"github.com/karbowiak/heya/internal/config"
)

func TestConfigSnapshotDeepCopiesJobWorkers(t *testing.T) {
	app := &App{config: &config.Config{Jobs: config.JobsConfig{Workers: map[string]config.Field[int]{
		"ffprobe": {Value: 2, Source: config.SourceDB},
	}}}}

	snapshot := app.ConfigSnapshot()
	snapshot.Jobs.Workers["ffprobe"] = config.Field[int]{Value: 99, Source: config.SourceDB}
	snapshot.Jobs.Workers["new_queue"] = config.Field[int]{Value: 1, Source: config.SourceDB}

	fresh := app.ConfigSnapshot()
	if got := fresh.Jobs.Workers["ffprobe"].Value; got != 2 {
		t.Fatalf("mutating snapshot changed app worker count to %d", got)
	}
	if _, ok := fresh.Jobs.Workers["new_queue"]; ok {
		t.Fatal("mutating snapshot added a worker queue to the app config")
	}
}

func TestDoctorConfigSectionIncludesRuntimeConfigValues(t *testing.T) {
	app := &App{config: &config.Config{
		AcoustIDBaseURL:           config.Field[string]{Value: "https://acoustid.invalid", Source: config.SourceEnv},
		AcoustIDRequestsPerSecond: config.Field[int]{Value: 7, Source: config.SourceEnv},
		Subsonic: config.SubsonicConfig{
			Enabled: config.Field[bool]{Value: true, Source: config.SourceDB},
		},
		Cast: config.CastConfig{
			Enabled: config.Field[bool]{Value: true, Source: config.SourceDB},
			BaseURL: config.Field[string]{Value: "https://cast.invalid", Source: config.SourceDB},
			Devices: config.Field[string]{Value: "receiver.invalid", Source: config.SourceDB},
		},
		Remote: config.RemoteConfig{
			Enabled:  config.Field[bool]{Value: true, Source: config.SourceDB},
			Port:     config.Field[int]{Value: 23456, Source: config.SourceDB},
			DNSToken: config.Field[string]{Value: "never-return-me", Source: config.SourceDB},
		},
		Jobs: config.JobsConfig{Workers: map[string]config.Field[int]{
			"ffprobe": {Value: 3, Source: config.SourceDB},
		}},
	}}

	fields := app.doctorConfigSection().Fields
	for key, want := range map[string]string{
		"infra.acoustid_base_url":            "https://acoustid.invalid",
		"infra.acoustid_requests_per_second": "7",
		"subsonic.enabled":                   "true",
		"cast.base_url":                      "https://cast.invalid",
		"remote.port":                        "23456",
		"jobs.workers.ffprobe":               "3",
		"remote.dns_token":                   "redacted (set)",
	} {
		if got := fields[key].Value; got != want {
			t.Errorf("doctor config %s = %q, want %q", key, got, want)
		}
	}
}

func TestRuntimeConfigReadersAndWritersCanRunConcurrently(t *testing.T) {
	app := &App{config: &config.Config{
		Port: config.Field[string]{Value: "8080", Source: config.SourceDefault},
		Cast: config.CastConfig{
			Enabled: config.Field[bool]{Value: true, Source: config.SourceDefault},
		},
		Jellyfin: config.JellyfinConfig{
			Enabled: config.Field[bool]{Value: false, Source: config.SourceDefault},
		},
		Subsonic: config.SubsonicConfig{
			Enabled: config.Field[bool]{Value: false, Source: config.SourceDefault},
		},
		Tailscale: config.TailscaleConfig{
			Enabled:  config.Field[bool]{Value: false, Source: config.SourceDefault},
			Hostname: config.Field[string]{Value: "heya", Source: config.SourceDefault},
			HTTPS:    config.Field[bool]{Value: false, Source: config.SourceDefault},
			Funnel:   config.Field[bool]{Value: false, Source: config.SourceDefault},
		},
		Jobs: config.JobsConfig{Workers: map[string]config.Field[int]{
			"ffprobe": {Value: 1, Source: config.SourceDefault},
		}},
	}}

	const iterations = 500
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			enabled := i%2 == 0
			app.UpdateJellyfinConfig(enabled)
			app.UpdateSubsonicConfig(enabled)
			app.UpdateTailscaleConfig(enabled, !enabled, enabled, "heya")
			app.configMu.Lock()
			app.config.Jobs.Workers["ffprobe"] = config.Field[int]{Value: i + 1, Source: config.SourceDB}
			app.configMu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = app.JellyfinEnabled()
			_ = app.SubsonicEnabled()
			_ = app.CastEnabled()
			_ = app.CastConfig()
			if snapshot := app.ConfigSnapshot(); snapshot == nil || snapshot.Jobs.Workers == nil {
				t.Errorf("iteration %d: incomplete config snapshot", i)
				return
			}
			if _, err := app.JobWorkerSettings(context.Background()); err != nil {
				t.Errorf("iteration %d: job worker settings: %v", i, err)
				return
			}
		}
	}()
	wg.Wait()
}
