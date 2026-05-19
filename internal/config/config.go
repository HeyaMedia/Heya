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
	return &Config{
		DatabaseURL: getenv("DATABASE_URL", "postgres://kura:kura@localhost:5440/kura?sslmode=disable"),
		Host:        getenv("HOST", "0.0.0.0"),
		Port:        getenv("PORT", "8080"),
		LogLevel:    getenv("LOG_LEVEL", "info"),
		LogFormat:   getenv("LOG_FORMAT", "console"),
		TMDBToken:   getenv("TMDB_API_TOKEN", ""),
		DataDir:     getenv("DATA_DIR", "./data"),
	}
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
