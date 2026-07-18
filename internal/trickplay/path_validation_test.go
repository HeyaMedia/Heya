package trickplay

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/karbowiak/heya/internal/vfs"
	"github.com/stretchr/testify/require"
)

func TestGeneratorsRejectURLInputsBeforeStartingFFmpeg(t *testing.T) {
	input := "https://reader:super-secret@storage.test/media/movie.mkv"

	_, err := GenerateSprites(context.Background(), input, 120, t.TempDir())
	require.ErrorIs(t, err, vfs.ErrUnsupportedPathScheme)
	require.NotContains(t, err.Error(), "super-secret")

	err = ExtractThumbnail(context.Background(), input, 120_000, filepath.Join(t.TempDir(), "thumb.jpg"))
	require.ErrorIs(t, err, vfs.ErrUnsupportedPathScheme)
	require.NotContains(t, err.Error(), "super-secret")
}
