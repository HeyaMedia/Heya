package transcoder

import (
	"context"
	"testing"

	"github.com/karbowiak/heya/internal/vfs"
	"github.com/stretchr/testify/require"
)

func TestFFmpegEntryPointsRejectURLInputsBeforeStartingProcess(t *testing.T) {
	input := "https://reader:super-secret@storage.test/media/movie.mkv"

	err := TranscodeToHLSWithOpts(context.Background(), TranscodeOpts{
		Input:     input,
		OutputDir: t.TempDir(),
		Profile:   Profile{Name: "test"},
	})
	require.ErrorIs(t, err, vfs.ErrUnsupportedPathScheme)
	require.NotContains(t, err.Error(), "super-secret")

	err = ExtractSubtitlesAs(context.Background(), input, 0, t.TempDir()+"/subtitle.vtt", "webvtt")
	require.ErrorIs(t, err, vfs.ErrUnsupportedPathScheme)
	require.NotContains(t, err.Error(), "super-secret")

	_, err = NewFFmpegBuilder().BuildHLSCommand(context.Background(), TranscodeOpts{
		Input:     input,
		OutputDir: t.TempDir(),
	})
	require.ErrorIs(t, err, vfs.ErrUnsupportedPathScheme)
	require.NotContains(t, err.Error(), "super-secret")
}
