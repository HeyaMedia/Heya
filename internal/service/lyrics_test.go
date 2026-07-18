package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/stretchr/testify/require"
)

func TestPreferredRecordingLyricsPrefersSyncedThenPlain(t *testing.T) {
	body, ok := preferredRecordingLyrics([]heyametadata.RecordingLyrics{
		{PlainLyrics: "newest plain"},
		{PlainLyrics: "older plain", SyncedLyrics: "[00:01.00]older synced"},
	})
	require.True(t, ok)
	require.Equal(t, "[00:01.00]older synced", string(body))

	body, ok = preferredRecordingLyrics([]heyametadata.RecordingLyrics{{PlainLyrics: "plain only"}})
	require.True(t, ok)
	require.Equal(t, "plain only", string(body))

	body, ok = preferredRecordingLyrics([]heyametadata.RecordingLyrics{{Instrumental: true}})
	require.False(t, ok)
	require.Nil(t, body)
}

func TestReadTrackLyricsFileUsesFilesystemPathContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "track.lrc")
	require.NoError(t, os.WriteFile(path, []byte("lyrics"), 0o600))
	body, err := readTrackLyricsFile(context.Background(), path)
	require.NoError(t, err)
	require.Equal(t, "lyrics", string(body))

	_, err = readTrackLyricsFile(context.Background(), "smb://reader:super-secret@nas/music/track.lrc")
	require.ErrorIs(t, err, vfs.ErrUnsupportedPathScheme)
	require.False(t, strings.Contains(err.Error(), "super-secret"), "diagnostic leaked credentials: %v", err)
}
