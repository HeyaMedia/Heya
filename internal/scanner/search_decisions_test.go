package scanner

import (
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

func TestSearchMatcherRevisionIsDomainSpecific(t *testing.T) {
	t.Parallel()

	if got := searchMatcherRevision(sqlc.MediaTypeMusic); got != scannerMusicSearchMatcherRevision {
		t.Fatalf("music matcher revision = %d, want %d", got, scannerMusicSearchMatcherRevision)
	}
	for _, mediaType := range []sqlc.MediaType{
		sqlc.MediaTypeMovie,
		sqlc.MediaTypeTv,
		sqlc.MediaTypeAnime,
		sqlc.MediaTypeBook,
	} {
		if got := searchMatcherRevision(mediaType); got != scannerSearchMatcherRevision {
			t.Fatalf("%s matcher revision = %d, want %d", mediaType, got, scannerSearchMatcherRevision)
		}
	}
}
