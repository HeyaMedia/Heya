package service

import (
	"testing"

	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
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
