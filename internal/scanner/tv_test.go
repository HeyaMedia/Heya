package scanner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/parser"
)

func TestTVFixtureProducesLocalPlans(t *testing.T) {
	tvDir := filepath.Join(testdataRoot(t), "library", "tv")
	if _, err := os.Stat(tvDir); os.IsNotExist(err) {
		t.Skip("testdata/library/tv not found")
	}

	emit := &captureEmitter{}
	inv, err := WalkInventory(context.Background(), []string{tvDir}, emit)
	if err != nil {
		t.Fatalf("walk inventory: %v", err)
	}

	plans, err := AnalyzeTV(context.Background(), inv, emit)
	if err != nil {
		t.Fatalf("analyze TV: %v", err)
	}
	matches, err := AnalyzeTVMatches(context.Background(), plans, emit)
	if err != nil {
		t.Fatalf("match TV: %v", err)
	}

	if got := countInventoryFiles(inv); got != 191 {
		t.Fatalf("classified inventory files: got %d, want 191", got)
	}
	if got := len(inventoryFilesByClass(inv, ClassExtraMedia)); got != 5 {
		t.Fatalf("local extras: got %d, want 5", got)
	}
	if got := len(plans); got != 75 {
		t.Fatalf("TV plans: got %d, want 75", got)
	}
	if got := len(matches); got != 20 {
		t.Fatalf("TV matches: got %d, want 20", got)
	}
	if got := countTVPlannedEpisodes(plans); got != 79 {
		t.Fatalf("planned episodes: got %d, want 79", got)
	}
	if got := countEvents(emit.events, "tv.file.unplanned"); got != 4 {
		t.Fatalf("unplanned TV files: got %d, want 4", got)
	}
	if got := countEvents(emit.events, "nfo.parse_failed"); got != 1 {
		t.Fatalf("NFO failures: got %d, want 1", got)
	}
	if got := countEvents(emit.events, "plexmatch.parsed"); got != 2 {
		t.Fatalf("plexmatch files: got %d, want 2", got)
	}

	byKey := map[string]TVMatch{}
	for _, match := range matches {
		byKey[match.Key] = match
	}

	assertTVMatch(t, byKey, "tmdb:1396", "Breaking Bad", "2008", "tmdb", 13, 13)
	assertTVMatch(t, byKey, "tmdb:87108", "Chernobyl", "2019", "tmdb", 5, 5)
	assertTVMatch(t, byKey, "tvdb:421555", "3 Body Problem", "2024", "tvdb", 6, 6)
	assertTVMatch(t, byKey, "tmdb:95480", "Slow Horses", "2022", "tmdb", 3, 5)
	assertTVMatch(t, byKey, "tmdb:19885", "Sherlock", "2010", "tmdb", 1, 3)
	assertTVMatch(t, byKey, "tmdb:57243", "Doctor Who", "2005", "tmdb", 2, 2)
	assertTVMatch(t, byKey, "tmdb:121", "Doctor Who", "1963", "tmdb", 1, 1)
	assertTVMatch(t, byKey, "tmdb:1972", "Battlestar Galactica", "2004", "tmdb", 2, 2)
	assertTVMatch(t, byKey, "imdb:tt0076984", "Battlestar Galactica", "1978", "imdb", 1, 1)
	assertTVMatch(t, byKey, "imdb:tt19395018", "Constellation", "2024", "imdb", 2, 2)
	assertTVMatch(t, byKey, "tmdb:136315", "The Bear", "2022", "tmdb", 1, 1)
	assertTVMatch(t, byKey, "title_year:show with extras|2020", "Show With Extras", "2020", "title_year", 1, 1)

	assertTVTitleOnly(t, titleOnlyTVMatches(matches, nil), "Only Murders in the Building", "Poker Face", "The Bear")
	assertTVUnplanned(t, emit.events,
		"Absolutely Cursed TV/___S01_FINAL_USE_THIS_ONE.mkv",
		"Absolutely Cursed TV/what even is this - 01 - final final.mkv",
		"Random Downloads/S01E02.mkv",
		"Random Downloads/episode.mkv",
	)

	var report bytes.Buffer
	WriteReport(&report, sqlc.Library{ID: 4, Name: "DevTV", MediaType: sqlc.MediaTypeTv}, Result{Inventory: inv, TVPlans: plans, TVMatches: matches}, emit.events)
	for _, want := range []string{
		"TV scan report: DevTV (id=4)",
		"TV episode plans:      75",
		"Local show identities: 20",
		"Multi-episode files",
		"Slow Horses (2022) S01E01,E02",
		"Local extras:          5",
		"Needs review: title-only show identities",
		"Only Murders in the Building",
		"Plexmatch files",
		"3 Body Problem (2024)/.plexmatch",
		"Local extras",
		"Breaking Bad (2008)/featurettes/Inside The RV.mkv type=featurette",
		"Show With Extras (2020)/samples/sample.mkv type=sample",
		"NFO parse failures",
		"The Office (US) (2005)/tvshow.nfo",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("TV report missing %q:\n%s", want, report.String())
		}
	}
}

func TestRunLibrarySupportsTVReport(t *testing.T) {
	tvDir := filepath.Join(testdataRoot(t), "library", "tv")
	if _, err := os.Stat(tvDir); os.IsNotExist(err) {
		t.Skip("testdata/library/tv not found")
	}

	var out bytes.Buffer
	result, err := RunLibrary(context.Background(), sqlc.Library{
		ID:        4,
		Name:      "DevTV",
		MediaType: sqlc.MediaTypeTv,
		Paths:     []string{tvDir},
	}, Options{Report: true}, &out)
	if err != nil {
		t.Fatalf("run TV library: %v", err)
	}
	if len(result.TVPlans) != 75 {
		t.Fatalf("runner TV plans: got %d, want 75", len(result.TVPlans))
	}
	if len(result.TVMatches) != 20 {
		t.Fatalf("runner TV matches: got %d, want 20", len(result.TVMatches))
	}
	report := out.String()
	for _, want := range []string{
		"TV scan report: DevTV (id=4)",
		"TV episode plans:      75",
		"Local extras:          5",
		"Needs review: title-only show identities",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("runner report missing %q:\n%s", want, report)
		}
	}
}

func TestTVYearNumberedSeasonIdentity(t *testing.T) {
	relPath := "MythBusters (2003)/Season 2014/MythBusters (2003) - S2014E01 [WEBDL-1080p][AAC 2.0][EN][8bit][h264][EN]-NTb.mkv"
	parsed := parser.ParseStoragePath(relPath)
	if parsed.Release == nil {
		t.Fatal("expected MythBusters release to parse")
	}
	if !parsed.Release.IsTv {
		t.Fatalf("release should be TV: %#v", parsed.Release)
	}

	season, episodes, absolute := tvEpisodeIdentity(relPath, parsed.Release)
	if season != 2014 || len(episodes) != 1 || episodes[0] != 1 || len(absolute) != 0 {
		t.Fatalf("episode identity: season=%d episodes=%v absolute=%v", season, episodes, absolute)
	}
	if showDir := tvShowDir(relPath, nil, nil); showDir != "MythBusters (2003)" {
		t.Fatalf("show directory = %q, want MythBusters (2003)", showDir)
	}
	show := parseTVShowFolder("MythBusters (2003)")
	if show.title != "MythBusters" || show.year != "2003" {
		t.Fatalf("show identity = %#v", show)
	}
}

func assertTVMatch(t *testing.T, matches map[string]TVMatch, key, title, year, keyType string, plans, episodes int) {
	t.Helper()
	match, ok := matches[key]
	if !ok {
		t.Fatalf("missing TV match %s", key)
	}
	if match.Title != title {
		t.Fatalf("%s title: got %q, want %q", key, match.Title, title)
	}
	if match.Year != year {
		t.Fatalf("%s year: got %q, want %q", key, match.Year, year)
	}
	if match.KeyType != keyType {
		t.Fatalf("%s key type: got %q, want %q", key, match.KeyType, keyType)
	}
	if len(match.Plans) != plans {
		t.Fatalf("%s plans: got %d, want %d", key, len(match.Plans), plans)
	}
	if len(match.Episodes) != episodes {
		t.Fatalf("%s episodes: got %d, want %d: %#v", key, len(match.Episodes), episodes, match.Episodes)
	}
}

func assertTVTitleOnly(t *testing.T, matches []TVMatch, titles ...string) {
	t.Helper()
	got := make([]string, len(matches))
	for i, match := range matches {
		got[i] = match.Title
	}
	if strings.Join(got, "\x00") != strings.Join(titles, "\x00") {
		t.Fatalf("title-only matches: got %#v, want %#v", got, titles)
	}
}

func assertTVUnplanned(t *testing.T, events []Event, relPaths ...string) {
	t.Helper()
	events = eventsByName(events, "tv.file.unplanned")
	got := make([]string, len(events))
	for i, ev := range events {
		got[i] = ev.RelPath
	}
	if strings.Join(got, "\x00") != strings.Join(relPaths, "\x00") {
		t.Fatalf("unplanned TV files: got %#v, want %#v", got, relPaths)
	}
}
