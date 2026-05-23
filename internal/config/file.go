package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type FileConfig struct {
	DatabaseURL  string              `yaml:"database_url,omitempty"`
	Host         string              `yaml:"host,omitempty"`
	Port         string              `yaml:"port,omitempty"`
	LogLevel     string              `yaml:"log_level,omitempty"`
	LogFormat    string              `yaml:"log_format,omitempty"`
	HeyaMediaURL string              `yaml:"heya_media_url,omitempty"`
	DataDir      string              `yaml:"data_dir,omitempty"`
	HWAccel      string              `yaml:"hw_accel,omitempty"`
	Tailscale    FileTailscaleConfig `yaml:"tailscale,omitempty"`
}

type FileTailscaleConfig struct {
	Enabled  bool   `yaml:"enabled,omitempty"`
	Hostname string `yaml:"hostname,omitempty"`
	StateDir string `yaml:"state_dir,omitempty"`
	HTTPS    bool   `yaml:"https,omitempty"`
	Funnel   bool   `yaml:"funnel,omitempty"`
}

var searchPaths = []string{
	"./heya.yaml",
	filepath.Join(homeDir(), ".config", "kura", "heya.yaml"),
	"/etc/heya/heya.yaml",
}

func FindConfigFile() string {
	for _, p := range searchPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func LoadFile(path string) (*FileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fc FileConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return nil, err
	}
	return &fc, nil
}

func SaveFile(path string, fc *FileConfig) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(fc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// SaveTailscale writes the tailscale block of heya.yaml in-place, preserving
// every other field already on disk. The auth key is intentionally never
// written here — it stays env-only so secrets don't land in YAML.
func SaveTailscale(ts TailscaleConfig) error {
	path := FindConfigFile()
	if path == "" {
		path = "./heya.yaml"
	}
	var fc *FileConfig
	if existing, err := LoadFile(path); err == nil {
		fc = existing
	} else {
		fc = &FileConfig{}
	}
	fc.Tailscale = FileTailscaleConfig{
		Enabled:  ts.Enabled,
		Hostname: ts.Hostname,
		StateDir: ts.StateDir,
		HTTPS:    ts.HTTPS,
		Funnel:   ts.Funnel,
	}
	return SaveFile(path, fc)
}

func MergeFileWithEnv(fc *FileConfig) *Config {
	cfg := &Config{
		DatabaseURL:  fc.DatabaseURL,
		Host:         fc.Host,
		Port:         fc.Port,
		LogLevel:     fc.LogLevel,
		LogFormat:    fc.LogFormat,
		HeyaMediaURL: fc.HeyaMediaURL,
		DataDir:      fc.DataDir,
		HWAccel:      fc.HWAccel,
		Tailscale: TailscaleConfig{
			Enabled:  fc.Tailscale.Enabled,
			Hostname: fc.Tailscale.Hostname,
			StateDir: fc.Tailscale.StateDir,
			HTTPS:    fc.Tailscale.HTTPS,
			Funnel:   fc.Tailscale.Funnel,
		},
	}

	envOverrides := []struct {
		key string
		dst *string
	}{
		{"DATABASE_URL", &cfg.DatabaseURL},
		{"HOST", &cfg.Host},
		{"PORT", &cfg.Port},
		{"LOG_LEVEL", &cfg.LogLevel},
		{"LOG_FORMAT", &cfg.LogFormat},
		{"HEYA_MEDIA_URL", &cfg.HeyaMediaURL},
		{"DATA_DIR", &cfg.DataDir},
		{"HEYA_HWACCEL", &cfg.HWAccel},
		{"HEYA_TAILSCALE_HOSTNAME", &cfg.Tailscale.Hostname},
		{"HEYA_TAILSCALE_STATE_DIR", &cfg.Tailscale.StateDir},
	}
	for _, o := range envOverrides {
		if v := os.Getenv(o.key); v != "" {
			*o.dst = v
		}
	}

	if v := os.Getenv("HEYA_TAILSCALE_ENABLED"); v != "" {
		cfg.Tailscale.Enabled = v == "true"
	}
	if v := os.Getenv("HEYA_TAILSCALE_HTTPS"); v != "" {
		cfg.Tailscale.HTTPS = v != "false"
	}
	if v := os.Getenv("HEYA_TAILSCALE_FUNNEL"); v != "" {
		cfg.Tailscale.Funnel = v == "true"
	}
	if v := os.Getenv("HEYA_TAILSCALE_AUTHKEY"); v != "" {
		cfg.Tailscale.AuthKey = v
	}

	applyDefaults(cfg)
	return cfg
}

func (c *Config) Sources() map[string]string {
	sources := make(map[string]string)

	file := FindConfigFile()
	var fc *FileConfig
	if file != "" {
		fc, _ = LoadFile(file)
	}

	fields := []struct {
		key     string
		envKey  string
		val     string
		fileVal string
		defVal  string
	}{
		{"database_url", "DATABASE_URL", c.DatabaseURL, fileStr(fc, "database_url"), "postgres://kura:kura@localhost:5440/kura?sslmode=disable"},
		{"host", "HOST", c.Host, fileStr(fc, "host"), "0.0.0.0"},
		{"port", "PORT", c.Port, fileStr(fc, "port"), "8080"},
		{"log_level", "LOG_LEVEL", c.LogLevel, fileStr(fc, "log_level"), "info"},
		{"log_format", "LOG_FORMAT", c.LogFormat, fileStr(fc, "log_format"), "console"},
		{"heya_media_url", "HEYA_MEDIA_URL", c.HeyaMediaURL, fileStr(fc, "heya_media_url"), "https://heya.media"},
		{"data_dir", "DATA_DIR", c.DataDir, fileStr(fc, "data_dir"), "./data"},
		{"hw_accel", "HEYA_HWACCEL", c.HWAccel, fileStr(fc, "hw_accel"), "auto"},
	}

	for _, f := range fields {
		if os.Getenv(f.envKey) != "" {
			sources[f.key] = "env"
		} else if fc != nil && f.fileVal != "" {
			sources[f.key] = "file (" + file + ")"
		} else {
			sources[f.key] = "default"
		}
	}

	return sources
}

func fileStr(fc *FileConfig, key string) string {
	if fc == nil {
		return ""
	}
	switch key {
	case "database_url":
		return fc.DatabaseURL
	case "host":
		return fc.Host
	case "port":
		return fc.Port
	case "log_level":
		return fc.LogLevel
	case "log_format":
		return fc.LogFormat
	case "heya_media_url":
		return fc.HeyaMediaURL
	case "data_dir":
		return fc.DataDir
	case "hw_accel":
		return fc.HWAccel
	}
	return ""
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}
