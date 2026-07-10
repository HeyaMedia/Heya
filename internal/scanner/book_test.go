package scanner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestBookFixtureProducesLocalPlans(t *testing.T) {
	bookDir := filepath.Join(testdataRoot(t), "library", "books")
	if _, err := os.Stat(bookDir); os.IsNotExist(err) {
		t.Skip("testdata/library/books not found")
	}

	emit := &captureEmitter{}
	inv, err := WalkInventory(context.Background(), []string{bookDir}, emit)
	if err != nil {
		t.Fatalf("walk inventory: %v", err)
	}
	plans, err := AnalyzeBooks(context.Background(), inv, emit)
	if err != nil {
		t.Fatalf("analyze books: %v", err)
	}
	if got := len(plans); got != 6 {
		t.Fatalf("book plans: got %d, want 6: %#v", got, plans)
	}
	byTitle := map[string]BookPlan{}
	for _, plan := range plans {
		byTitle[plan.Title] = plan
		if plan.Format != "book" {
			t.Fatalf("%s format: got %q, want book", plan.Title, plan.Format)
		}
		if len(plan.Files) != 1 {
			t.Fatalf("%s files: got %d, want 1: %#v", plan.Title, len(plan.Files), plan.Files)
		}
	}
	assertBookPlan(t, byTitle, "Project Hail Mary", "Andy Weir", "2021", "epub")
	assertBookPlan(t, byTitle, "Mistborn The Final Empire", "Brandon Sanderson", "2006", "epub")
	assertBookPlan(t, byTitle, "The Name of the Wind", "Patrick Rothfuss", "2007", "pdf")
	assertBookPlan(t, byTitle, "Exhalation", "Ted Chiang", "2019", "epub")
	assertBookPlan(t, byTitle, "Dune", "Frank Herbert", "1965", "epub")
	assertBookPlan(t, byTitle, "Dune Messiah", "Frank Herbert", "1969", "pdf")

	var report bytes.Buffer
	WriteReport(&report, sqlc.Library{ID: 9, Name: "DevBooks", MediaType: sqlc.MediaTypeBook}, Result{
		Inventory: inv,
		BookPlans: plans,
	}, emit.events)
	for _, want := range []string{
		"Book scan report: DevBooks (id=9)",
		"Book plans:       6",
		"Ebooks:           6",
		"Audiobooks:       0",
		"Book plan overview",
		"Project Hail Mary (2021) by Andy Weir",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("book report missing %q:\n%s", want, report.String())
		}
	}
}

func TestAudiobookFixtureGroupsChapterFiles(t *testing.T) {
	audioDir := filepath.Join(testdataRoot(t), "library", "audiobooks")
	if _, err := os.Stat(audioDir); os.IsNotExist(err) {
		t.Skip("testdata/library/audiobooks not found")
	}

	emit := &captureEmitter{}
	inv, err := WalkInventory(context.Background(), []string{audioDir}, emit)
	if err != nil {
		t.Fatalf("walk inventory: %v", err)
	}
	plans, err := AnalyzeBooks(context.Background(), inv, emit)
	if err != nil {
		t.Fatalf("analyze audiobooks: %v", err)
	}
	if got := len(plans); got != 3 {
		t.Fatalf("audiobook plans: got %d, want 3: %#v", got, plans)
	}
	byTitle := map[string]BookPlan{}
	for _, plan := range plans {
		byTitle[plan.Title] = plan
		if plan.Format != "audiobook" {
			t.Fatalf("%s format: got %q, want audiobook", plan.Title, plan.Format)
		}
	}
	assertBookPlanFiles(t, byTitle, "The Martian", "Andy Weir", "2014", 2)
	assertBookPlanFiles(t, byTitle, "Project Hail Mary", "Andy Weir", "2021", 4)
	assertBookPlanFiles(t, byTitle, "The Hobbit", "J.R.R. Tolkien", "1937", 3)
}

func TestRunLibrarySupportsBookReport(t *testing.T) {
	bookDir := filepath.Join(testdataRoot(t), "library", "books")
	if _, err := os.Stat(bookDir); os.IsNotExist(err) {
		t.Skip("testdata/library/books not found")
	}

	var out bytes.Buffer
	result, err := RunLibrary(context.Background(), sqlc.Library{
		ID:        9,
		Name:      "DevBooks",
		MediaType: sqlc.MediaTypeBook,
		Paths:     []string{bookDir},
	}, Options{Report: true}, &out)
	if err != nil {
		t.Fatalf("run book library: %v", err)
	}
	if len(result.BookPlans) != 6 {
		t.Fatalf("runner book plans: got %d, want 6", len(result.BookPlans))
	}
	if !strings.Contains(out.String(), "Search was not run. Add --search") {
		t.Fatalf("book runner report missing search note:\n%s", out.String())
	}
}

func TestSearchBookPlansSelectsAndRejects(t *testing.T) {
	emit := &captureEmitter{}
	provider := &fakeBookSearchProvider{results: map[string][]metadata.SearchResult{
		"Project Hail Mary|Andy Weir|2021|book": {
			{
				ProviderID:   "heya:book:ol_work_id:OL21745884W",
				ProviderName: "heya",
				Title:        "Project Hail Mary",
				Year:         "2021",
				Description:  "Andy Weir",
				Confidence:   0.70,
				ExternalIDs:  map[string]string{"ol_work_id": "OL21745884W"},
			},
		},
		"Bad Title|Somebody|2020|book": {
			{
				ProviderID:   "heya:book:ol_work_id:OLBAD",
				ProviderName: "heya",
				Title:        "Completely Different",
				Year:         "2020",
				Description:  "Somebody",
				Confidence:   0.70,
			},
		},
	}}
	plans := []BookPlan{
		{Key: bookIdentityKey("Andy Weir", "Project Hail Mary", "2021", "book"), Title: "Project Hail Mary", Author: "Andy Weir", Year: "2021", Format: "book", Confidence: 0.96},
		{Key: bookIdentityKey("Somebody", "Bad Title", "2020", "book"), Title: "Bad Title", Author: "Somebody", Year: "2020", Format: "book", Confidence: 0.96},
	}
	search, err := SearchBookPlans(context.Background(), plans, provider, emit)
	if err != nil {
		t.Fatalf("search books: %v", err)
	}
	if len(search) != 2 {
		t.Fatalf("book search rows: got %d, want 2", len(search))
	}
	byKey := map[string]BookSearchMatch{}
	for _, item := range search {
		byKey[item.Key] = item
	}
	project := byKey[plans[0].Key]
	if !project.Accepted || project.ProviderID != "heya:book:ol_work_id:OL21745884W" {
		t.Fatalf("Project Hail Mary search: %#v", project)
	}
	bad := byKey[plans[1].Key]
	if bad.Accepted || bad.Reason != "ambiguous_or_low_confidence" {
		t.Fatalf("bad title search: %#v", bad)
	}
}

func TestBookDatabaseFormatKeepsAudiobooksLogical(t *testing.T) {
	if got := bookDatabaseFormat(BookMaterializePreview{Format: "audiobook", FileFormat: "mp3"}); got != "audiobook" {
		t.Fatalf("audiobook database format: got %q, want audiobook", got)
	}
	if got := bookDatabaseFormat(BookMaterializePreview{Format: "book", FileFormat: "epub"}); got != "epub" {
		t.Fatalf("ebook database format: got %q, want epub", got)
	}
}

func TestApplyBookRowDefaultsMissingSubjectsToEmptyArray(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scanner-book-null-subjects-test",
		MediaType:    sqlc.MediaTypeBook,
		Paths:        []string{"/tmp/book-null-subjects"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:    lib.ID,
		MediaType:    sqlc.MediaTypeBook,
		Title:        "Zeus Is Dead",
		SortTitle:    "zeus is dead",
		ProviderKind: "heya",
	})
	require.NoError(t, err)

	detail := &metadata.MediaDetail{Title: "Zeus Is Dead", Subjects: nil}
	preview := BookMaterializePreview{Format: "audiobook", FileFormat: "m4b"}
	created, action, err := applyBookRow(ctx, q, item.ID, detail, preview)
	require.NoError(t, err)
	require.Equal(t, "create_book_row", action)
	require.NotNil(t, created.Subjects)
	require.Empty(t, created.Subjects)

	updated, action, err := applyBookRow(ctx, q, item.ID, detail, preview)
	require.NoError(t, err)
	require.Equal(t, "update_book_row", action)
	require.NotNil(t, updated.Subjects)
	require.Empty(t, updated.Subjects)
}

func assertBookPlan(t *testing.T, plans map[string]BookPlan, title, author, year, format string) {
	t.Helper()
	plan, ok := plans[title]
	if !ok {
		t.Fatalf("missing book plan %q", title)
	}
	if plan.Author != author || plan.Year != year || plan.FileFormat != format {
		t.Fatalf("%s: got author=%q year=%q file_format=%q, want author=%q year=%q file_format=%q", title, plan.Author, plan.Year, plan.FileFormat, author, year, format)
	}
}

func assertBookPlanFiles(t *testing.T, plans map[string]BookPlan, title, author, year string, files int) {
	t.Helper()
	plan, ok := plans[title]
	if !ok {
		t.Fatalf("missing audiobook plan %q", title)
	}
	if plan.Author != author || plan.Year != year || len(plan.Files) != files {
		t.Fatalf("%s: got author=%q year=%q files=%d, want author=%q year=%q files=%d", title, plan.Author, plan.Year, len(plan.Files), author, year, files)
	}
}

type fakeBookSearchProvider struct {
	results map[string][]metadata.SearchResult
}

func (p *fakeBookSearchProvider) Search(_ context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	if kind != metadata.KindBook {
		return nil, nil
	}
	key := query.Title + "|" + query.Author + "|" + query.Year + "|" + query.Format
	return p.results[key], nil
}
