package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAlbumWriteTitleYear locks down the guard against
// uq_albums_artist_title_year (artist_id, lower(title), year) — enrichment
// must never rewrite an album onto a (title, year) tuple another album of the
// same artist already owns, or the UPDATE 23505s and the album never enriches.
func TestAlbumWriteTitleYear(t *testing.T) {
	// neverCollides / alwaysCollides stand in for the sibling lookup; the
	// callCount probe asserts we only hit the DB when the tuple actually moves.
	const (
		local      = "Stripped"
		localYear  = "2002"
		canonical  = "Stripped (Deluxe Edition)"
		emptyTitle = ""
	)

	tests := []struct {
		name          string
		embeddedTitle string
		localTitle    string
		candidateYear string
		localYear     string
		collides      bool
		wantTitle     string
		wantYear      string
		wantLookup    bool // did we expect a sibling lookup at all?
	}{
		{
			name:          "no change skips the lookup entirely",
			embeddedTitle: local, localTitle: local,
			candidateYear: localYear, localYear: localYear,
			collides:  false,
			wantTitle: local, wantYear: localYear, wantLookup: false,
		},
		{
			name:          "title drift with no sibling adopts canonical title",
			embeddedTitle: canonical, localTitle: local,
			candidateYear: localYear, localYear: localYear,
			collides:  false,
			wantTitle: canonical, wantYear: localYear, wantLookup: true,
		},
		{
			name:          "title drift onto an existing sibling keeps local title+year",
			embeddedTitle: canonical, localTitle: local,
			candidateYear: localYear, localYear: localYear,
			collides:  true,
			wantTitle: local, wantYear: localYear, wantLookup: true,
		},
		{
			name:          "year backfill onto a same-titled sibling keeps empty local year",
			embeddedTitle: local, localTitle: local,
			candidateYear: "1998", localYear: "",
			collides:  true,
			wantTitle: local, wantYear: "", wantLookup: true,
		},
		{
			name:          "year backfill with no sibling adopts the upstream year",
			embeddedTitle: local, localTitle: local,
			candidateYear: "1998", localYear: "",
			collides:  false,
			wantTitle: local, wantYear: "1998", wantLookup: true,
		},
		{
			name:          "empty upstream title preserves local title and skips lookup when year is stable",
			embeddedTitle: emptyTitle, localTitle: local,
			candidateYear: localYear, localYear: localYear,
			collides:  false,
			wantTitle: local, wantYear: localYear, wantLookup: false,
		},
		{
			name:          "empty upstream title still guards a colliding year backfill",
			embeddedTitle: emptyTitle, localTitle: local,
			candidateYear: "1998", localYear: "",
			collides:  true,
			wantTitle: local, wantYear: "", wantLookup: true,
		},
		{
			name:          "case-only title difference is not a real change",
			embeddedTitle: "STRIPPED", localTitle: local,
			candidateYear: localYear, localYear: localYear,
			collides:  false,
			wantTitle: "STRIPPED", wantYear: localYear, wantLookup: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sawLookup bool
			gotTitle, gotYear := albumWriteTitleYear(
				tt.embeddedTitle, tt.localTitle, tt.candidateYear, tt.localYear,
				func(_, _ string) bool {
					sawLookup = true
					return tt.collides
				},
			)
			assert.Equal(t, tt.wantTitle, gotTitle, "title")
			assert.Equal(t, tt.wantYear, gotYear, "year")
			assert.Equal(t, tt.wantLookup, sawLookup, "sibling lookup invoked")
		})
	}
}
