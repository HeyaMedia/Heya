package service

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckedImportDestination(t *testing.T) {
	root := filepath.Clean("/storage/Music")
	got, err := checkedImportDestination(root, "/storage/Music/Sabrina Carpenter/emails i can't send (2022)")
	require.NoError(t, err)
	require.Equal(t, "/storage/Music/Sabrina Carpenter/emails i can't send (2022)", got)

	_, err = checkedImportDestination(root, "/storage/emails i can't send")
	require.Error(t, err)
}

func TestExistingMusicAlbumDir(t *testing.T) {
	artist := "/storage/Music/Sabrina Carpenter"
	want := artist + "/Sabrina Carpenter - Album - 2022 - emails i can’t send"
	paths := []string{
		want + "/01 - emails i can’t send.flac",
		want + "/02 - Vicious.flac",
		"/storage/NewMusic/Sabrina Carpenter/emails i can’t send/01.flac",
	}
	require.Equal(t, want, existingMusicAlbumDir(paths, artist, "emails i can't send", "2022"))
}
