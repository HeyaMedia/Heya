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
