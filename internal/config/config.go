package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds every infrastructure-level knob loaded at boot. Each Field
// carries provenance so consumers (handlers, the UI) can tell whether a
// value came from env or fell back to the built-in default. Anything that
// belongs to the "user-tunable in the settings UI" category lives in DB
// tables, not here — see internal/service/sonicanalysis_settings.go and
// internal/service/tailscale_settings.go.
type Config struct {
	DatabaseURL         Field[string]
	Host                Field[string]
	Port                Field[string]
	LogLevel            Field[string]
	LogFormat           Field[string]
	HeyaMediaURL        Field[string]
	DataDir             Field[string]
	HWAccel             Field[string]
	TranscodeCacheDir   Field[string]
	TranscodeCacheMaxGB Field[int]
	Tailscale           TailscaleConfig
}

// TailscaleConfig holds the env-sourced tailscale knobs. Enabled/HTTPS/Funnel
// can also be set from the UI (DB-backed) — the effective value is computed
// at request time by merging env > db > default. AuthKey and StateDir are
// boot-time only: never UI-editable, never persisted to DB.
type TailscaleConfig struct {
	Enabled  Field[bool]
	Hostname Field[string]
	AuthKey  Field[string] `json:"-"` // never exposed via API
	StateDir Field[string]
	HTTPS    Field[bool]
	Funnel   Field[bool]
}

// Load reads .env / .env.local (without overriding real env), then resolves
// every Field from the environment. Defaults are applied for any var that
// wasn't set. There is no yaml layer — Heya is env-only.
func Load() *Config {
	loadDotEnv()

	dataDir := envString("HEYA_DATA_DIR", "./data")

	return &Config{
		DatabaseURL:         envString("HEYA_DATABASE_URL", "postgres://heya:heya@localhost:5440/heya?sslmode=disable"),
		Host:                envString("HEYA_HOST", "0.0.0.0"),
		Port:                envString("HEYA_PORT", "8080"),
		LogLevel:            envString("HEYA_LOG_LEVEL", "info"),
		LogFormat:           envString("HEYA_LOG_FORMAT", "console"),
		HeyaMediaURL:        envString("HEYA_MEDIA_URL", "https://heya.media"),
		DataDir:             dataDir,
		HWAccel:             envString("HEYA_HWACCEL", "auto"),
		TranscodeCacheDir:   envString("HEYA_TRANSCODE_CACHE_DIR", dataDir.Value+"/transcode"),
		TranscodeCacheMaxGB: envInt("HEYA_TRANSCODE_CACHE_MAX_GB", 50),
		Tailscale: TailscaleConfig{
			Enabled:  envBool("HEYA_TAILSCALE_ENABLED", false),
			Hostname: envString("HEYA_TAILSCALE_HOSTNAME", "heya"),
			AuthKey:  envString("HEYA_TAILSCALE_AUTHKEY", ""),
			StateDir: envString("HEYA_TAILSCALE_STATE_DIR", dataDir.Value+"/tailscale"),
			HTTPS:    envBool("HEYA_TAILSCALE_HTTPS", false),
			Funnel:   envBool("HEYA_TAILSCALE_FUNNEL", false),
		},
	}
}

func loadDotEnv() {
	for _, path := range []string{".env", ".env.local"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if len(v) >= 2 {
				first, last := v[0], v[len(v)-1]
				if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
					v = v[1 : len(v)-1]
				}
			}
			if _, exists := os.LookupEnv(k); !exists {
				_ = os.Setenv(k, v)
			}
		}
	}
}

// Addr returns the host:port the HTTP server should bind.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%s", c.Host.Value, c.Port.Value)
}

// Sources returns the flat key→provenance map for the infra layer. The
// /api/config/sources endpoint extends this with DB-backed setting groups
// (tailscale.*, sonic_analysis.*, library.N.*).
func (c *Config) Sources() map[string]SourceEntry {
	return map[string]SourceEntry{
		"infra.database_url":      c.DatabaseURL.Entry(),
		"infra.host":              c.Host.Entry(),
		"infra.port":              c.Port.Entry(),
		"infra.log_level":         c.LogLevel.Entry(),
		"infra.log_format":        c.LogFormat.Entry(),
		"infra.data_dir":          c.DataDir.Entry(),
		"infra.heya_media_url":    c.HeyaMediaURL.Entry(),
		"transcoder.hwaccel":      c.HWAccel.Entry(),
		"transcoder.cache_dir":    c.TranscodeCacheDir.Entry(),
		"transcoder.cache_max_gb": c.TranscodeCacheMaxGB.Entry(),
		"tailscale.enabled":       c.Tailscale.Enabled.Entry(),
		"tailscale.hostname":      c.Tailscale.Hostname.Entry(),
		"tailscale.state_dir":     c.Tailscale.StateDir.Entry(),
		"tailscale.https":         c.Tailscale.HTTPS.Entry(),
		"tailscale.funnel":        c.Tailscale.Funnel.Entry(),
	}
}
