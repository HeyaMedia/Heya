package service

import (
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

func asset(at, label string, sort int, local, remote string) sqlc.MediaAsset {
	return sqlc.MediaAsset{
		AssetType: sqlc.AssetType(at),
		Label:     label,
		SortOrder: int32(sort),
		LocalPath: local,
		RemoteUrl: remote,
	}
}

func TestPickMediaAsset(t *testing.T) {
	// Ordered like ListMediaAssets (asset_type, sort_order): primary poster
	// before season poster; primary backdrop before carousel extras.
	assets := []sqlc.MediaAsset{
		asset("backdrop", "", 0, "", "http://cdn/bd0.webp"),
		asset("backdrop", "en", 11, "", "http://cdn/bd11.webp"),
		asset("poster", "", 0, "", "http://cdn/p0.webp"),
		asset("poster", "season-1", 1001, "", "http://cdn/s1.webp"),
	}

	// Bare poster request (sort=-1, no label) must pick the primary, never the
	// labeled season poster.
	if got := pickMediaAsset(assets, "poster", -1, ""); got == nil || got.RemoteUrl != "http://cdn/p0.webp" {
		t.Fatalf("bare poster: want primary p0, got %+v", got)
	}
	// Labeled request picks the season row.
	if got := pickMediaAsset(assets, "poster", -1, "season-1"); got == nil || got.RemoteUrl != "http://cdn/s1.webp" {
		t.Fatalf("season poster: want s1, got %+v", got)
	}
	// Sort-exact request picks the carousel backdrop.
	if got := pickMediaAsset(assets, "backdrop", 11, ""); got == nil || got.RemoteUrl != "http://cdn/bd11.webp" {
		t.Fatalf("backdrop sort 11: want bd11, got %+v", got)
	}
	// Bare backdrop picks the primary.
	if got := pickMediaAsset(assets, "backdrop", -1, ""); got == nil || got.RemoteUrl != "http://cdn/bd0.webp" {
		t.Fatalf("bare backdrop: want bd0, got %+v", got)
	}
	// No match.
	if got := pickMediaAsset(assets, "logo", -1, ""); got != nil {
		t.Fatalf("logo: want nil, got %+v", got)
	}
}

func TestPickMediaAsset_LocalWins(t *testing.T) {
	assets := []sqlc.MediaAsset{asset("poster", "", 0, "/data/p.jpg", "http://cdn/p.webp")}
	got := pickMediaAsset(assets, "poster", -1, "")
	if got == nil || got.LocalPath != "/data/p.jpg" {
		t.Fatalf("want row with local path, got %+v", got)
	}
}

func TestImageCacheFilename(t *testing.T) {
	cases := []struct {
		at   string
		sort int
		url  string
		want string
	}{
		{"poster", 0, "https://media.heya.media/x/abc.webp", "poster.webp"},
		{"backdrop", 0, "https://media.heya.media/x/abc.webp", "backdrop.webp"},
		{"poster", 1001, "https://media.heya.media/x/s1.webp", "poster1001.webp"},
		{"still", 2103, "https://media.heya.media/x/e.jpg", "still2103.jpg"},
		{"poster", 0, "https://media.heya.media/noext", "poster.jpg"},
	}
	for _, c := range cases {
		if got := imageCacheFilename(c.at, c.sort, c.url); got != c.want {
			t.Errorf("imageCacheFilename(%q,%d,%q) = %q, want %q", c.at, c.sort, c.url, got, c.want)
		}
	}
}
