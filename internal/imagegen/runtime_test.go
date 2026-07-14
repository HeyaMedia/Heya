package imagegen

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogDefaultsToZImage(t *testing.T) {
	m, ok := ModelByID(DefaultModel)
	require.True(t, ok)
	require.Equal(t, "z-image-turbo-q4", m.ID)
	require.Len(t, m.Artifacts, 3)
	require.Equal(t, int64(6696835812), m.DownloadSize())
	require.Equal(t, MemoryModeLowVRAM, m.DefaultMemoryMode)

	sd15, ok := ModelByID("stable-diffusion-1.5-q4")
	require.True(t, ok)
	require.Equal(t, int64(1566768416), sd15.DownloadSize())
	require.Equal(t, "model", sd15.Artifacts[0].Role)
	require.Equal(t, MemoryModeAuto, sd15.DefaultMemoryMode)
	require.Equal(t, 512, sd15.DefaultWidth)
}

func TestAutoBackendInheritsRuntimeImageBackend(t *testing.T) {
	t.Setenv("HEYA_IMAGE_BACKEND", "")
	t.Setenv("HEYA_AI_LOCAL_BACKEND", BackendVulkan)
	require.Equal(t, BackendVulkan, ResolveBackend(BackendAuto))
	t.Setenv("HEYA_IMAGE_BACKEND", BackendROCm)
	require.Equal(t, BackendROCm, ResolveBackend(BackendAuto))
}

func TestReusesExactLLMArtifact(t *testing.T) {
	data := t.TempDir()
	r := NewRuntime(filepath.Join(data, "imagegen"))
	m, _ := ModelByID(DefaultModel)
	llm := m.Artifacts[1]
	shared := filepath.Join(data, "llm", "models", llm.SharedLLMFile)
	require.NoError(t, os.MkdirAll(filepath.Dir(shared), 0750))
	f, err := os.Create(shared)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(llm.Size))
	require.NoError(t, f.Close())
	require.Equal(t, shared, r.artifactPath(llm))
}

func TestStatusAndGenerateDoNotCreateOrDownloadArtifacts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "never-created")
	r := NewRuntime(dir)
	require.False(t, r.ModelPresent(DefaultModel))
	require.False(t, r.RuntimePresent(BackendAuto))
	state, progress, downloadErr := r.DownloadStatus()
	require.Equal(t, DownloadIdle, state)
	require.Nil(t, progress)
	require.Empty(t, downloadErr)
	_, err := r.Generate(context.Background(), Request{ModelID: DefaultModel, Backend: BackendAuto, Prompt: "test"})
	require.ErrorContains(t, err, "not downloaded")
	_, err = os.Stat(dir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestParseDevices(t *testing.T) {
	devices := parseDevices([]byte("Vulkan0\tIntel Arc A380\nVulkan1\tAMD Radeon Graphics\nCPU\tx86_64 CPU\n"))
	require.Equal(t, []ComputeDevice{
		{Name: "Vulkan0", Description: "Intel Arc A380"},
		{Name: "Vulkan1", Description: "AMD Radeon Graphics"},
		{Name: "CPU", Description: "x86_64 CPU"},
	}, devices)
}

func TestGenerationDeviceArgs(t *testing.T) {
	devices := []ComputeDevice{{Name: "Vulkan0", Description: "Intel Arc"}, {Name: "Vulkan1", Description: "AMD Radeon"}}
	require.Equal(t, []string{"--auto-fit"}, mustDeviceArgs(t, "auto", MemoryModeAuto, devices))
	require.Equal(t, []string{"--auto-fit"}, mustDeviceArgs(t, "", MemoryModeAuto, devices))
	require.Equal(t, []string{"--backend", "Vulkan1"}, mustDeviceArgs(t, "vulkan1", MemoryModeAuto, devices))
	require.Equal(t, []string{"--offload-to-cpu", "--max-vram", "-1", "--stream-layers"}, mustDeviceArgs(t, "auto", MemoryModeLowVRAM, devices))
	require.Equal(t, []string{"--offload-to-cpu", "--max-vram", "-1", "--stream-layers", "--backend", "Vulkan1"}, mustDeviceArgs(t, "vulkan1", MemoryModeLowVRAM, devices))
	_, err := generationDeviceArgs("Vulkan2", MemoryModeAuto, devices)
	require.ErrorContains(t, err, `unknown compute device "Vulkan2"`)
	_, err = generationDeviceArgs("auto", "tiny", devices)
	require.ErrorContains(t, err, `unknown memory mode "tiny"`)
}

func mustDeviceArgs(t *testing.T, requested, memoryMode string, devices []ComputeDevice) []string {
	t.Helper()
	args, err := generationDeviceArgs(requested, memoryMode, devices)
	require.NoError(t, err)
	return args
}

func TestExtractZipFlattenFindsCLI(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "runtime.zip")
	f, err := os.Create(archive)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	name := "bundle/bin/sd-cli"
	if runtimeBinaryName() == "sd-cli.exe" {
		name += ".exe"
	}
	w, err := zw.Create(name)
	require.NoError(t, err)
	_, err = w.Write([]byte("binary"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
	dest := filepath.Join(dir, "out")
	require.NoError(t, extractZipFlatten(archive, dest))
	_, err = os.Stat(filepath.Join(dest, runtimeBinaryName()))
	require.NoError(t, err)
}

func runtimeBinaryName() string {
	if filepath.Separator == '\\' {
		return "sd-cli.exe"
	}
	return "sd-cli"
}
