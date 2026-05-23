package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	DatabaseURL         string
	Host                string
	Port                string
	LogLevel            string
	LogFormat           string
	HeyaMediaURL        string
	DataDir             string
	TranscodeCacheDir   string
	TranscodeCacheMaxGB int
	HWAccel             string
	Tailscale           TailscaleConfig
}

type TailscaleConfig struct {
	Enabled  bool   `json:"enabled"`
	Hostname string `json:"hostname"`
	AuthKey  string `json:"-"` // never expose over the API
	StateDir string `json:"state_dir,omitempty"`
	HTTPS    bool   `json:"https"`
	Funnel   bool   `json:"funnel"`
}

func Load() *Config {
	loadDotEnv()

	if path := FindConfigFile(); path != "" {
		if fc, err := LoadFile(path); err == nil {
			return MergeFileWithEnv(fc)
		}
	}

	cfg := &Config{
		DatabaseURL:  getenv("DATABASE_URL", ""),
		Host:         getenv("HOST", ""),
		Port:         getenv("PORT", ""),
		LogLevel:     getenv("LOG_LEVEL", ""),
		LogFormat:    getenv("LOG_FORMAT", ""),
		HeyaMediaURL: getenv("HEYA_MEDIA_URL", ""),
		DataDir:      getenv("DATA_DIR", ""),
		HWAccel:      getenv("HEYA_HWACCEL", ""),
		Tailscale: TailscaleConfig{
			Enabled:  getenv("HEYA_TAILSCALE_ENABLED", "") == "true",
			Hostname: getenv("HEYA_TAILSCALE_HOSTNAME", ""),
			AuthKey:  getenv("HEYA_TAILSCALE_AUTHKEY", ""),
			StateDir: getenv("HEYA_TAILSCALE_STATE_DIR", ""),
			HTTPS:    getenv("HEYA_TAILSCALE_HTTPS", "true") != "false",
			Funnel:   getenv("HEYA_TAILSCALE_FUNNEL", "") == "true",
		},
	}
	applyDefaults(cfg)
	return cfg
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
			if os.Getenv(k) == "" {
				os.Setenv(k, v)
			}
		}
	}
}

func applyDefaults(cfg *Config) {
	if cfg.DatabaseURL == "" {
		cfg.DatabaseURL = "postgres://heya:heya@localhost:5440/heya?sslmode=disable"
	}
	if cfg.Host == "" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.LogFormat == "" {
		cfg.LogFormat = "console"
	}
	if cfg.HeyaMediaURL == "" {
		cfg.HeyaMediaURL = "https://heya.media"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
	}
	if cfg.TranscodeCacheDir == "" {
		cfg.TranscodeCacheDir = cfg.DataDir + "/transcode"
	}
	if cfg.TranscodeCacheMaxGB == 0 {
		cfg.TranscodeCacheMaxGB = 50
	}
	if cfg.HWAccel == "" {
		cfg.HWAccel = "auto"
	}
	if cfg.Tailscale.Hostname == "" {
		cfg.Tailscale.Hostname = "heya"
	}
	if cfg.Tailscale.StateDir == "" {
		cfg.Tailscale.StateDir = cfg.DataDir + "/tailscale"
	}
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}

func (c *Config) ToFileConfig() *FileConfig {
	return &FileConfig{
		DatabaseURL:  c.DatabaseURL,
		Host:         c.Host,
		Port:         c.Port,
		LogLevel:     c.LogLevel,
		LogFormat:    c.LogFormat,
		HeyaMediaURL: c.HeyaMediaURL,
		DataDir:      c.DataDir,
		HWAccel:      c.HWAccel,
		Tailscale: FileTailscaleConfig{
			Enabled:  c.Tailscale.Enabled,
			Hostname: c.Tailscale.Hostname,
			StateDir: c.Tailscale.StateDir,
			HTTPS:    c.Tailscale.HTTPS,
			Funnel:   c.Tailscale.Funnel,
		},
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
