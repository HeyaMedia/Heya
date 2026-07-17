package playlistsync

import (
	"reflect"
	"testing"
)

func TestSeriesDisplayName(t *testing.T) {
	tests := map[string]string{
		"weekly-jams":        "Weekly Jams",
		"weekly-exploration": "Weekly Exploration",
		"daily-jams":         "Daily Jams",
		"top-discoveries":    "Top Discoveries",
	}
	for key, want := range tests {
		if got := SeriesDisplayName(key); got != want {
			t.Fatalf("SeriesDisplayName(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestMergeTrackIDs(t *testing.T) {
	tests := []struct {
		name                string
		base, local, remote []string
		want                []string
	}{
		{"local only", []string{"a", "b"}, []string{"b", "a", "c"}, []string{"a", "b"}, []string{"b", "a", "c"}},
		{"remote only", []string{"a", "b"}, []string{"a", "b"}, []string{"b"}, []string{"b"}},
		{"both additions", []string{"a"}, []string{"a", "l"}, []string{"a", "r"}, []string{"a", "r", "l"}},
		{"deletion wins", []string{"a", "b"}, []string{"a"}, []string{"a", "b", "r"}, []string{"a", "r"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MergeTrackIDs(tt.base, tt.local, tt.remote); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
