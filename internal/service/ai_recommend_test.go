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
	}}, nil, nil, nil)
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
