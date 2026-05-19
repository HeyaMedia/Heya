package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type FileConfig struct {
	DatabaseURL string `yaml:"database_url,omitempty"`
	Host        string `yaml:"host,omitempty"`
	Port        string `yaml:"port,omitempty"`
	LogLevel    string `yaml:"log_level,omitempty"`
	LogFormat   string `yaml:"log_format,omitempty"`
	TMDBToken   string `yaml:"tmdb_api_token,omitempty"`
	DataDir     string `yaml:"data_dir,omitempty"`
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

func MergeFileWithEnv(fc *FileConfig) *Config {
	cfg := &Config{
		DatabaseURL: fc.DatabaseURL,
		Host:        fc.Host,
		Port:        fc.Port,
		LogLevel:    fc.LogLevel,
		LogFormat:   fc.LogFormat,
		TMDBToken:   fc.TMDBToken,
		DataDir:     fc.DataDir,
	}

	if v := os.Getenv("DATABASE_URL"); v != "" {
		cfg.DatabaseURL = v
	}
	if v := os.Getenv("HOST"); v != "" {
		cfg.Host = v
	}
	if v := os.Getenv("PORT"); v != "" {
		cfg.Port = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		cfg.LogFormat = v
	}
	if v := os.Getenv("TMDB_API_TOKEN"); v != "" {
		cfg.TMDBToken = v
	}
	if v := os.Getenv("DATA_DIR"); v != "" {
		cfg.DataDir = v
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
		key    string
		envKey string
		val    string
		fileVal string
		defVal string
	}{
		{"database_url", "DATABASE_URL", c.DatabaseURL, fileStr(fc, "database_url"), "postgres://kura:kura@localhost:5440/kura?sslmode=disable"},
		{"host", "HOST", c.Host, fileStr(fc, "host"), "0.0.0.0"},
		{"port", "PORT", c.Port, fileStr(fc, "port"), "8080"},
		{"log_level", "LOG_LEVEL", c.LogLevel, fileStr(fc, "log_level"), "info"},
		{"log_format", "LOG_FORMAT", c.LogFormat, fileStr(fc, "log_format"), "console"},
		{"tmdb_api_token", "TMDB_API_TOKEN", c.TMDBToken, fileStr(fc, "tmdb_api_token"), ""},
		{"data_dir", "DATA_DIR", c.DataDir, fileStr(fc, "data_dir"), "./data"},
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
	case "tmdb_api_token":
		return fc.TMDBToken
	case "data_dir":
		return fc.DataDir
	}
	return ""
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}
