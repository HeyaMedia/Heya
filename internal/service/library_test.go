package service

import (
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseMediaTypeAnime pins that 'anime' is an accepted library type. It
// stays a distinct enum value here — the matcher (not ParseMediaType) is what
// collapses it onto the TV pipeline.
func TestParseMediaTypeAnime(t *testing.T) {
	mt, err := ParseMediaType("anime")
	require.NoError(t, err)
	assert.Equal(t, sqlc.MediaTypeAnime, mt)
}

func TestParseMediaTypeKnownAndUnknown(t *testing.T) {
	for in, want := range map[string]sqlc.MediaType{
		"movie": sqlc.MediaTypeMovie,
		"tv":    sqlc.MediaTypeTv,
		"anime": sqlc.MediaTypeAnime,
		"music": sqlc.MediaTypeMusic,
		"book":  sqlc.MediaTypeBook,
	} {
		got, err := ParseMediaType(in)
		require.NoError(t, err, "ParseMediaType(%q)", in)
		assert.Equal(t, want, got)
	}

	_, err := ParseMediaType("cartoons")
	assert.Error(t, err)
}
