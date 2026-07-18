package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateLibraryPathsAcceptsMountedFilesystemDirectory(t *testing.T) {
	assert.NoError(t, validateLibraryPaths([]string{t.TempDir()}))
}

func TestValidateLibraryPathsRejectsInvalidPathConfigurations(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "media.mkv")
	require.NoError(t, os.WriteFile(file, []byte("media"), 0o600))

	tests := []struct {
		name string
		path []string
		want string
	}{
		{name: "empty list", want: "at least one filesystem path"},
		{name: "relative path", path: []string{"media"}, want: "must be absolute"},
		{name: "URL path", path: []string{"https://example.test/media"}, want: "URL-style library paths are not supported"},
		{name: "legacy SMB path", path: []string{"smb://reader:super-secret@nas/media"}, want: "mount the share"},
		{name: "missing directory", path: []string{filepath.Join(dir, "missing")}, want: "no such file"},
		{name: "regular file", path: []string{file}, want: "is not a directory"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLibraryPaths(tt.path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.NotContains(t, err.Error(), "super-secret")
		})
	}
}
