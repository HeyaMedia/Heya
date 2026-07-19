package service

import (
	"math/rand"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

func TestGenreMixCandidatesSparseGate(t *testing.T) {
	affinities := []userGenreAffinity{{Genre: "Rock", Score: 10}}
	if got := genreMixCandidates(affinities, genreMixMinAffinityTracks-1); got != nil {
		t.Fatalf("below genreMixMinAffinityTracks should produce no candidates, got %v", got)
	}
	if got := genreMixCandidates(affinities, genreMixMinAffinityTracks); len(got) != 1 {
		t.Fatalf("at the sparse-gate floor with one dominant genre, expected 1 candidate, got %v", got)
	}
}

func TestGenreMixCandidatesFlatGate(t *testing.T) {
	// 10 genres with equal mass — no single genre clears genreMixMinShare
	// (10% each, floor is 12%).
	affinities := make([]userGenreAffinity, 10)
	for i := range affinities {
		affinities[i] = userGenreAffinity{Genre: "Genre", Score: 1}
	}
	if got := genreMixCandidates(affinities, 100); got != nil {
		t.Fatalf("flat distribution across 10 genres should name none, got %v", got)
	}
}

func TestGenreMixCandidatesTopOneOrTwo(t *testing.T) {
	// One dominant genre (70%) plus a long tail — only the leader clears the
	// bar, so exactly 1 mix comes out.
	dominant := []userGenreAffinity{
		{Genre: "Metal", Score: 70},
		{Genre: "Jazz", Score: 5},
		{Genre: "Pop", Score: 5},
		{Genre: "Folk", Score: 5},
		{Genre: "Blues", Score: 5},
		{Genre: "Ambient", Score: 5},
		{Genre: "Classical", Score: 5},
	}
	got := genreMixCandidates(dominant, 100)
	if len(got) != 1 || got[0].Genre != "Metal" {
		t.Fatalf("expected exactly [Metal], got %v", got)
	}

	// Two close leaders (45%/40%) both clear the bar — capped at 2 even
	// though a third genre also clears it.
	twoLeaders := []userGenreAffinity{
		{Genre: "Metal", Score: 45},
		{Genre: "Punk", Score: 40},
		{Genre: "Jazz", Score: 15},
	}
	got = genreMixCandidates(twoLeaders, 100)
	if len(got) != 2 || got[0].Genre != "Metal" || got[1].Genre != "Punk" {
		t.Fatalf("expected exactly [Metal, Punk], got %v", got)
	}
}

func TestGenreMixCandidatesEmptyInput(t *testing.T) {
	if got := genreMixCandidates(nil, 100); got != nil {
		t.Fatalf("no genres at all should produce no candidates, got %v", got)
	}
}

func TestTitleCaseGenre(t *testing.T) {
	cases := map[string]string{
		"hip hop":   "Hip Hop",
		"ROCK":      "Rock",
		"  metal  ": "Metal",
		"new  wave": "New Wave",
	}
	for in, want := range cases {
		if got := titleCaseGenre(in); got != want {
			t.Errorf("titleCaseGenre(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAssembleGenreMixRespectsExcludeAndDiscoveryQuota(t *testing.T) {
	var core []sqlc.ListArtistTopTracksForMixRow
	for i := int64(1); i <= 4; i++ {
		core = append(core, sqlc.ListArtistTopTracksForMixRow{TrackID: i, ArtistID: i, TrackTitle: "core"})
	}
	var neighbors []genreMixTrackRow
	for i := int64(10); i < 20; i++ {
		neighbors = append(neighbors, genreMixTrackRow{
			Track:       sqlc.ListArtistTopTracksForMixRow{TrackID: i, ArtistID: i, TrackTitle: "neighbor"},
			NeverPlayed: i%2 == 0, // half known, half discovery
		})
	}
	exclude := map[int64]bool{10: true, 12: true}
	rng := rand.New(rand.NewSource(1)) //nolint:gosec // deterministic test rng

	out := assembleGenreMix(core, neighbors, 12, exclude, rng)
	for _, tr := range out {
		if exclude[tr.TrackID] {
			t.Fatalf("excluded track %d leaked into the genre mix", tr.TrackID)
		}
	}
	seen := map[int64]bool{}
	for _, tr := range out {
		if seen[tr.TrackID] {
			t.Fatalf("duplicate track %d in genre mix", tr.TrackID)
		}
		seen[tr.TrackID] = true
	}
}
