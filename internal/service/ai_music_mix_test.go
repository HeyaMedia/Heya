package service

import (
	"fmt"
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

func TestAIMusicCandidatePoolLimitKeepsPromptBounded(t *testing.T) {
	tests := []struct {
		limit int
		want  int
	}{
		{limit: 5, want: 48},
		{limit: 30, want: 60},
		{limit: 60, want: 90},
	}
	for _, tt := range tests {
		if got := aiMusicCandidatePoolLimit(tt.limit); got != tt.want {
			t.Fatalf("aiMusicCandidatePoolLimit(%d) = %d, want %d", tt.limit, got, tt.want)
		}
	}
}

func musicCand(trackID, artistID int64, title string, bpm float32, dist float32) aiMusicCandidate {
	b := bpm
	c := aiMusicCandidate{BestDistance: dist, BPM: &b}
	c.Row.TrackID = trackID
	c.Row.ArtistID = artistID
	c.Row.TrackTitle = title
	c.Row.Duration = 200
	return c
}

func TestSelectAIMusicCandidatesDiversity(t *testing.T) {
	// Six tracks from one artist, two from another; cap for limit=6 is 2.
	var pool []aiMusicCandidate
	for i := int64(1); i <= 6; i++ {
		pool = append(pool, musicCand(i, 1, fmt.Sprintf("same-%d", i), 120, 0.1))
	}
	pool = append(pool, musicCand(7, 2, "other-1", 100, 0.2), musicCand(8, 2, "other-2", 140, 0.3))

	out := selectAIMusicCandidates(pool, 6)
	counts := map[int64]int{}
	for _, c := range out[:4] {
		counts[c.Row.ArtistID]++
	}
	// First pass honors the cap (2 per artist for limit 6 → ceil(6/8)=1 → max(2,1)=2).
	if counts[1] > 2 {
		t.Fatalf("artist cap not honored in first pass: %v", counts)
	}
	// The cap relaxes to fill the requested length from a narrow library.
	if len(out) != 6 {
		t.Fatalf("want 6 selected after cap relaxation, got %d", len(out))
	}
}

func TestAIMusicSequenceRising(t *testing.T) {
	tracks := []aiMusicCandidate{
		musicCand(1, 1, "fast", 170, 0.1),
		musicCand(2, 2, "slow", 80, 0.1),
		musicCand(3, 3, "mid", 120, 0.1),
	}
	out := aiMusicSequenceByArc(tracks, "rising")
	if *out[0].BPM != 80 || *out[1].BPM != 120 || *out[2].BPM != 170 {
		t.Fatalf("rising arc must sort by BPM ascending: %v %v %v", *out[0].BPM, *out[1].BPM, *out[2].BPM)
	}
}

func TestAIMusicSequenceCinematicPeaksInside(t *testing.T) {
	var tracks []aiMusicCandidate
	for i := int64(0); i < 8; i++ {
		tracks = append(tracks, musicCand(i+1, i+1, fmt.Sprintf("t%d", i), float32(80+10*i), 0.1))
	}
	out := aiMusicSequenceByArc(tracks, "cinematic")
	peak, peakAt := float32(0), 0
	for i, c := range out {
		if *c.BPM > peak {
			peak, peakAt = *c.BPM, i
		}
	}
	if peakAt == 0 || peakAt == len(out)-1 {
		t.Fatalf("cinematic peak must be interior, got index %d of %d", peakAt, len(out))
	}
	if *out[0].BPM > *out[peakAt].BPM || *out[len(out)-1].BPM > *out[peakAt].BPM {
		t.Fatalf("cinematic must rise to the peak and wind down")
	}
}

func TestAIMusicSequenceBreaksArtistAdjacency(t *testing.T) {
	tracks := []aiMusicCandidate{
		musicCand(1, 1, "a1", 100, 0.1),
		musicCand(2, 1, "a2", 110, 0.1),
		musicCand(3, 2, "b1", 120, 0.1),
		musicCand(4, 3, "c1", 130, 0.1),
	}
	out := aiMusicSequenceByArc(tracks, "steady")
	for i := 1; i < len(out); i++ {
		if out[i].Row.ArtistID == out[i-1].Row.ArtistID {
			t.Fatalf("same artist back-to-back at %d: %v", i, out)
		}
	}
}

func TestAIMusicDerivedReason(t *testing.T) {
	c := musicCand(1, 1, "x", 120, 0.1)
	c.Moods = []string{"dark", "aggressive"}
	c.Genres = []string{"industrial metal"}
	if got := aiMusicDerivedReason(c); got != "dark · aggressive · industrial metal" {
		t.Fatalf("unexpected derived reason: %q", got)
	}
	c.Moods, c.Genres = nil, nil
	if got := aiMusicDerivedReason(c); got != "Strong sonic match" {
		t.Fatalf("empty tags must fall back: %q", got)
	}

	// Classifier slugs and hierarchical genres clean up for display.
	c.Moods = []string{"mood_happy", "danceability"}
	c.Genres = []string{"Electronic---Trance"}
	if got := aiMusicDerivedReason(c); got != "happy · danceable · Trance" {
		t.Fatalf("tags not cleaned: %q", got)
	}
}

func TestSelectAIMusicCandidatesDistanceCutoff(t *testing.T) {
	pool := []aiMusicCandidate{
		musicCand(1, 1, "close", 120, 0.30),
		musicCand(2, 2, "close2", 125, 0.35),
		musicCand(3, 3, "far", 130, 0.55), // past best+0.12 — junk tail
	}
	out := selectAIMusicCandidates(pool, 2)
	if len(out) != 2 || out[0].Row.TrackID != 1 || out[1].Row.TrackID != 2 {
		t.Fatalf("cutoff must prefer close matches: %#v", out)
	}
	// But the cutoff is soft: asking for more than the close set relaxes it.
	if out := selectAIMusicCandidates(pool, 3); len(out) != 3 {
		t.Fatalf("cutoff must relax rather than under-fill: got %d", len(out))
	}
}

func TestSelectAIMusicCandidatesDedupesVersions(t *testing.T) {
	pool := []aiMusicCandidate{
		musicCand(1, 1, "King Of Trash (Original Mix)", 140, 0.30),
		musicCand(2, 1, "King Of Trash (Trash Guitar Mix)", 142, 0.31),
		musicCand(3, 2, "Other Song", 120, 0.32),
	}
	out := selectAIMusicCandidates(pool, 3)
	if len(out) != 2 {
		t.Fatalf("mix must carry one version per song, got %d", len(out))
	}
	if out[0].Row.TrackID != 1 || out[1].Row.TrackID != 3 {
		t.Fatalf("unexpected version-dedup selection: %#v", out)
	}
}
