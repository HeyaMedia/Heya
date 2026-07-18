package cast

import (
	"context"
	"testing"

	"github.com/karbowiak/heya/internal/vfs"
	"github.com/stretchr/testify/require"
)

func TestPCMFeederRejectsURLInputBeforeStartingFFmpeg(t *testing.T) {
	_, err := newPCMFeeder(context.Background(), TrackInfo{
		Path: "https://reader:super-secret@storage.test/music/track.flac",
	})
	require.ErrorIs(t, err, vfs.ErrUnsupportedPathScheme)
	require.NotContains(t, err.Error(), "super-secret")
}
