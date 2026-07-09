package sonicanalysis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubGateDir points the provider-lib gate at a scratch directory for the
// duration of a test, so the disk logic can be exercised on any OS without
// touching (or writing to) the real system lib path.
func stubGateDir(t *testing.T, dir string) {
	t.Helper()
	orig := gatedProviderDir
	gatedProviderDir = func() string { return dir }
	t.Cleanup(func() { gatedProviderDir = orig })
}

// TestProviderLibGate verifies the on-disk gate that keeps the CPU-only base
// image from triggering ONNX Runtime's noisy "Failed to load library
// libonnxruntime_providers_*.so" log while probing GPU EPs it doesn't ship.
func TestProviderLibGate(t *testing.T) {
	dir := t.TempDir()
	stubGateDir(t, dir)

	// Not loaded from a sibling .so → never gated.
	for _, a := range []Accelerator{AccelCPU, AccelCoreML, AccelDirectML} {
		if !providerLibInstalled(a) {
			t.Errorf("providerLibInstalled(%s) = false, want true (never gated)", a)
		}
	}

	// Provider lib absent (the base CPU image) → gated off.
	if providerLibInstalled(AccelCUDA) {
		t.Error("providerLibInstalled(cuda) = true with no provider lib, want false")
	}

	// Provider lib present (a vendor image) → available.
	if err := os.WriteFile(filepath.Join(dir, providerLibFiles[AccelCUDA]), []byte("stub"), 0o644); err != nil {
		t.Fatalf("write stub provider lib: %v", err)
	}
	if !providerLibInstalled(AccelCUDA) {
		t.Error("providerLibInstalled(cuda) = false with provider lib present, want true")
	}
}

// TestProviderGateDisabledForCustomLib guards the regression Codex flagged:
// when an operator points ONNXRUNTIME_LIB at their own ORT, the provider may
// resolve via LD_LIBRARY_PATH/ldconfig/rpath from a directory we can't see,
// so the gate must defer to ORT rather than falsely disable a working EP.
// (On non-Linux the gate is off unconditionally, which this also covers.)
func TestProviderGateDisabledForCustomLib(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ONNXRUNTIME_LIB", filepath.Join(dir, "libonnxruntime.so"))
	// No provider .so exists beside that path, yet neither GPU EP may be
	// gated off — the override hands provider resolution back to ORT.
	for _, a := range []Accelerator{AccelCUDA, AccelOpenVINO} {
		if !providerLibInstalled(a) {
			t.Errorf("providerLibInstalled(%s) = false under custom ONNXRUNTIME_LIB; must defer to ORT", a)
		}
	}
}

// TestBuildSessionOptionsSkipsUninstalledProvider confirms a GPU EP whose
// provider lib is absent fails fast with a clear message and — crucially —
// before any ORT call, so no provider-load line is emitted. This is the
// base-image path: `auto` and the status probe both route through here.
func TestBuildSessionOptionsSkipsUninstalledProvider(t *testing.T) {
	stubGateDir(t, t.TempDir()) // empty scratch dir: no provider libs present

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

func TestOpenVINOProviderOptionsPrecision(t *testing.T) {
	t.Setenv("HEYA_SONIC_OPENVINO_CACHE_DIR", "/tmp/openvino-cache")

	options, err := openVINOProviderOptions("GPU", "FP32")
	if err != nil {
		t.Fatalf("openVINOProviderOptions: %v", err)
	}
	if got := options["device_type"]; got != "GPU" {
		t.Errorf("device_type = %q, want GPU", got)
	}
	if got := options["precision"]; got != "FP32" {
		t.Errorf("precision = %q, want FP32", got)
	}
	if got := options["cache_dir"]; got != "/tmp/openvino-cache" {
		t.Errorf("cache_dir = %q, want /tmp/openvino-cache", got)
	}

	defaults, err := openVINOProviderOptions("GPU", "")
	if err != nil {
		t.Fatalf("default openVINOProviderOptions: %v", err)
	}
	if _, exists := defaults["precision"]; exists {
		t.Error("default provider options unexpectedly override precision")
	}

	if _, err := openVINOProviderOptions("GPU", "BF16"); err == nil {
		t.Error("unsupported precision accepted")
	}
}
