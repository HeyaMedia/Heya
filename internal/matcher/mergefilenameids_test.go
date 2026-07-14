package matcher

import (
	"testing"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/parser"
)

func TestMergeFilenameIDs(t *testing.T) {
	// Filename-only ID must carry id + title + year so unified discovery gets
	// both exact identifier evidence and useful corroborating hints.
	got := mergeFilenameIDs(nil, &parser.SceneReleaseParse{Title: "A Goofy Movie", Year: "1995", ImdbID: "tt0113198"})
	if got == nil {
		t.Fatal("expected non-nil ids for a filename with an embedded id")
	}
	if got.IMDBID != "tt0113198" || got.Title != "A Goofy Movie" || got.Year != "1995" {
		t.Errorf("got %+v; want imdb/title/year populated for the stub path", got)
	}

	// NFO wins; filename only fills the gaps it left.
	existing := &metadata.NFOIDs{IMDBID: "tt9999999", Title: "NFO Title"}
	got2 := mergeFilenameIDs(existing, &parser.SceneReleaseParse{Title: "Filename Title", Year: "2000", ImdbID: "tt0113198", TmdbID: "603"})
	if got2.IMDBID != "tt9999999" {
		t.Errorf("NFO imdb must win, got %q", got2.IMDBID)
	}
	if got2.Title != "NFO Title" {
		t.Errorf("NFO title must win, got %q", got2.Title)
	}
	if got2.TMDBID != "603" || got2.Year != "2000" {
		t.Errorf("filename must fill the missing tmdb/year, got tmdb=%q year=%q", got2.TMDBID, got2.Year)
	}

	// No filename ID → don't synthesize an NFOIDs (nil stays nil).
	if mergeFilenameIDs(nil, &parser.SceneReleaseParse{Title: "X"}) != nil {
		t.Error("no filename ID must not synthesize an NFOIDs")
	}
}

func TestNFOIdentifierEvidenceKeepsEveryKnownID(t *testing.T) {
	ids := &metadata.NFOIDs{
		TMDBID: "1396", IMDBID: "tt0903747", TVDBID: "81189",
		AniDBID: "123", MALID: "456", MBID: "artist-mbid",
	}
	got := nfoIdentifierEvidence(ids)
	for key, want := range map[string]string{
		"tmdb": "1396", "imdb": "tt0903747", "tvdb": "81189",
		"anidb": "123", "myanimelist": "456", "musicbrainz": "artist-mbid",
	} {
		if got[key] != want {
			t.Fatalf("identifier %s = %q, want %q (all: %#v)", key, got[key], want, got)
		}
	}
}
