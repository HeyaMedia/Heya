package service

import "testing"

func TestDisposePicks(t *testing.T) {
	pool := []ForYouItem{
		{ID: 1, Title: "A", Score: 0.9},
		{ID: 2, Title: "B", Score: 0.8},
		{ID: 3, Title: "C", Score: 0.7},
		{ID: 4, Title: "D", Score: 0.6},
	}
	picks := []aiRecPick{
		{ID: 2, Reason: "tangential", Fit: 2}, // junk tail — cut when strong fits exist
		{ID: 3, Reason: " fits the mood ", Fit: 5},
		{ID: 99, Reason: "hallucinated id", Fit: 5}, // not in pool — dropped
		{ID: 1, Reason: "classic pick", Fit: 4},
		{ID: 3, Reason: "duplicate", Fit: 5}, // already used — dropped
		{ID: 4, Reason: "also perfect", Fit: 5},
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
	pool := []ForYouItem{{ID: 1, Score: 0.9}, {ID: 2, Score: 0.8}, {ID: 3, Score: 0.7}}
	picks := []aiRecPick{{ID: 1, Fit: 5}, {ID: 2, Fit: 5}, {ID: 3, Fit: 5}}
	if out := disposePicks(pool, picks, 2); len(out) != 2 {
		t.Fatalf("limit not enforced: got %d", len(out))
	}
}

func TestDisposePicksWeakOnly(t *testing.T) {
	pool := []ForYouItem{
		{ID: 1, Score: 0.9}, {ID: 2, Score: 0.8}, {ID: 3, Score: 0.7},
		{ID: 4, Score: 0.6}, {ID: 5, Score: 0.5},
	}
	picks := []aiRecPick{
		{ID: 1, Fit: 2}, {ID: 2, Fit: 2}, {ID: 3, Fit: 1}, {ID: 4, Fit: 2}, {ID: 5, Fit: 1},
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
	if out := disposePicks(nil, []aiRecPick{{ID: 1, Reason: "x", Fit: 5}}, 5); len(out) != 0 {
		t.Fatalf("empty pool must yield no picks, got %d", len(out))
	}
}
