package cast

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// The cliap2 AirPlay 2 sender is embedded per-platform and extracted to
// <DataDir>/cast/bin on first use (same shape as the llama-server bundle
// under <DataDir>/llm/server). Provenance + license + update procedure:
// internal/cast/bin/SOURCES.md.
//
//go:embed bin/cliap2-*
var cliap2FS embed.FS

// cliap2AssetName maps GOOS/GOARCH onto the embedded artifact names
// (which follow upstream CI naming, hence "aarch64" on linux but
// "arm64" on macOS).
func cliap2AssetName() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return "cliap2-macos-arm64", nil
	case "darwin/amd64":
		return "cliap2-macos-x86_64", nil
	case "linux/amd64":
		return "cliap2-linux-x86_64", nil
	case "linux/arm64":
		return "cliap2-linux-aarch64", nil
	}
	return "", fmt.Errorf("cast: no cliap2 build for %s/%s", runtime.GOOS, runtime.GOARCH)
}

// ensureCliap2 resolves the sender binary. A $PATH-provided cliap2 wins
// (the container images compile one against their own distro's shared
// libs — the embedded CI builds target Debian Bookworm sonames and
// won't load on trixie); the embedded copy is the fallback for dev
// boxes and bare-metal installs, extracted into binDir (idempotent —
// re-extracts only when the size differs, e.g. after an upstream bump).
func ensureCliap2(binDir string) (string, error) {
	if path, err := exec.LookPath("cliap2"); err == nil {
		return path, nil
	}
	return extractCliap2(binDir)
}

func extractCliap2(binDir string) (string, error) {
	name, err := cliap2AssetName()
	if err != nil {
		return "", err
	}
	data, err := cliap2FS.ReadFile("bin/" + name)
	if err != nil {
		return "", fmt.Errorf("cast: embedded cliap2 missing: %w", err)
	}
	dest := filepath.Join(binDir, name)
	if st, err := os.Stat(dest); err == nil && st.Size() == int64(len(data)) {
		return dest, nil
	}
	if err := os.MkdirAll(binDir, 0o750); err != nil {
		return "", err
	}
	// Write-to-temp + rename so a concurrent spawn never sees a torn
	// binary (ETXTBSY on an exec'd file is also avoided by the rename).
	tmp, err := os.CreateTemp(binDir, name+".tmp-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil { //nolint:gosec // extracted sender must be executable
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return dest, nil
}
