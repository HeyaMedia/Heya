package scanner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

func TestAnimeFixtureProducesLocalPlans(t *testing.T) {
	animeDir := filepath.Join(testdataRoot(t), "library", "anime")
	if _, err := os.Stat(animeDir); os.IsNotExist(err) {
		t.Skip("testdata/library/anime not found")
	}

	emit := &captureEmitter{}
	inv, err := WalkInventory(context.Background(), []string{animeDir}, emit)
	if err != nil {
		t.Fatalf("walk inventory: %v", err)
	}
	plans, err := AnalyzeAnime(context.Background(), inv, emit)
	if err != nil {
		t.Fatalf("analyze anime: %v", err)
	}
	matches, err := AnalyzeAnimeMatches(context.Background(), plans, emit)
	if err != nil {
		t.Fatalf("match anime: %v", err)
	}

	if got := countInventoryFiles(inv); got != 178 {
		t.Fatalf("classified inventory files: got %d, want 178", got)
	}
	if got := len(inventoryFilesByClass(inv, ClassExtraMedia)); got != 4 {
		t.Fatalf("local extras: got %d, want 4", got)
	}
	if got := len(plans); got != 104 {
		t.Fatalf("anime plans: got %d, want 104", got)
	}
	if got := len(matches); got != 12 {
		t.Fatalf("anime matches: got %d, want 12", got)
	}
	if got := countTVPlannedEpisodes(plans); got != 104 {
		t.Fatalf("planned episodes: got %d, want 104", got)
	}
	if got := len(multiEpisodeTVPlans(plans)); got != 0 {
		t.Fatalf("multi-episode plans: got %d, want 0", got)
	}
	if got := countEvents(emit.events, "anime.file.unplanned"); got != 3 {
		t.Fatalf("unplanned anime files: got %d, want 3", got)
	}
	if got := countEvents(emit.events, "nfo.parse_failed"); got != 1 {
		t.Fatalf("NFO failures: got %d, want 1", got)
	}

	byKey := map[string]TVMatch{}
	for _, match := range matches {
		byKey[match.Key] = match
	}
	assertTVMatch(t, byKey, "tvdb:371898", "86 Eighty Six", "2021", "tvdb", 23, 23)
	assertFlattened86Episodes(t, byKey["tvdb:371898"])
	assertTVMatch(t, byKey, "tmdb:1429", "Attack on Titan", "2013", "tmdb", 10, 10)
	assertTVMatch(t, byKey, "tmdb:209867", "Frieren: Beyond Journey's End", "2023", "tmdb", 10, 10)
	assertTVMatch(t, byKey, "tvdb:389597", "Solo Leveling", "2024", "tvdb", 8, 8)
	assertTVMatch(t, byKey, "title_year:chainsaw man|2022", "Chainsaw Man", "2022", "title_year", 12, 9)
	assertTVMatch(t, byKey, "title_year:dan da dan|2024", "DAN DA DAN", "2024", "title_year", 11, 9)

	assertTVTitleOnly(t, titleOnlyTVMatches(matches, nil), "Bocchi the Rock!", "Mob Psycho 100", "Oshi no Ko", "Sousou no Frieren")
	assertAnimeUnplanned(t, emit.events,
		"Absolutely Cursed Anime/[UnknownGroup] 01 [1080p].mkv",
		"Absolutely Cursed Anime/___S01_FINAL_USE_THIS_ONE.mkv",
		"Absolutely Cursed Anime/episode final final v3.mkv",
	)

	var report bytes.Buffer
	WriteReport(&report, sqlc.Library{ID: 4, Name: "Anime", MediaType: sqlc.MediaTypeAnime}, Result{Inventory: inv, TVPlans: plans, TVMatches: matches}, emit.events)
	for _, want := range []string{
		"Anime scan report: Anime (id=4)",
		"Anime episode plans:   104",
		"Local anime identities: 12",
		"Planned episodes:      104",
		"Local extras:          4",
		"Attack on Titan (2013)/NCOPs/Attack on Titan Creditless Opening 01.mkv type=opening",
		"Attack on Titan (2013)/NCEDs/Attack on Titan Creditless Ending 01.mkv type=ending",
		"Anime plan overview",
		"Chainsaw Man (2022)/tvshow.nfo",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("anime report missing %q:\n%s", want, report.String())
		}
	}
}

func TestAnimeAbsoluteFixtureProducesAbsolutePlans(t *testing.T) {
	animeDir := filepath.Join(testdataRoot(t), "library", "anime-absolute")
	if _, err := os.Stat(animeDir); os.IsNotExist(err) {
		t.Skip("testdata/library/anime-absolute not found")
	}

	emit := &captureEmitter{}
	inv, err := WalkInventory(context.Background(), []string{animeDir}, emit)
	if err != nil {
		t.Fatalf("walk inventory: %v", err)
	}
	plans, err := AnalyzeAnime(context.Background(), inv, emit)
	if err != nil {
		t.Fatalf("analyze absolute anime: %v", err)
	}
	matches, err := AnalyzeAnimeMatches(context.Background(), plans, emit)
	if err != nil {
		t.Fatalf("match absolute anime: %v", err)
	}

	if got := countInventoryFiles(inv); got != 64 {
		t.Fatalf("classified inventory files: got %d, want 64", got)
	}
	if got := len(plans); got != 58 {
		t.Fatalf("absolute anime plans: got %d, want 58", got)
	}
	if got := len(matches); got != 9 {
		t.Fatalf("absolute anime matches: got %d, want 9", got)
	}
	if got := countTVPlannedEpisodes(plans); got != 58 {
		t.Fatalf("planned episodes: got %d, want 58", got)
	}
	if got := countEvents(emit.events, "anime.file.unplanned"); got != 0 {
		t.Fatalf("unplanned absolute anime files: got %d, want 0", got)
	}

	byKey := map[string]TVMatch{}
	for _, match := range matches {
		byKey[match.Key] = match
	}
	assertTVMatch(t, byKey, "anidb:452", "A D Police", "1999", "anidb", 10, 8)
	assertTVMatch(t, byKey, "anidb:17725", "Bullbuster", "", "anidb", 10, 10)
	assertTVMatch(t, byKey, "anidb:8854", "Eureka Seven AO", "", "anidb", 10, 10)
	assertTVMatch(t, byKey, "anidb:5921", "Ultraviolet - Code 044", "", "anidb", 1, 1)
	assertTVMatch(t, byKey, "anidb:11148", "Valkyrie Drive - Mermaid", "", "anidb", 1, 1)
	assertTVMatch(t, byKey, "anidb:18048", "Yuuki Bakuhatsu Bang Bravern", "", "anidb", 1, 1)

	assertTVTitleOnly(t, titleOnlyTVMatches(matches, nil), "Seikai no Monshou", "Top o Nerae!", "Uchuu Senkan Yamato 2")

	var report bytes.Buffer
	WriteReport(&report, sqlc.Library{ID: 5, Name: "AnimeAbsolute", MediaType: sqlc.MediaTypeAnime}, Result{Inventory: inv, TVPlans: plans, TVMatches: matches}, emit.events)
	for _, want := range []string{
		"Anime scan report: AnimeAbsolute (id=5)",
		"Anime episode plans:   58",
		"Local anime identities: 9",
		"Unplanned media:       0",
		"A D Police (1999) [anidb:452]",
		"Uchuu Senkan Yamato 2",
		"episodes=absolute=11",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("absolute anime report missing %q:\n%s", want, report.String())
		}
	}
}

func TestRunLibrarySupportsAnimeReport(t *testing.T) {
	animeDir := filepath.Join(testdataRoot(t), "library", "anime")
	if _, err := os.Stat(animeDir); os.IsNotExist(err) {
		t.Skip("testdata/library/anime not found")
	}

	var out bytes.Buffer
	result, err := RunLibrary(context.Background(), sqlc.Library{
		ID:        4,
		Name:      "Anime",
		MediaType: sqlc.MediaTypeAnime,
		Paths:     []string{animeDir},
	}, Options{Report: true}, &out)
	if err != nil {
		t.Fatalf("run anime library: %v", err)
	}
	if len(result.TVPlans) != 104 {
		t.Fatalf("runner anime plans: got %d, want 104", len(result.TVPlans))
	}
	if len(result.TVMatches) != 12 {
		t.Fatalf("runner anime matches: got %d, want 12", len(result.TVMatches))
	}
	report := out.String()
	for _, want := range []string{
		"Anime scan report: Anime (id=4)",
		"Anime episode plans:   104",
		"Local extras:          4",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("runner report missing %q:\n%s", want, report)
		}
	}
}

func assertFlattened86Episodes(t *testing.T, match TVMatch) {
	t.Helper()
	if len(match.Episodes) != 23 {
		t.Fatalf("86 flattened episodes: got %d, want 23", len(match.Episodes))
	}
	for idx, ref := range match.Episodes {
		want := idx + 1
		if ref.Season != 1 || ref.Episode != want {
			t.Fatalf("86 flattened episode %d: got %#v, want S01E%02d", idx, ref, want)
		}
	}
}

func assertAnimeUnplanned(t *testing.T, events []Event, relPaths ...string) {
	t.Helper()
	events = eventsByName(events, "anime.file.unplanned")
	got := make([]string, len(events))
	for i, ev := range events {
		got[i] = ev.RelPath
	}
	if strings.Join(got, "\x00") != strings.Join(relPaths, "\x00") {
		t.Fatalf("unplanned anime files: got %#v, want %#v", got, relPaths)
	}
}
