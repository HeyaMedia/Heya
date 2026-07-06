package heyamedia

import (
	"testing"

	gen "github.com/karbowiak/heya/clients/heyamedia"
)

// TestMapCollection covers the franchise-membership mapping that feeds the
// matcher's linkCollection (which find-or-creates a collections row and points
// movies.collection_id at it). heya.media exposes the `collection` field but
// may leave it unpopulated per title, so nil/empty must map to nil — the
// matcher only links when detail.Collection is non-nil with a name.
func TestMapCollection(t *testing.T) {
	if got := mapCollection(nil); got != nil {
		t.Fatalf("nil collection should map to nil, got %+v", got)
	}
	if got := mapCollection(&gen.Collection{Name: ""}); got != nil {
		t.Fatalf("unnamed collection should map to nil, got %+v", got)
	}

	overview := "Miami's finest detectives."
	badBoysYear, badBoysTmdb, badBoysVote := int64(1995), int64(9737), 6.7
	badBoysPoster := "https://media.heya.media/images/bb1.webp"
	c := &gen.Collection{
		Name:      "Bad Boys Collection",
		Overview:  &overview,
		Ids:       gen.CollectionIDs{Tmdb: 90727},
		Posters:   &[]gen.ArtworkItem{{Url: "https://img/poster-1.jpg", Source: "tmdb"}, {Url: "https://img/poster-2.jpg", Source: "tmdb"}},
		Backdrops: &[]gen.ArtworkItem{{Url: "https://img/backdrop-1.jpg", Source: "tmdb"}},
		Parts: &[]gen.CollectionPart{
			{Title: "Bad Boys", Year: &badBoysYear, TmdbId: &badBoysTmdb, PosterPath: &badBoysPoster, VoteAverage: &badBoysVote},
			{Title: "Bad Boys II"}, // sparse entry — optional fields absent
		},
	}
	got := mapCollection(c)
	if got == nil {
		t.Fatal("expected non-nil CollectionDetail")
	}
	if len(got.Parts) != 2 {
		t.Fatalf("Parts len = %d, want 2", len(got.Parts))
	}
	p0 := got.Parts[0]
	if p0.Title != "Bad Boys" || p0.Year != 1995 || p0.TmdbID != 9737 || p0.PosterPath != badBoysPoster || p0.VoteAverage != 6.7 {
		t.Errorf("Parts[0] mismatch: %+v", p0)
	}
	if got.Parts[1].Title != "Bad Boys II" || got.Parts[1].TmdbID != 0 {
		t.Errorf("Parts[1] sparse entry mismatch: %+v", got.Parts[1])
	}
	if got.Name != "Bad Boys Collection" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Overview != overview {
		t.Errorf("Overview = %q", got.Overview)
	}
	if got.ExternalIDs["tmdb"] != "90727" {
		t.Errorf("ExternalIDs[tmdb] = %q, want 90727", got.ExternalIDs["tmdb"])
	}
	if got.PosterPath != "https://img/poster-1.jpg" { // first poster wins
		t.Errorf("PosterPath = %q", got.PosterPath)
	}
	if got.BackdropPath != "https://img/backdrop-1.jpg" {
		t.Errorf("BackdropPath = %q", got.BackdropPath)
	}
}

// TestMapMovieOrTV_Collection asserts the collection block is threaded through
// the full movie mapper into MediaDetail (the field the matcher reads), and
// that an absent block stays nil rather than a zero-value struct.
func TestMapMovieOrTV_Collection(t *testing.T) {
	withCol := mapMovieOrTV("id1", "movie", "Fast Five", nil, "fast-five", nil, gen.ExternalIDsDTO{},
		&gen.Detail{Collection: &gen.Collection{Name: "The Fast Saga Collection", Ids: gen.CollectionIDs{Tmdb: 9485}}})
	if withCol.Collection == nil || withCol.Collection.Name != "The Fast Saga Collection" {
		t.Fatalf("detail.Collection not populated: %+v", withCol.Collection)
	}

	noCol := mapMovieOrTV("id2", "movie", "Standalone", nil, "standalone", nil, gen.ExternalIDsDTO{}, &gen.Detail{})
	if noCol.Collection != nil {
		t.Fatalf("absent collection should be nil, got %+v", noCol.Collection)
	}
}
