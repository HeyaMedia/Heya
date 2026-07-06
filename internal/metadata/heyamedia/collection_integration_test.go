package heyamedia_test

import (
	"context"
	"testing"

	"github.com/karbowiak/heya/internal/metadata/heyamedia"
)

// TestHeyaProvider_MovieCollectionParts fetches a known franchise film
// (LOTR: Fellowship, tmdb 120) from a live heya.media and asserts the client +
// mapper surface the collection membership. Isolates the fetch/map path from
// the enrich queue. Gated on HEYA_MEDIA_URL like the other integration tests.
func TestHeyaProvider_MovieCollectionParts(t *testing.T) {
	skipIfHeyaDown(t)
	c := heyamedia.NewClient(heyaURL())
	p := heyamedia.NewHeyaProvider(c)

	detail, err := p.GetDetail(context.Background(), "heya:movie:tmdb:120", nil)
	if err != nil {
		t.Fatalf("GetDetail: %v", err)
	}
	if detail == nil {
		t.Fatal("nil detail")
	}
	if detail.Collection == nil {
		t.Fatalf("expected a collection on %q, got nil", detail.Title)
	}
	t.Logf("collection=%q parts=%d", detail.Collection.Name, len(detail.Collection.Parts))
	for _, pt := range detail.Collection.Parts {
		t.Logf("  - %s (%d) tmdb=%d poster=%s", pt.Title, pt.Year, pt.TmdbID, pt.PosterPath)
	}
	if len(detail.Collection.Parts) == 0 {
		t.Error("expected non-empty collection.parts")
	}
}
