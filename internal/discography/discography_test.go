package discography

import (
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

func TestMatchLocalAlbumPrecedence(t *testing.T) {
	locals := []sqlc.Album{
		{ID: 1, Title: "Room 93", Year: "2014", MusicbrainzID: "aaa-111"},
		{ID: 2, Title: "Room 93", Year: "2015"},
		{ID: 3, Title: "BADLANDS", Year: "2015", ExternalIds: []byte(`{"mb_release_group":"bbb-222"}`)},
	}

	// External id beats title matching, wherever the id lives locally.
	if got, ok := matchLocalAlbum(metadata.AlbumEntry{Title: "Different Name", ExternalIDs: map[string]string{"mbid": "BBB-222"}}, locals); !ok || got.ID != 3 {
		t.Fatalf("external id match: got %v ok=%v, want album 3", got.ID, ok)
	}
	// Title+year narrows between same-titled releases.
	if got, ok := matchLocalAlbum(metadata.AlbumEntry{Title: "room 93!", Year: 2015}, locals); !ok || got.ID != 2 {
		t.Fatalf("title+year match: got %v ok=%v, want album 2", got.ID, ok)
	}
	// Title alone falls back to the first candidate.
	if got, ok := matchLocalAlbum(metadata.AlbumEntry{Title: "Room 93"}, locals); !ok || got.ID != 1 {
		t.Fatalf("title match: got %v ok=%v, want album 1", got.ID, ok)
	}
	if _, ok := matchLocalAlbum(metadata.AlbumEntry{Title: "Manic"}, locals); ok {
		t.Fatal("unknown title must not match")
	}
}

func TestEditionKeyGroupsVariants(t *testing.T) {
	base := EditionKey("If I Can’t Have Love, I Want Power")
	for _, variant := range []string{
		"If I Can’t Have Love, I Want Power (Deluxe)",
		"If I Can’t Have Love, I Want Power (Extended)",
		"If I Can’t Have Love, I Want Power (25th Anniversary Edition)",
	} {
		if EditionKey(variant) != base {
			t.Fatalf("EditionKey(%q) should group with the base album", variant)
		}
	}
	// Parens that name a DIFFERENT release must stay distinct.
	for _, distinct := range []string{
		"BADLANDS (live from Webster Hall)",
		"Room 93 (commentary)",
		"Colors (Ian Asher remix)",
	} {
		if EditionKey(distinct) == EditionKey("BADLANDS") && distinct != "BADLANDS (live from Webster Hall)" {
			t.Fatalf("EditionKey(%q) must not collapse into another release", distinct)
		}
	}
	if EditionKey("BADLANDS (live from Webster Hall)") == EditionKey("BADLANDS") {
		t.Fatal("live release must not group with the studio album")
	}
	if EditionKey("hopeless fountain kingdom (Deluxe Plus)") != EditionKey("hopeless fountain kingdom") {
		t.Fatal("Deluxe Plus must group with the base album")
	}
}

func TestParseDatePartials(t *testing.T) {
	for value, valid := range map[string]bool{"2020-01-17": true, "2020-01": true, "2020": true, "": false, "soon": false} {
		if got := parseDate(value).Valid; got != valid {
			t.Fatalf("parseDate(%q).Valid = %v, want %v", value, got, valid)
		}
	}
}
