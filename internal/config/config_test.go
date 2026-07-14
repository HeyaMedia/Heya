package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// allHeyaEnvKeys is the canonical list every test starts from a known state on.
var allHeyaEnvKeys = []string{
	"HEYA_DATABASE_URL", "HEYA_DB_MAX_CONNS", "HEYA_DB_MIN_CONNS",
	"HEYA_PASSIVE_MODE", "HEYA_ALLOW_REMOTE_ACTIVE",
	"HEYA_HOST", "HEYA_PORT", "HEYA_LOG_LEVEL",
	"HEYA_LOG_FORMAT", "HEYA_DATA_DIR", "HEYA_METADATA_URL", "HEYA_METADATA_API_KEY", "HEYA_THEINTRODB_API_KEY", "HEYA_HWACCEL",
	"HEYA_TRANSCODE_CACHE_DIR", "HEYA_TRANSCODE_CACHE_MAX_GB",
	"HEYA_CAST_ENABLED", "HEYA_CAST_BASE_URL", "HEYA_CAST_DEVICES",
	"HEYA_TAILSCALE_ENABLED", "HEYA_TAILSCALE_HOSTNAME",
	"HEYA_TAILSCALE_STATE_DIR", "HEYA_TAILSCALE_HTTPS",
	"HEYA_TAILSCALE_FUNNEL", "HEYA_TAILSCALE_AUTHKEY",
	"HEYA_PODCAST_INDEX_KEY", "HEYA_PODCAST_INDEX_SECRET",
}

func init() {
	for kind := range DefaultJobWorkerCounts {
		allHeyaEnvKeys = append(allHeyaEnvKeys, JobWorkerEnvVar(kind))
	}
}

// clearHeyaEnv unsets every HEYA_ env var for the duration of the test.
// t.Setenv("") would set the var to empty but still present, which LookupEnv
// reports as ok=true — not what we want when asserting default fallbacks.
func clearHeyaEnv(t *testing.T) {
	t.Helper()
	for _, k := range allHeyaEnvKeys {
		old, had := os.LookupEnv(k)
		os.Unsetenv(k)
		if had {
			t.Cleanup(func() { os.Setenv(k, old) })
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	clearHeyaEnv(t)

	cfg := Load()

	assert.Equal(t, "0.0.0.0", cfg.Host.Value)
	assert.Equal(t, SourceDefault, cfg.Host.Source)
	assert.Equal(t, "info", cfg.LogLevel.Value)
	assert.Equal(t, "console", cfg.LogFormat.Value)
	assert.Equal(t, "./data", cfg.DataDir.Value)
	assert.Equal(t, "auto", cfg.HWAccel.Value)
	assert.Equal(t, 50, cfg.TranscodeCacheMaxGB.Value)
	assert.Equal(t, 4, cfg.Jobs.Workers["process_scan"].Value)
	assert.Equal(t, 4, cfg.Jobs.Workers["fetch_metadata"].Value)
	assert.Equal(t, 4, cfg.Jobs.Workers["apply_metadata"].Value)
	assert.False(t, cfg.Tailscale.Enabled.Value)
	assert.False(t, cfg.Tailscale.HTTPS.Value)
	assert.False(t, cfg.Tailscale.Funnel.Value)
	assert.Equal(t, "heya", cfg.Tailscale.Hostname.Value)
}

func TestLoadEnvOverrides(t *testing.T) {
	clearHeyaEnv(t)
	t.Setenv("HEYA_HOST", "127.0.0.1")
	t.Setenv("HEYA_PORT", "9090")
	t.Setenv("HEYA_TAILSCALE_ENABLED", "true")
	t.Setenv("HEYA_TAILSCALE_HTTPS", "true")
	t.Setenv("HEYA_TRANSCODE_CACHE_MAX_GB", "200")
	t.Setenv("HEYA_JOB_WORKERS_APPLY_METADATA", "6")
	t.Setenv("HEYA_CAST_BASE_URL", "https://heya.lan")

	cfg := Load()

	assert.Equal(t, "127.0.0.1", cfg.Host.Value)
	assert.Equal(t, SourceEnv, cfg.Host.Source)
	assert.Equal(t, "HEYA_HOST", cfg.Host.EnvVar)

	assert.Equal(t, "9090", cfg.Port.Value)
	assert.Equal(t, SourceEnv, cfg.Port.Source)

	assert.True(t, cfg.Tailscale.Enabled.Value)
	assert.Equal(t, SourceEnv, cfg.Tailscale.Enabled.Source)
	assert.True(t, cfg.Tailscale.HTTPS.Value)
	assert.Equal(t, 200, cfg.TranscodeCacheMaxGB.Value)
	assert.Equal(t, 6, cfg.Jobs.Workers["apply_metadata"].Value)
	assert.Equal(t, SourceEnv, cfg.Jobs.Workers["apply_metadata"].Source)
	assert.Equal(t, "https://heya.lan", cfg.Cast.BaseURL.Value)
	assert.Equal(t, SourceEnv, cfg.Cast.BaseURL.Source)
}

func TestAddr(t *testing.T) {
	cfg := &Config{
		Host: Field[string]{Value: "127.0.0.1", Source: SourceDefault},
		Port: Field[string]{Value: "3000", Source: SourceDefault},
	}
	assert.Equal(t, "127.0.0.1:3000", cfg.Addr())
}

func TestSources(t *testing.T) {
	clearHeyaEnv(t)
	t.Setenv("HEYA_HOST", "from-env")

	cfg := Load()
	sources := cfg.Sources()

	assert.Equal(t, SourceEnv, sources["infra.host"].Source)
	assert.Equal(t, "HEYA_HOST", sources["infra.host"].EnvVar)
	assert.Equal(t, SourceDefault, sources["infra.port"].Source)
	assert.Empty(t, sources["infra.port"].EnvVar)
	assert.Equal(t, SourceDefault, sources["jobs.workers.process_scan"].Source)
}

func TestAllowRemoteActiveSourceRegistered(t *testing.T) {
	clearHeyaEnv(t)
	t.Setenv("HEYA_ALLOW_REMOTE_ACTIVE", "true")
	cfg := Load()
	assert.True(t, cfg.AllowRemoteActive.Value)
	assert.Equal(t, SourceEnv, cfg.Sources()["infra.allow_remote_active"].Source)
}

func TestSourceRegistryKeysAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, field := range sourceFields {
		if seen[field.key] {
			t.Fatalf("duplicate source key %q", field.key)
		}
		seen[field.key] = true
	}
}
