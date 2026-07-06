package matcher

import (
	"reflect"
	"testing"
)

func TestResolveAbsolute(t *testing.T) {
	absMap := map[int]absSeasonEpisode{
		24: {season: 2, episode: 2},
		26: {season: 2, episode: 4},
		1:  {season: 1, episode: 1},
	}
	cases := []struct {
		name     string
		absEps   []int
		seasons  []int
		episodes []int
	}{
		{"single", []int{24}, []int{2}, []int{2}},
		{"two same season", []int{24, 26}, []int{2}, []int{2, 4}},
		{"unknown drops", []int{99}, nil, nil},
		{"mixed known/unknown", []int{99, 1}, []int{1}, []int{1}},
		{"empty", nil, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			seasons, episodes := resolveAbsolute(c.absEps, absMap)
			if !reflect.DeepEqual(seasons, c.seasons) {
				t.Errorf("seasons: got %v, want %v", seasons, c.seasons)
			}
			if !reflect.DeepEqual(episodes, c.episodes) {
				t.Errorf("episodes: got %v, want %v", episodes, c.episodes)
			}
		})
	}
}

func TestResolveAbsoluteSpansSeasonsSorted(t *testing.T) {
	// Defensive: a (rare) multi-ep file spanning seasons yields unique, sorted
	// seasons — the season-2 entry must not shadow season 1.
	absMap := map[int]absSeasonEpisode{
		25: {season: 2, episode: 1},
		13: {season: 1, episode: 13},
	}
	seasons, episodes := resolveAbsolute([]int{25, 13}, absMap)
	if !reflect.DeepEqual(seasons, []int{1, 2}) {
		t.Errorf("seasons: got %v, want [1 2]", seasons)
	}
	if !reflect.DeepEqual(episodes, []int{1, 13}) {
		t.Errorf("episodes: got %v, want [1 13]", episodes)
	}
}

func TestIntsEqual(t *testing.T) {
	if !intsEqual(nil, nil) {
		t.Error("nil==nil should be equal")
	}
	if !intsEqual([]int{1, 2}, []int{1, 2}) {
		t.Error("[1 2]==[1 2] should be equal")
	}
	if intsEqual([]int{1, 2}, []int{2, 1}) {
		t.Error("order matters")
	}
	if intsEqual([]int{1}, []int{1, 2}) {
		t.Error("length matters")
	}
}
