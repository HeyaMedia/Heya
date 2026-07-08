package mediatype

import (
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/assert"
)

func TestRuntime(t *testing.T) {
	// Anime is the only type that differs at runtime — it resolves to tv so
	// its media_items are queried under the type they're actually stored as.
	assert.Equal(t, sqlc.MediaTypeTv, Runtime(sqlc.MediaTypeAnime))

	// Everything else is identity.
	for _, mt := range []sqlc.MediaType{
		sqlc.MediaTypeMovie, sqlc.MediaTypeTv, sqlc.MediaTypeMusic,
		sqlc.MediaTypeBook, sqlc.MediaTypeComic, sqlc.MediaTypePodcast,
		sqlc.MediaTypeRadio,
	} {
		assert.Equal(t, mt, Runtime(mt), "Runtime(%s) should be identity", mt)
	}
}

func TestIsTVLike(t *testing.T) {
	assert.True(t, IsTVLike(sqlc.MediaTypeTv))
	assert.True(t, IsTVLike(sqlc.MediaTypeAnime))
	assert.False(t, IsTVLike(sqlc.MediaTypeMovie))
	assert.False(t, IsTVLike(sqlc.MediaTypeMusic))
}

func TestIsVideo(t *testing.T) {
	assert.True(t, IsVideo(sqlc.MediaTypeMovie))
	assert.True(t, IsVideo(sqlc.MediaTypeTv))
	assert.True(t, IsVideo(sqlc.MediaTypeAnime))
	assert.False(t, IsVideo(sqlc.MediaTypeMusic))
	assert.False(t, IsVideo(sqlc.MediaTypeBook))
}
