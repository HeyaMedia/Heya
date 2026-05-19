package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL string
	Host        string
	Port        string
	LogLevel    string
	LogFormat   string
	TMDBToken   string
	DataDir     string
}

func Load() *Config {
	if path := FindConfigFile(); path != "" {
		if fc, err := LoadFile(path); err == nil {
			return MergeFileWithEnv(fc)
		}
	}

	cfg := &Config{
		DatabaseURL: getenv("DATABASE_URL", ""),
		Host:        getenv("HOST", ""),
		Port:        getenv("PORT", ""),
		LogLevel:    getenv("LOG_LEVEL", ""),
		LogFormat:   getenv("LOG_FORMAT", ""),
		TMDBToken:   getenv("TMDB_API_TOKEN", ""),
		DataDir:     getenv("DATA_DIR", ""),
	}
	applyDefaults(cfg)
	return cfg
}

func applyDefaults(cfg *Config) {
	if cfg.DatabaseURL == "" {
		cfg.DatabaseURL = "postgres://kura:kura@localhost:5440/kura?sslmode=disable"
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
	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
	}
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}

func (c *Config) ToFileConfig() *FileConfig {
	return &FileConfig{
		DatabaseURL: c.DatabaseURL,
		Host:        c.Host,
		Port:        c.Port,
		LogLevel:    c.LogLevel,
		LogFormat:   c.LogFormat,
		TMDBToken:   c.TMDBToken,
		DataDir:     c.DataDir,
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
