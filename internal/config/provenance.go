package config

import (
	"os"
	"strconv"
	"strings"
)

// Source identifies where a configuration value came from. Used by the
// /api/config/sources endpoint so the settings UI can disable any control
// whose value was set via env (and therefore can't be changed at runtime).
type Source string

const (
	SourceDefault Source = "default"
	SourceEnv     Source = "env"
	SourceDB      Source = "db"
)

// Field wraps a configuration value with its provenance. EnvVar is set only
// when Source == SourceEnv, so the UI can show "Locked by HEYA_FOO" tooltips.
type Field[T any] struct {
	Value  T      `json:"value"`
	Source Source `json:"source"`
	EnvVar string `json:"env_var,omitempty"`
}

// SourceEntry is the flat shape returned by /api/config/sources. It carries
// only provenance, not the value (values flow through their normal payloads).
type SourceEntry struct {
	Source Source `json:"source"`
	EnvVar string `json:"env_var,omitempty"`
}

// Entry returns this field's provenance in SourceEntry form.
func (f Field[T]) Entry() SourceEntry {
	return SourceEntry{Source: f.Source, EnvVar: f.EnvVar}
}

func (f Field[T]) EnvLock() (envVar string, locked bool) {
	if f.Source != SourceEnv {
		return "", false
	}
	return f.EnvVar, true
}

func envString(envVar, def string) Field[string] {
	if v, ok := os.LookupEnv(envVar); ok {
		return Field[string]{Value: v, Source: SourceEnv, EnvVar: envVar}
	}
	return Field[string]{Value: def, Source: SourceDefault}
}

func envBool(envVar string, def bool) Field[bool] {
	if v, ok := os.LookupEnv(envVar); ok {
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			b = def
		}
		return Field[bool]{Value: b, Source: SourceEnv, EnvVar: envVar}
	}
	return Field[bool]{Value: def, Source: SourceDefault}
}

func envInt(envVar string, def int) Field[int] {
	if v, ok := os.LookupEnv(envVar); ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			n = def
		}
		return Field[int]{Value: n, Source: SourceEnv, EnvVar: envVar}
	}
	return Field[int]{Value: def, Source: SourceDefault}
}
