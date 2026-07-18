package parser

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type releaseExpected struct {
	Exists           bool     `json:"exists"`
	Strategy         string   `json:"strategy"`
	Media            string   `json:"media"`
	Title            string   `json:"title"`
	Year             string   `json:"year"`
	Group            string   `json:"group"`
	Resolution       string   `json:"resolution"`
	Source           string   `json:"source"`
	Codec            string   `json:"codec"`
	Catalog          string   `json:"catalog"`
	ReleaseHash      string   `json:"releaseHash"`
	AnidbID          string   `json:"anidbId"`
	Seasons          []int    `json:"seasons"`
	Episodes         []int    `json:"episodes"`
	AbsoluteEpisodes []int    `json:"absoluteEpisodes"`
	FlagsContain     []string `json:"flagsContain"`

	Artist               *string `json:"artist"`
	ArtistDisambiguation *string `json:"artistDisambiguation"`
	Album                *string `json:"album"`
	ReleaseKind          *string `json:"releaseKind"`
	DiscNumber           *int    `json:"discNumber"`
	TrackNumber          *int    `json:"trackNumber"`
	TrackTitle           *string `json:"trackTitle"`
}

type testExpected struct {
	Media          string          `json:"media"`
	EntryType      string          `json:"entryType"`
	Extension      string          `json:"extension"`
	Status         string          `json:"status"`
	ReleaseSegment *string         `json:"releaseSegment"`
	Release        releaseExpected `json:"release"`
}

type testCase struct {
	Label     string       `json:"label"`
	Kind      string       `json:"kind"`
	Input     string       `json:"input"`
	MediaHint string       `json:"mediaHint"`
	Expected  testExpected `json:"expected"`
}

func loadTestCases(t *testing.T, path string) []testCase {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	var cases []testCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("failed to parse %s: %v", path, err)
	}
	return cases
}

func runReleaseParsing(t *testing.T, fixture string) {
	t.Helper()
	cases := loadTestCases(t, fixture)

	for _, tc := range cases {
		if tc.Kind != "storage-path" && tc.Kind != "release-name" {
			continue
		}
		t.Run(tc.Label, func(t *testing.T) {
			switch tc.Kind {
			case "storage-path":
				result := ParseStoragePath(tc.Input)

				if tc.Expected.Media != "" && string(result.Media) != tc.Expected.Media {
					t.Errorf("media: got %q, want %q", result.Media, tc.Expected.Media)
				}
				if tc.Expected.EntryType != "" && string(result.EntryType) != tc.Expected.EntryType {
					t.Errorf("entryType: got %q, want %q", result.EntryType, tc.Expected.EntryType)
				}
				if tc.Expected.Extension != "" && result.Extension != tc.Expected.Extension {
					t.Errorf("extension: got %q, want %q", result.Extension, tc.Expected.Extension)
				}
				if tc.Expected.Status != "" && string(result.Status) != tc.Expected.Status {
					t.Errorf("status: got %q, want %q", result.Status, tc.Expected.Status)
				}

				if tc.Expected.ReleaseSegment != nil {
					if *tc.Expected.ReleaseSegment == "" {
						// null in JSON means no release segment expected
					} else if result.ReleaseSegment != *tc.Expected.ReleaseSegment {
						t.Errorf("releaseSegment: got %q, want %q", result.ReleaseSegment, *tc.Expected.ReleaseSegment)
					}
				}

				checkRelease(t, result.Release, tc.Expected.Release)

			case "release-name":
				hint := SceneMediaKind(tc.MediaHint)
				if hint == "" {
					hint = MediaUnknown
				}
				release := ParseSceneReleaseName(tc.Input, hint)
				checkRelease(t, release, tc.Expected.Release)
			}
		})
	}
}

func checkRelease(t *testing.T, release *SceneReleaseParse, expected releaseExpected) {
	t.Helper()

	if !expected.Exists {
		if release != nil {
			t.Errorf("expected no release, got %+v", release)
		}
		return
	}

	if release == nil {
		t.Fatal("expected release, got nil")
	}

	if expected.Strategy != "" && string(release.Strategy) != expected.Strategy {
		t.Errorf("strategy: got %q, want %q", release.Strategy, expected.Strategy)
	}
	if expected.Media != "" && string(release.Media) != expected.Media {
		t.Errorf("release.media: got %q, want %q", release.Media, expected.Media)
	}
	if expected.Title != "" && release.Title != expected.Title {
		t.Errorf("title: got %q, want %q", release.Title, expected.Title)
	}
	if expected.Year != "" && release.Year != expected.Year {
		t.Errorf("year: got %q, want %q", release.Year, expected.Year)
	}
	if expected.Group != "" && release.Group != expected.Group {
		t.Errorf("group: got %q, want %q", release.Group, expected.Group)
	}
	if expected.Resolution != "" && release.Resolution != expected.Resolution {
		t.Errorf("resolution: got %q, want %q", release.Resolution, expected.Resolution)
	}
	if expected.Source != "" && release.Source != expected.Source {
		t.Errorf("source: got %q, want %q", release.Source, expected.Source)
	}
	if expected.Codec != "" && release.Codec != expected.Codec {
		t.Errorf("codec: got %q, want %q", release.Codec, expected.Codec)
	}
	if expected.Catalog != "" && release.Catalog != expected.Catalog {
		t.Errorf("catalog: got %q, want %q", release.Catalog, expected.Catalog)
	}
	if expected.ReleaseHash != "" && release.ReleaseHash != expected.ReleaseHash {
		t.Errorf("releaseHash: got %q, want %q", release.ReleaseHash, expected.ReleaseHash)
	}
	if expected.AnidbID != "" && release.AnidbID != expected.AnidbID {
		t.Errorf("anidbId: got %q, want %q", release.AnidbID, expected.AnidbID)
	}

	if expected.Seasons != nil {
		if !intSliceEqual(release.Seasons, expected.Seasons) {
			t.Errorf("seasons: got %v, want %v", release.Seasons, expected.Seasons)
		}
	}
	if expected.Episodes != nil {
		if !intSliceEqual(release.Episodes, expected.Episodes) {
			t.Errorf("episodes: got %v, want %v", release.Episodes, expected.Episodes)
		}
	}
	if expected.AbsoluteEpisodes != nil {
		if !intSliceEqual(release.AbsoluteEpisodes, expected.AbsoluteEpisodes) {
			t.Errorf("absoluteEpisodes: got %v, want %v", release.AbsoluteEpisodes, expected.AbsoluteEpisodes)
		}
	}

	for _, flag := range expected.FlagsContain {
		found := false
		for _, f := range release.Flags {
			if strings.EqualFold(f, flag) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("flag %q not found in %v", flag, release.Flags)
		}
	}

	if expected.Artist != nil && release.Artist != *expected.Artist {
		t.Errorf("artist: got %q, want %q", release.Artist, *expected.Artist)
	}
	if expected.ArtistDisambiguation != nil && release.ArtistDisambiguation != *expected.ArtistDisambiguation {
		t.Errorf("artistDisambiguation: got %q, want %q", release.ArtistDisambiguation, *expected.ArtistDisambiguation)
	}
	if expected.Album != nil && release.Album != *expected.Album {
		t.Errorf("album: got %q, want %q", release.Album, *expected.Album)
	}
	if expected.ReleaseKind != nil && release.ReleaseKind != *expected.ReleaseKind {
		t.Errorf("releaseKind: got %q, want %q", release.ReleaseKind, *expected.ReleaseKind)
	}
	if expected.DiscNumber != nil && release.DiscNumber != *expected.DiscNumber {
		t.Errorf("discNumber: got %d, want %d", release.DiscNumber, *expected.DiscNumber)
	}
	if expected.TrackNumber != nil && release.TrackNumber != *expected.TrackNumber {
		t.Errorf("trackNumber: got %d, want %d", release.TrackNumber, *expected.TrackNumber)
	}
	if expected.TrackTitle != nil && release.TrackTitle != *expected.TrackTitle {
		t.Errorf("trackTitle: got %q, want %q", release.TrackTitle, *expected.TrackTitle)
	}
}

func intSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTVReleaseParsing(t *testing.T) {
	runReleaseParsing(t, "../../testdata/parser/tv/release-parsing.json")
}

func TestTVAnimeAbsolute(t *testing.T) {
	runReleaseParsing(t, "../../testdata/parser/tv/anime-absolute.json")
}

func TestMovieReleaseParsing(t *testing.T) {
	runReleaseParsing(t, "../../testdata/parser/movies/release-parsing.json")
}

func TestMusicReleaseParsing(t *testing.T) {
	runReleaseParsing(t, "../../testdata/parser/music/release-parsing.json")
}

func TestMusicCuratedLayouts(t *testing.T) {
	runReleaseParsing(t, "../../testdata/parser/music/curated-layouts.json")
}

func TestBookReleaseParsing(t *testing.T) {
	runReleaseParsing(t, "../../testdata/parser/books/release-parsing.json")
}

type upstreamCorpus struct {
	Label             string   `json:"label"`
	Source            string   `json:"source"`
	MediaHint         string   `json:"mediaHint"`
	MinimumParseRatio float64  `json:"minimumParseRatio"`
	Cases             []string `json:"cases"`
}

func loadCorpus(t *testing.T, path string) []upstreamCorpus {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	var corpora []upstreamCorpus
	if err := json.Unmarshal(data, &corpora); err != nil {
		t.Fatalf("failed to parse %s: %v", path, err)
	}
	return corpora
}

func TestTVUpstreamCorpus(t *testing.T) {
	runUpstreamCorpus(t, "../../testdata/parser/tv/upstream-corpus.json")
}

func TestMovieUpstreamCorpus(t *testing.T) {
	runUpstreamCorpus(t, "../../testdata/parser/movies/upstream-corpus.json")
}

func TestMusicUpstreamCorpus(t *testing.T) {
	runUpstreamCorpus(t, "../../testdata/parser/music/upstream-corpus.json")
}

func runUpstreamCorpus(t *testing.T, path string) {
	t.Helper()
	corpora := loadCorpus(t, path)

	for _, corpus := range corpora {
		t.Run(corpus.Label, func(t *testing.T) {
			hint := SceneMediaKind(corpus.MediaHint)
			if hint == "" {
				hint = MediaUnknown
			}

			total := len(corpus.Cases)
			parsed := 0

			for _, name := range corpus.Cases {
				result := ParseSceneReleaseName(name, hint)
				if result != nil {
					parsed++
				}
			}

			ratio := float64(parsed) / float64(total)
			t.Logf("parsed %d/%d (%.1f%%), minimum %.1f%%", parsed, total, ratio*100, corpus.MinimumParseRatio*100)

			if ratio < corpus.MinimumParseRatio {
				t.Errorf("parse ratio %.1f%% below minimum %.1f%%", ratio*100, corpus.MinimumParseRatio*100)
			}
		})
	}
}
