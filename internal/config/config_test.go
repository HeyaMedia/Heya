package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaults(t *testing.T) {
	for _, key := range []string{"DATABASE_URL", "HOST", "PORT", "LOG_LEVEL", "LOG_FORMAT", "TMDB_API_TOKEN", "DATA_DIR"} {
		t.Setenv(key, "")
	}

	cfg := &Config{}
	applyDefaults(cfg)

	assert.Equal(t, "postgres://kura:kura@localhost:5440/kura?sslmode=disable", cfg.DatabaseURL)
	assert.Equal(t, "0.0.0.0", cfg.Host)
	assert.Equal(t, "8080", cfg.Port)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "console", cfg.LogFormat)
	assert.Equal(t, "./data", cfg.DataDir)
}

func TestAddr(t *testing.T) {
	cfg := &Config{Host: "127.0.0.1", Port: "3000"}
	assert.Equal(t, "127.0.0.1:3000", cfg.Addr())
}

func TestMergeFileWithEnvFileValues(t *testing.T) {
	for _, key := range []string{"DATABASE_URL", "HOST", "PORT", "LOG_LEVEL", "LOG_FORMAT", "TMDB_API_TOKEN", "DATA_DIR"} {
		t.Setenv(key, "")
	}

	fc := &FileConfig{
		Host:     "custom-host",
		Port:     "9090",
		LogLevel: "debug",
	}
	cfg := MergeFileWithEnv(fc)

	assert.Equal(t, "custom-host", cfg.Host)
	assert.Equal(t, "9090", cfg.Port)
	assert.Equal(t, "debug", cfg.LogLevel)
}

func TestMergeFileWithEnvEnvOverrides(t *testing.T) {
	t.Setenv("HOST", "env-host")
	t.Setenv("PORT", "7777")

	fc := &FileConfig{
		Host: "file-host",
		Port: "9090",
	}
	cfg := MergeFileWithEnv(fc)

	assert.Equal(t, "env-host", cfg.Host)
	assert.Equal(t, "7777", cfg.Port)
}

func TestLoadFileSaveFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	original := &FileConfig{
		Host:      "myhost",
		Port:      "4000",
		LogLevel:  "warn",
		LogFormat: "json",
		DataDir:   "/tmp/data",
	}

	err := SaveFile(path, original)
	require.NoError(t, err)

	loaded, err := LoadFile(path)
	require.NoError(t, err)

	assert.Equal(t, original.Host, loaded.Host)
	assert.Equal(t, original.Port, loaded.Port)
	assert.Equal(t, original.LogLevel, loaded.LogLevel)
	assert.Equal(t, original.LogFormat, loaded.LogFormat)
	assert.Equal(t, original.DataDir, loaded.DataDir)
}

func TestLoadFileNotFound(t *testing.T) {
	_, err := LoadFile("/nonexistent/path.yaml")
	assert.Error(t, err)
}

func TestSources(t *testing.T) {
	for _, key := range []string{"DATABASE_URL", "HOST", "PORT", "LOG_LEVEL", "LOG_FORMAT", "TMDB_API_TOKEN", "DATA_DIR"} {
		t.Setenv(key, "")
	}

	cfg := &Config{}
	applyDefaults(cfg)
	sources := cfg.Sources()

	assert.Equal(t, "default", sources["host"])
	assert.Equal(t, "default", sources["port"])
}

func TestSourcesEnv(t *testing.T) {
	t.Setenv("HOST", "from-env")

	cfg := &Config{Host: "from-env"}
	applyDefaults(cfg)
	sources := cfg.Sources()

	assert.Equal(t, "env", sources["host"])
}

func TestToFileConfig(t *testing.T) {
	cfg := &Config{
		Host:     "h",
		Port:     "p",
		LogLevel: "l",
		DataDir:  "d",
	}
	fc := cfg.ToFileConfig()
	assert.Equal(t, "h", fc.Host)
	assert.Equal(t, "p", fc.Port)
	assert.Equal(t, "d", fc.DataDir)
}

func TestSaveFileCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "nested", "config.yaml")

	err := SaveFile(path, &FileConfig{Host: "test"})
	require.NoError(t, err)

	_, err = os.Stat(path)
	assert.NoError(t, err)
}
