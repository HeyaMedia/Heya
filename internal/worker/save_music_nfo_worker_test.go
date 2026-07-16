package worker

import (
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

func TestMusicArtistDirMatches(t *testing.T) {
	artist := sqlc.Artist{
		Name:     "Asaco",
		SortName: "Asaco",
		Aliases:  []string{"Asako"},
	}
	for _, dir := range []string{
		"/storage/NewMusic/Asaco",
		"/storage/NewMusic/Asaco (Japanese artist)",
		"/storage/NewMusic/Asako",
	} {
		if !musicArtistDirMatches(dir, artist) {
			t.Errorf("expected matching artist directory: %s", dir)
		}
	}
	if musicArtistDirMatches("/storage/NewMusic/DJ Paul", artist) {
		t.Fatal("different artist directory was accepted")
	}
	if musicArtistDirMatches("/storage/NewMusic", artist) {
		t.Fatal("library root was accepted as the artist directory")
	}
}
