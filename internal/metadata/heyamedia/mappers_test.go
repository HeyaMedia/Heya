package heyamedia

import (
	"testing"

	gen "github.com/karbowiak/heya/clients/heyamedia"
)

func TestMapSeasonsSynthesizesEpisodeSlotsFromCounts(t *testing.T) {
	specials := int64(1)
	seasonOne := int64(3)
	seasonTwoAired := int64(2)

	seasons := mapSeasons([]gen.Season{
		{Number: 0, EpisodeCount: &specials},
		{Number: 1, EpisodeCount: &seasonOne},
		{Number: 2, AiredEpisodes: &seasonTwoAired},
	})

	if got := len(seasons); got != 3 {
		t.Fatalf("seasons: got %d, want 3", got)
	}
	if got := len(seasons[0].Episodes); got != 1 {
		t.Fatalf("special episodes: got %d, want 1", got)
	}
	if got := seasons[0].Episodes[0].AbsoluteNumber; got != 0 {
		t.Fatalf("special absolute number: got %d, want 0", got)
	}
	if got := len(seasons[1].Episodes); got != 3 {
		t.Fatalf("season 1 episodes: got %d, want 3", got)
	}
	if got := seasons[1].Episodes[0].Number; got != 1 {
		t.Fatalf("season 1 first episode number: got %d, want 1", got)
	}
	if got := seasons[1].Episodes[2].AbsoluteNumber; got != 3 {
		t.Fatalf("season 1 last absolute number: got %d, want 3", got)
	}
	if got := len(seasons[2].Episodes); got != 2 {
		t.Fatalf("season 2 episodes: got %d, want 2", got)
	}
	if got := seasons[2].Episodes[0].AbsoluteNumber; got != 4 {
		t.Fatalf("season 2 first absolute number: got %d, want 4", got)
	}
}
