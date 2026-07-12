package service

import (
	"strings"
	"testing"
)

func TestAIRecScopeUsesViewerFacingMediaKinds(t *testing.T) {
	if got := aiRecScope("tv"); got != "TV series only" {
		t.Fatalf("unexpected TV recommendation scope: %q", got)
	}
	if got := aiRecScope(""); got != "movies and TV series" {
		t.Fatalf("unexpected mixed recommendation scope: %q", got)
	}
}

func TestAIRecCurateUserTreatsAnimeAsTV(t *testing.T) {
	prompt := aiRecCurateUser("aliens", "tv", []ForYouItem{{
		ID: 1, Title: "Dan Da Dan", MediaType: "anime",
	}}, nil, nil, nil, nil)
	if strings.Contains(prompt, "anime") {
		t.Fatalf("TV prompt leaked the anime storage subtype: %q", prompt)
	}
	if !strings.Contains(prompt, `title="Dan Da Dan" | tv`) {
		t.Fatalf("anime candidate was not presented as TV: %q", prompt)
	}
}

func TestDisposePicks(t *testing.T) {
	pool := []ForYouItem{
		{ID: 1, Title: "A", Score: 0.9},
		{ID: 2, Title: "B", Score: 0.8},
		{ID: 3, Title: "C", Score: 0.7},
		{ID: 4, Title: "D", Score: 0.6},
	}
	picks := []aiRecPick{
		{Key: 2, Title: "B", Reason: "tangential", Fit: 2}, // junk tail — cut when strong fits exist
		{Key: 3, Title: "C", Reason: " fits the mood ", Fit: 5},
		{Key: 99, Title: "Z", Reason: "hallucinated key", Fit: 5}, // not in pool — dropped
		{Key: 1, Title: "A", Reason: "classic pick", Fit: 4},
		{Key: 3, Title: "C", Reason: "duplicate", Fit: 5}, // already used — dropped
		{Key: 4, Title: "D", Reason: "also perfect", Fit: 5},
	}

	out := disposePicks(pool, picks, 10)
	if len(out) != 3 {
		t.Fatalf("want 3 picks (junk tail cut), got %d", len(out))
	}
	// Ordering is ours: fit desc, then embedding similarity desc.
	if out[0].ID != 3 || out[1].ID != 4 || out[2].ID != 1 {
		t.Fatalf("want [3 4 1], got [%d %d %d]", out[0].ID, out[1].ID, out[2].ID)
	}
	if out[0].Reason != "fits the mood" {
		t.Fatalf("reason not trimmed: %q", out[0].Reason)
	}
	if out[0].Score != 0.7 {
		t.Fatalf("similarity score lost: %v", out[0].Score)
	}
}

func TestDisposePicksLimit(t *testing.T) {
	pool := []ForYouItem{{ID: 1, Title: "A", Score: 0.9}, {ID: 2, Title: "B", Score: 0.8}, {ID: 3, Title: "C", Score: 0.7}}
	picks := []aiRecPick{{Key: 1, Title: "A", Fit: 5}, {Key: 2, Title: "B", Fit: 5}, {Key: 3, Title: "C", Fit: 5}}
	if out := disposePicks(pool, picks, 2); len(out) != 2 {
		t.Fatalf("limit not enforced: got %d", len(out))
	}
}

func TestDisposePicksWeakOnly(t *testing.T) {
	pool := []ForYouItem{
		{ID: 1, Title: "A", Score: 0.9}, {ID: 2, Title: "B", Score: 0.8}, {ID: 3, Title: "C", Score: 0.7},
		{ID: 4, Title: "D", Score: 0.6}, {ID: 5, Title: "E", Score: 0.5},
	}
	picks := []aiRecPick{
		{Key: 1, Title: "A", Fit: 2}, {Key: 2, Title: "B", Fit: 2}, {Key: 3, Title: "C", Fit: 1},
		{Key: 4, Title: "D", Fit: 2}, {Key: 5, Title: "E", Fit: 1},
	}
	out := disposePicks(pool, picks, 12)
	if len(out) != 4 {
		t.Fatalf("weak-only fallback should cap at 4 near-misses, got %d", len(out))
	}
	if out[0].ID != 1 {
		t.Fatalf("weak picks must still order by fit then similarity, got first=%d", out[0].ID)
	}
}

func TestDisposePicksEmpty(t *testing.T) {
	if out := disposePicks(nil, []aiRecPick{{Key: 1, Title: "A", Reason: "x", Fit: 5}}, 5); len(out) != 0 {
		t.Fatalf("empty pool must yield no picks, got %d", len(out))
	}
}

func TestCountStrongPicks(t *testing.T) {
	picks := []aiRecPick{{Fit: 5}, {Fit: 4}, {Fit: 3}, {Fit: 2}}
	if got := countStrongPicks(picks); got != 2 {
		t.Fatalf("want 2 strong picks (fit ≥4), got %d", got)
	}
	if got := maxFit(picks); got != 5 {
		t.Fatalf("want max fit 5, got %d", got)
	}
	if got := maxFit(nil); got != 0 {
		t.Fatalf("empty picks must have max fit 0, got %d", got)
	}
}

// TestFollowupMergeOrdering simulates AIRecommend's two-round composition:
// round-2 picks are re-keyed by the round-1 pool length into a combined pool,
// then one dispose pass orders both rounds together.
func TestFollowupMergeOrdering(t *testing.T) {
	pool1 := []ForYouItem{{ID: 1, Title: "A", Score: 0.9}, {ID: 2, Title: "B", Score: 0.8}}
	pool2 := []ForYouItem{{ID: 3, Title: "C", Score: 0.7}}
	picks1 := []aiRecPick{{Key: 1, Title: "A", Fit: 2, Reason: "tangential"}}
	picks2 := []aiRecPick{{Key: 1, Title: "C", Fit: 5, Reason: "the real find"}}

	graded := picks1
	offset := len(pool1)
	for _, p := range picks2 {
		p.Key += offset
		graded = append(graded, p)
	}
	pool := append(append([]ForYouItem{}, pool1...), pool2...)

	out := disposePicks(pool, graded, 12)
	if len(out) != 1 || out[0].ID != 3 {
		t.Fatalf("round-2 strong pick must resolve through the offset key and cut the weak round-1 pick: %#v", out)
	}
	if out[0].Reason != "the real find" {
		t.Fatalf("round-2 reason lost in merge: %q", out[0].Reason)
	}
}

func TestDisposePicksRecoversTitleKeyMismatch(t *testing.T) {
	pool := []ForYouItem{
		{ID: 10, Title: "Three-Body", Score: 0.82},
		{ID: 20, Title: "[OSHI NO KO]", Score: 0.79},
	}
	picks := []aiRecPick{{
		Key: 1, Title: "Oshi no Ko", Reason: "the exact reincarnation premise", Fit: 5,
	}}

	out := disposePicks(pool, picks, 12)
	if len(out) != 1 || out[0].ID != 20 {
		t.Fatalf("title must win over a mismatched key: %#v", out)
	}
}
