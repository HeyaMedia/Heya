package service

import (
	"testing"

	"github.com/karbowiak/heya/internal/metadata"
)

// TestBuildCollectionPartViews covers the owned-vs-missing resolution the
// collection page relies on: release order is preserved, parts whose tmdb id
// matches a local movie are tagged owned (with the local ref) and counted, and
// everything else — no match, or no tmdb id at all — stays missing.
func TestBuildCollectionPartViews(t *testing.T) {
	parts := []metadata.CollectionPart{
		{Title: "The Fellowship of the Ring", Year: 2001, TmdbID: 120},
		{Title: "The Two Towers", Year: 2002, TmdbID: 121},
		{Title: "The Return of the King", Year: 2003, TmdbID: 122},
		{Title: "Announced, Undated", TmdbID: 0}, // no tmdb → never resolvable
	}
	local := map[int64]collectionLocalRef{
		120: {ID: 10, Slug: "the-fellowship-of-the-ring-2001"},
		122: {ID: 12, Slug: "the-return-of-the-king-2003"},
	}

	views, owned := buildCollectionPartViews(parts, local)

	if len(views) != 4 {
		t.Fatalf("views len = %d, want 4", len(views))
	}
	if owned != 2 {
		t.Errorf("owned = %d, want 2", owned)
	}
	// Order preserved (stored release order).
	if views[0].Title != "The Fellowship of the Ring" || views[3].Title != "Announced, Undated" {
		t.Errorf("order not preserved: %q .. %q", views[0].Title, views[3].Title)
	}
	// Owned parts carry the resolved local ref.
	if views[0].LocalMediaItemID == nil || *views[0].LocalMediaItemID != 10 ||
		views[0].LocalSlug == nil || *views[0].LocalSlug != "the-fellowship-of-the-ring-2001" {
		t.Errorf("Fellowship not resolved to local movie: %+v", views[0])
	}
	if views[2].LocalMediaItemID == nil || *views[2].LocalMediaItemID != 12 {
		t.Errorf("Return of the King not resolved: %+v", views[2])
	}
	// tmdb present but absent locally → missing.
	if views[1].LocalMediaItemID != nil {
		t.Errorf("Two Towers should be missing (not in library): %+v", views[1])
	}
	// No tmdb id → always missing, never a false positive.
	if views[3].LocalMediaItemID != nil {
		t.Errorf("no-tmdb part should be missing: %+v", views[3])
	}

	// Empty membership resolves to nothing owned.
	if v, o := buildCollectionPartViews(nil, local); len(v) != 0 || o != 0 {
		t.Errorf("empty parts: views=%d owned=%d, want 0/0", len(v), o)
	}
}
