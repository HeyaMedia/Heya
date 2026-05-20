package transcoder

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGeneratePlaylistBasic(t *testing.T) {
	pl := GeneratePlaylist(30.0, "seg_%04d.ts", "abc123")

	assert.Contains(t, pl, "#EXTM3U")
	assert.Contains(t, pl, "#EXT-X-PLAYLIST-TYPE:VOD")
	assert.Contains(t, pl, "#EXT-X-ENDLIST")
	assert.Contains(t, pl, "seg_0000.ts?token=abc123")
	assert.Contains(t, pl, "seg_0004.ts?token=abc123")

	segments := strings.Count(pl, "#EXTINF:")
	assert.Equal(t, 5, segments)
}

func TestGeneratePlaylistDuration(t *testing.T) {
	pl := GeneratePlaylist(4921.067, "seg_%04d.ts", "tok")

	segments := strings.Count(pl, "#EXTINF:")
	assert.Equal(t, 821, segments)
	assert.Contains(t, pl, "#EXT-X-TARGETDURATION:7")
	assert.Contains(t, pl, "seg_0000.ts?token=tok")
	assert.Contains(t, pl, "seg_0820.ts?token=tok")
}

func TestGeneratePlaylistNoToken(t *testing.T) {
	pl := GeneratePlaylist(12.0, "seg_%04d.ts", "")

	assert.Contains(t, pl, "seg_0000.ts\n")
	assert.NotContains(t, pl, "?token=")
}
