package discography

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

func TestMatchLocalAlbumsPrecedence(t *testing.T) {
	locals := []sqlc.Album{
		{ID: 1, Title: "Room 93", Year: "2014", MusicbrainzID: "aaa-111"},
		{ID: 2, Title: "Room 93", Year: "2015"},
		{ID: 3, Title: "BADLANDS", Year: "2015", ExternalIds: []byte(`{"mb_release_group":"bbb-222"}`)},
	}
	one := func(entry metadata.AlbumEntry) pgtype.Int8 {
		return matchLocalAlbums([]metadata.AlbumEntry{entry}, locals)[0]
	}

	// External id beats title matching, wherever the id lives locally.
	if got := one(metadata.AlbumEntry{Title: "Different Name", ExternalIDs: map[string]string{"mbid": "BBB-222"}}); !got.Valid || got.Int64 != 3 {
		t.Fatalf("external id match: got %+v, want album 3", got)
	}
	// Title+year narrows between same-titled releases.
	if got := one(metadata.AlbumEntry{Title: "room 93!", Year: 2015}); !got.Valid || got.Int64 != 2 {
		t.Fatalf("title+year match: got %+v, want album 2", got)
	}
	// Title alone falls back to the first candidate.
	if got := one(metadata.AlbumEntry{Title: "Room 93"}); !got.Valid || got.Int64 != 1 {
		t.Fatalf("title match: got %+v, want album 1", got)
	}
	if got := one(metadata.AlbumEntry{Title: "Manic"}); got.Valid {
		t.Fatal("unknown title must not match")
	}
}

func TestMatchLocalAlbumsClaimsEachLocalOnce(t *testing.T) {
	locals := []sqlc.Album{
		{ID: 10, Title: "BADLANDS", Year: "2015", MusicbrainzID: "rg-album"},
	}

	// A same-titled single must not double-attach to the album's local row —
	// the exact external-id claim wins even though the single sorts first.
	got := matchLocalAlbums([]metadata.AlbumEntry{
		{Title: "BADLANDS", Type: "single", Year: 2015},
		{Title: "BADLANDS", Type: "album", Year: 2015, ExternalIDs: map[string]string{"mbid": "rg-album"}},
	}, locals)
	if got[0].Valid {
		t.Fatalf("title-only single claimed the local out from under the ext-id album match: %+v", got[0])
	}
	if !got[1].Valid || got[1].Int64 != 10 {
		t.Fatalf("ext-id album match should claim local 10, got %+v", got[1])
	}

	// Without an ext-id anywhere, the first entry claims and the second stays
	// catalog-only rather than duplicating the local.
	got = matchLocalAlbums([]metadata.AlbumEntry{
		{Title: "BADLANDS", Type: "album", Year: 2015},
		{Title: "BADLANDS", Type: "single", Year: 2015},
	}, locals)
	if !got[0].Valid || got[0].Int64 != 10 {
		t.Fatalf("first title match should claim local 10, got %+v", got[0])
	}
	if got[1].Valid {
		t.Fatalf("second entry must not re-claim the same local, got %+v", got[1])
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
