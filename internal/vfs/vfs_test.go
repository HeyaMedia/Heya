package vfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateLocalPath(t *testing.T) {
	for _, path := range []string{"/mnt/media", t.TempDir()} {
		assert.NoError(t, ValidateLocalPath(path), path)
	}
	assert.ErrorContains(t, ValidateLocalPath("./media"), "must be absolute")

	for _, path := range []string{
		"smb://reader:secret@nas/media",
		"SMB://reader:secret@nas/media",
		"https://reader:secret@example.test/media",
	} {
		err := ValidateLocalPath(path)
		require.ErrorIs(t, err, ErrUnsupportedPathScheme)
		assert.NotContains(t, err.Error(), "secret")
	}
	assert.Contains(t, ValidateLocalPath("smb://nas/media").Error(), "mount the share")
	assert.Error(t, ValidateLocalPath(""))
	assert.ErrorContains(t, ValidateLocalPath(" /mnt/media "), "surrounding whitespace")
}

func TestOpenAPIsRejectURLPathsBeforeFilesystemAccess(t *testing.T) {
	path := "smb://reader:secret@nas/media/file.mkv"
	_, err := Open(path)
	require.ErrorIs(t, err, ErrUnsupportedPathScheme)
	assert.NotContains(t, err.Error(), "secret")
	_, err = OpenFile(path)
	require.ErrorIs(t, err, ErrUnsupportedPathScheme)
}

func TestRedactPath(t *testing.T) {
	assert.Equal(t, "/local/path", RedactPath("/local/path"))
	assert.Equal(t, "https://xxxxx@example.test/media", RedactPath("https://user:pass@example.test/media"))
}

func TestUnsupportedPathErrorCategory(t *testing.T) {
	err := ValidateLocalPath("ftp://reader:secret@example.test/media")
	assert.True(t, errors.Is(err, ErrUnsupportedPathScheme))
}

func TestOpenLocalValid(t *testing.T) {
	dir := t.TempDir()
	src, err := openLocal(dir)
	require.NoError(t, err)
	assert.NotNil(t, src.FS)
	assert.Equal(t, dir, src.RootPath)
}

func TestOpenLocalNotExist(t *testing.T) {
	_, err := openLocal("/nonexistent/path/12345")
	assert.Error(t, err)
}

func TestOpenLocalFileNotDir(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(f, []byte("hi"), 0o644))

	_, err := openLocal(f)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}
