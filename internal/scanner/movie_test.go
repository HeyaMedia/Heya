package scanner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/karbowiak/heya/internal/parser"
)

type captureEmitter struct {
	mu     sync.Mutex
	events []Event
}

func (c *captureEmitter) Emit(ev Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func TestMovieFixtureProducesLocalPlans(t *testing.T) {
	movieDir := filepath.Join(testdataRoot(t), "library", "movies")
	if _, err := os.Stat(movieDir); os.IsNotExist(err) {
		t.Skip("testdata/library/movies not found")
	}

	emit := &captureEmitter{}
	inv, err := WalkInventory(context.Background(), []string{movieDir}, emit)
	if err != nil {
		t.Fatalf("walk inventory: %v", err)
	}

	plans, err := AnalyzeMovies(context.Background(), inv, emit)
	if err != nil {
		t.Fatalf("analyze movies: %v", err)
	}

	byKey := map[string]MoviePlan{}
	keyCount := map[string]int{}
	for _, p := range plans {
		byKey[moviePlanKey(p.Title, p.Year)] = p
		keyCount[moviePlanKey(p.Title, p.Year)]++
	}

	if len(plans) != 25 {
		t.Fatalf("plans: got %d, want 25: %#v", len(plans), plans)
	}
	assertMoviePlan(t, byKey, "A Goofy Movie", "1995", "nfo", map[string]string{"imdb": "tt0113198"})
	assertMoviePlan(t, byKey, "Actually Corrected By NFO", "2021", "nfo", map[string]string{"imdb": "tt0000001", "tmdb": "999001"})
	assertMoviePlan(t, byKey, "Dune", "2021", "nfo", map[string]string{"imdb": "tt1160419", "tmdb": "438631"})
	assertMoviePlan(t, byKey, "The Matrix", "1999", "nfo", map[string]string{"imdb": "tt0133093", "tmdb": "603"})
	assertMoviePlan(t, byKey, "Alien", "1979", "filename", map[string]string{"imdb": "tt0078748"})
	assertMoviePlan(t, byKey, "Alien", "1992", "filename", map[string]string{"imdb": "tt0103644"})
	assertMoviePlan(t, byKey, "Anora", "2024", "filename", nil)
	assertMoviePlan(t, byKey, "Avatar The Way of Water", "2022", "filename", nil)
	assertMoviePlan(t, byKey, "Everything Everywhere All at Once", "2022", "nfo", map[string]string{"imdb": "tt6710474", "tmdb": "545611"})
	assertMoviePlan(t, byKey, "Jackass Presents Bad Grandpa .5", "2014", "filename", nil)
	assertMoviePlan(t, byKey, "Mad Max: Fury Road", "2015", "nfo", map[string]string{"imdb": "tt1392190", "tmdb": "76341"})
	assertMoviePlan(t, byKey, "Naked Gun 33 1/3: The Final Insult", "1994", "nfo", map[string]string{"imdb": "tt0110622"})
	assertMoviePlan(t, byKey, "Nope", "2022", "nfo", map[string]string{"imdb": "tt10954984", "tmdb": "762504"})
	assertMoviePlan(t, byKey, "Oppenheimer", "2023", "nfo", map[string]string{"imdb": "tt15398776", "tmdb": "872585"})
	assertMoviePlan(t, byKey, "TMDb Only Folder", "2024", "filename", map[string]string{"tmdb": "1234567"})
	assertMoviePlan(t, byKey, "The Lord of the Rings: The Fellowship of the Ring", "2001", "nfo", map[string]string{"imdb": "tt0120737", "tmdb": "120"})
	assertMoviePlan(t, byKey, "The Naked Gun", "2025", "filename", nil)
	assertMoviePlan(t, byKey, "Thunderbolts", "2025", "nfo", map[string]string{"imdb": "tt20969586", "tmdb": "986056"})

	if got := keyCount[moviePlanKey("Kill Bill Vol 1", "2003")]; got != 1 {
		t.Fatalf("split-disc fixture should be one multipart movie plan: got %d Kill Bill plans, want 1", got)
	}
	killBillPlan := byKey[moviePlanKey("Kill Bill Vol 1", "2003")]
	if len(killBillPlan.Files) != 2 {
		t.Fatalf("Kill Bill plan files: got %d, want 2: %#v", len(killBillPlan.Files), killBillPlan.Files)
	}
	if len(killBillPlan.Parts) != 2 {
		t.Fatalf("Kill Bill plan parts: got %d, want 2: %#v", len(killBillPlan.Parts), killBillPlan.Parts)
	}

	unplanned := []string{
		"Absolutely Cursed Naming/___1080p_FINAL_FINAL_USE_THIS_ONE.mkv",
		"Documentaries/random.mkv",
		"No Year But IMDB {imdb-tt0114709}/video.mkv",
		"The Matrix (1999)/Deleted Intro Clip.mkv",
	}
	for _, relPath := range unplanned {
		assertFileNotPlanned(t, plans, relPath)
	}

	if !eventSeen(emit.events, "movie.file.unplanned") {
		t.Fatalf("expected movie.file.unplanned event for non-movie media inside movie fixture")
	}

	matches, err := AnalyzeMovieMatches(context.Background(), plans, emit)
	if err != nil {
		t.Fatalf("match movies: %v", err)
	}
	byMatch := map[string]MovieMatch{}
	for _, match := range matches {
		byMatch[moviePlanKey(match.Title, match.Year)] = match
	}

	if len(matches) != 25 {
		t.Fatalf("matches: got %d, want 25: %#v", len(matches), matches)
	}
	assertMovieMatch(t, byMatch, "Dune", "2021", "tmdb:438631", "tmdb", 0.99, 1)
	assertMovieMatch(t, byMatch, "The Matrix", "1999", "tmdb:603", "tmdb", 0.99, 1)
	assertMovieMatch(t, byMatch, "A Goofy Movie", "1995", "imdb:tt0113198", "imdb", 0.99, 1)
	assertMovieMatch(t, byMatch, "Avatar The Way of Water", "2022", "title_year:avatar the way of water|2022", "title_year", 0.82, 1)
	assertMovieMatch(t, byMatch, "The Naked Gun", "2025", "title_year:the naked gun|2025", "title_year", 0.82, 1)
	assertMovieMatch(t, byMatch, "Actually Corrected By NFO", "2021", "tmdb:999001", "tmdb", 0.99, 1)
	assertMovieMatch(t, byMatch, "Jackass Presents Bad Grandpa .5", "2014", "title_year:jackass presents bad grandpa 5|2014", "title_year", 0.82, 1)
	assertMovieMatch(t, byMatch, "Kill Bill Vol 1", "2003", "title_year:kill bill vol 1|2003", "title_year", 0.82, 1)
	if !contains(byMatch[moviePlanKey("Alien", "1992")].Aliases, "Alien³") {
		t.Fatalf("Alien 1992 should carry path alias Alien³: %#v", byMatch[moviePlanKey("Alien", "1992")].Aliases)
	}

	killBill := byMatch[moviePlanKey("Kill Bill Vol 1", "2003")]
	if len(killBill.Files) != 2 {
		t.Fatalf("Kill Bill match files: got %d, want 2: %#v", len(killBill.Files), killBill.Files)
	}
	if !eventSeen(emit.events, "plan.movie.multipart_joined") {
		t.Fatalf("expected plan.movie.multipart_joined event for split-disc fixture")
	}
}

func TestMovieIdentityUsesParserMovieFixtures(t *testing.T) {
	cases := loadMovieParserCases(t)
	for _, tc := range cases {
		if tc.Kind != "storage-path" {
			continue
		}
		t.Run(tc.Label, func(t *testing.T) {
			parsed := parser.ParseStoragePath(tc.Input)
			ident, ok := movieIdentity(parsed, nil)

			if !tc.Expected.Release.Exists {
				if ok {
					t.Fatalf("identity: got %#v, want none", ident)
				}
				return
			}
			if !ok {
				t.Fatalf("expected movie identity")
			}
			if tc.Expected.Release.Title != "" && ident.title != tc.Expected.Release.Title {
				t.Fatalf("title: got %q, want %q", ident.title, tc.Expected.Release.Title)
			}
			if tc.Expected.Release.Year != "" && ident.year != tc.Expected.Release.Year {
				t.Fatalf("year: got %q, want %q", ident.year, tc.Expected.Release.Year)
			}
		})
	}
}

func assertFileNotPlanned(t *testing.T, plans []MoviePlan, relPath string) {
	t.Helper()
	for _, p := range plans {
		for _, f := range p.Files {
			if f == relPath {
				t.Fatalf("%s should be classified but not planned as a movie: %#v", relPath, p)
			}
		}
	}
}

func assertMovieMatch(t *testing.T, matches map[string]MovieMatch, title, year, key, keyType string, confidence float64, planCount int) {
	t.Helper()
	match, ok := matches[moviePlanKey(title, year)]
	if !ok {
		t.Fatalf("missing movie match %q (%s)", title, year)
	}
	if match.Key != key {
		t.Fatalf("%s match key: got %q, want %q", title, match.Key, key)
	}
	if match.KeyType != keyType {
		t.Fatalf("%s key type: got %q, want %q", title, match.KeyType, keyType)
	}
	if match.Confidence != confidence {
		t.Fatalf("%s confidence: got %.2f, want %.2f", title, match.Confidence, confidence)
	}
	if len(match.Plans) != planCount {
		t.Fatalf("%s plan count: got %d, want %d", title, len(match.Plans), planCount)
	}
}

func assertMoviePlan(t *testing.T, plans map[string]MoviePlan, title, year, source string, ids map[string]string) {
	t.Helper()
	plan, ok := plans[moviePlanKey(title, year)]
	if !ok {
		t.Fatalf("missing movie plan %q (%s)", title, year)
	}
	if plan.Year != year {
		t.Fatalf("%s year: got %q, want %q", title, plan.Year, year)
	}
	if plan.Source != source {
		t.Fatalf("%s source: got %q, want %q", title, plan.Source, source)
	}
	for k, want := range ids {
		if got := plan.ExternalIDs[k]; got != want {
			t.Fatalf("%s id %s: got %q, want %q", title, k, got, want)
		}
	}
}

func moviePlanKey(title, year string) string {
	return title + "\x00" + year
}

func eventSeen(events []Event, name string) bool {
	for _, ev := range events {
		if ev.Event == name {
			return true
		}
	}
	return false
}

type movieParserCase struct {
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	Input    string `json:"input"`
	Expected struct {
		Release struct {
			Exists bool   `json:"exists"`
			Title  string `json:"title"`
			Year   string `json:"year"`
		} `json:"release"`
	} `json:"expected"`
}

func loadMovieParserCases(t *testing.T) []movieParserCase {
	t.Helper()
	path := filepath.Join(testdataRoot(t), "parser", "movies", "release-parsing.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read movie parser fixture: %v", err)
	}
	var cases []movieParserCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse movie parser fixture: %v", err)
	}
	return cases
}

func testdataRoot(t *testing.T) string {
	t.Helper()
	wd, _ := os.Getwd()
	for d := wd; d != "/"; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "testdata")); err == nil {
			return filepath.Join(d, "testdata")
		}
	}
	return filepath.Join(wd, "..", "..", "testdata")
}
