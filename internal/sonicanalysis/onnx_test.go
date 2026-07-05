package sonicanalysis

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestProviderLibInstalled verifies the on-disk gate that keeps the CPU-only
// base image from triggering ONNX Runtime's noisy "Failed to load library
// libonnxruntime_providers_*.so" log when it probes GPU execution providers
// whose provider libraries aren't shipped. The gate is Linux-only (Windows
// providers are DLLs on the loader search path, macOS has no CUDA/OpenVINO),
// so the assertions below only hold there.
func TestProviderLibInstalled(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("provider-lib gate only applies on linux")
	}
	dir := t.TempDir()
	t.Setenv("ONNXRUNTIME_LIB", filepath.Join(dir, "libonnxruntime.so"))

	// CPU / CoreML / DirectML aren't loaded from a sibling .so, so they're
	// never gated regardless of what's on disk.
	for _, a := range []Accelerator{AccelCPU, AccelCoreML, AccelDirectML} {
		if !providerLibInstalled(a) {
			t.Errorf("providerLibInstalled(%s) = false, want true (never gated)", a)
		}
	}

	// CUDA's provider lib is absent → gated off.
	if providerLibInstalled(AccelCUDA) {
		t.Error("providerLibInstalled(cuda) = true with no provider lib on disk, want false")
	}

	// Drop the provider lib next to libonnxruntime.so → now available.
	cuda := filepath.Join(dir, providerLibFiles[AccelCUDA])
	if err := os.WriteFile(cuda, []byte("stub"), 0o644); err != nil {
		t.Fatalf("write stub provider lib: %v", err)
	}
	if !providerLibInstalled(AccelCUDA) {
		t.Error("providerLibInstalled(cuda) = false with provider lib present, want true")
	}
}

// TestBuildSessionOptionsSkipsUninstalledProvider confirms that requesting a
// GPU EP with no provider library installed fails fast with a clear message
// and — crucially — before any ORT call, so no scary provider-load line is
// emitted. This is exactly the base-image path: `auto` and the status probe
// both route through buildSessionOptions.
func TestBuildSessionOptionsSkipsUninstalledProvider(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("provider-lib gate only applies on linux")
	}
	dir := t.TempDir()
	t.Setenv("ONNXRUNTIME_LIB", filepath.Join(dir, "libonnxruntime.so"))

	for _, a := range []Accelerator{AccelCUDA, AccelOpenVINO} {
		_, _, err := buildSessionOptions(a)
		if err == nil {
			t.Errorf("buildSessionOptions(%s) with no provider lib: err = nil, want error", a)
			continue
		}
		if !strings.Contains(err.Error(), "not installed") {
			t.Errorf("buildSessionOptions(%s) error = %q, want it to mention 'not installed'", a, err)
		}
	}
}
