package service

import (
	"strings"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

func recommendationCandidate(trackID, artistID int64, title string, score float64, known bool) musicRecommendationCandidate {
	return musicRecommendationCandidate{
		Track: sqlc.ListArtistTopTracksForMixRow{
			TrackID: trackID, ArtistID: artistID, TrackTitle: title, Duration: 180,
		},
		Score: score,
		State: musicCandidateState{TrackKnown: known, Affinity: map[bool]float64{true: 8}[known]},
	}
}

func TestMusicAffinityIgnoresSkipsAndKeepsCompletionWeak(t *testing.T) {
	if strings.Contains(musicAffinityCTE, "listened_seconds") {
		t.Fatal("affinity must not infer taste from skip/listen position")
	}
	if !strings.Contains(musicAffinityCTE, "pe.completed") || !strings.Contains(musicAffinityCTE, "LEAST(2.0") {
		t.Fatal("completed plays must be the only bounded implicit signal")
	}
	for _, table := range []string{"user_track_ratings", "user_album_ratings", "user_artist_ratings"} {
		if !strings.Contains(musicVetoFilter, table) {
			t.Fatalf("veto filter missing %s", table)
		}
	}
}

func TestSelectMusicRecommendationsBlendsFamiliarAndFresh(t *testing.T) {
	var candidates []musicRecommendationCandidate
	for i := int64(1); i <= 6; i++ {
		candidates = append(candidates, recommendationCandidate(i, i, "known", float64(20-i), true))
	}
	for i := int64(7); i <= 10; i++ {
		candidates = append(candidates, recommendationCandidate(i, i, "fresh", float64(20-i), false))
	}
	out := selectMusicRecommendations(candidates, 10, recommendForYou)
	if len(out) != 10 {
		t.Fatalf("expected 10 tracks, got %d", len(out))
	}
	known := 0
	for _, track := range out[:5] {
		if track.TrackID <= 6 {
			known++
		}
	}
	if known != 3 {
		t.Fatalf("first blend block should be 3 familiar + 2 fresh, got %d familiar", known)
	}
}

func TestSelectMusicRecommendationsDedupesSongVersions(t *testing.T) {
	candidates := []musicRecommendationCandidate{
		recommendationCandidate(1, 10, "Same Song", 5, false),
		recommendationCandidate(2, 10, "Same Song (Remastered)", 4, false),
		recommendationCandidate(3, 20, "Other", 3, false),
	}
	out := selectMusicRecommendations(candidates, 3, recommendRadio)
	if len(out) != 2 || out[0].TrackID != 1 || out[1].TrackID != 3 {
		t.Fatalf("recording version dedupe failed: %#v", out)
	}
}

func TestMusicCandidateModeEligibility(t *testing.T) {
	now := time.Now()
	fresh := recommendationCandidate(1, 1, "fresh", 1, false)
	if !musicCandidateEligible(fresh, recommendDiscovery, now) {
		t.Fatal("unknown track should be eligible for discovery")
	}
	known := recommendationCandidate(2, 2, "known", 1, true)
	known.State.LastPlayed = now.Add(-90 * 24 * time.Hour)
	if musicCandidateEligible(known, recommendDiscovery, now) {
		t.Fatal("known track must not enter discovery")
	}
	if !musicCandidateEligible(known, recommendRediscover, now) {
		t.Fatal("old positive track should enter rediscovery")
	}
	known.State.LastPlayed = now.Add(-7 * 24 * time.Hour)
	if musicCandidateEligible(known, recommendRediscover, now) {
		t.Fatal("recent track must not enter rediscovery")
	}
}

func TestMusicRotationJitterIsStableAndScoped(t *testing.T) {
	a := musicRotationJitter(1, 2, "for_you", 10)
	if a != musicRotationJitter(1, 2, "for_you", 10) {
		t.Fatal("same daily key must be stable")
	}
	if a == musicRotationJitter(1, 2, "for_you", 11) {
		t.Fatal("day bucket should rotate candidate jitter")
	}
}
