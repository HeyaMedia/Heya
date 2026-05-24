package service

import "fmt"

// ErrFieldLockedByEnv is returned by setting-writers when the caller tries
// to change a field whose effective value is sourced from an env var.
// Handlers translate this to HTTP 409 Conflict.
type ErrFieldLockedByEnv struct {
	Field  string // dotted key, e.g. "tailscale.enabled"
	EnvVar string // the env var that locked it
}

func (e *ErrFieldLockedByEnv) Error() string {
	return fmt.Sprintf("field %s is locked by environment variable %s", e.Field, e.EnvVar)
}
