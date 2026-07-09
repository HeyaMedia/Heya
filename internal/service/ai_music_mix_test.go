package service

import (
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

func musicCandidate(id, artistID int64, title string, distance float32) aiMusicCandidate {
	return aiMusicCandidate{
		Row: sqlc.SimilarTracksByTextRichRow{
			TrackID: id, TrackTitle: title, Duration: 180,
			ArtistID: artistID, ArtistName: "Artist", AlbumID: id, AlbumTitle: "Album",
		},
		BestDistance: distance,
	}
}

func TestDisposeAIMusicPicksGroundsAndFills(t *testing.T) {
	candidates := []aiMusicCandidate{
		musicCandidate(1, 10, "One", 0.1),
		musicCandidate(2, 20, "Two", 0.2),
		musicCandidate(3, 30, "Three", 0.3),
		musicCandidate(4, 40, "Four", 0.4),
	}
	picks := []aiMusicPick{
		{ID: 2, Reason: "  crushing opener  "},
		{ID: 999, Reason: "hallucinated"},
		{ID: 2, Reason: "duplicate"},
	}
	out := disposeAIMusicPicks(candidates, picks, 4)
	if len(out) != 4 {
		t.Fatalf("expected fallback fill to 4, got %d", len(out))
	}
	if out[0].TrackID != 2 || out[0].Reason != "crushing opener" {
		t.Fatalf("validated model order/reason lost: %#v", out[0])
	}
	seen := map[int64]bool{}
	for _, track := range out {
		if track.TrackID == 999 || seen[track.TrackID] {
			t.Fatalf("ungrounded or duplicate track: %#v", track)
		}
		seen[track.TrackID] = true
	}
}

func TestDisposeAIMusicPicksAvoidsAdjacentArtist(t *testing.T) {
	candidates := []aiMusicCandidate{
		musicCandidate(1, 10, "One", 0.1),
		musicCandidate(2, 10, "Two", 0.2),
		musicCandidate(3, 20, "Three", 0.3),
		musicCandidate(4, 30, "Four", 0.4),
	}
	picks := []aiMusicPick{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}
	out := disposeAIMusicPicks(candidates, picks, 4)
	if len(out) != 4 {
		t.Fatalf("expected 4 tracks, got %d", len(out))
	}
	for i := 1; i < len(out); i++ {
		if out[i-1].ArtistID == out[i].ArtistID {
			t.Fatalf("adjacent artist repeat at %d: %#v", i, out)
		}
	}
}

func TestDisposeAIMusicPicksDedupesRecording(t *testing.T) {
	a := musicCandidate(1, 10, "Same Song", 0.1)
	b := musicCandidate(2, 10, " same song ", 0.2)
	b.Row.Duration = 184 // same 15-second recording bucket
	c := musicCandidate(3, 20, "Other", 0.3)
	out := disposeAIMusicPicks([]aiMusicCandidate{a, b, c}, []aiMusicPick{{ID: 1}, {ID: 2}, {ID: 3}}, 3)
	if len(out) != 2 || out[0].TrackID != 1 || out[1].TrackID != 3 {
		t.Fatalf("recording duplicate survived: %#v", out)
	}
}

func TestNormalizeMusicProbes(t *testing.T) {
	out := normalizeMusicProbes([]string{" industrial metal ", "Industrial Metal", "", "martial drums"}, "fallback")
	if len(out) != 2 || out[0] != "industrial metal" || out[1] != "martial drums" {
		t.Fatalf("unexpected probes: %#v", out)
	}
	if fallback := normalizeMusicProbes(nil, " raw brief "); len(fallback) != 1 || fallback[0] != "raw brief" {
		t.Fatalf("fallback missing: %#v", fallback)
	}
}
