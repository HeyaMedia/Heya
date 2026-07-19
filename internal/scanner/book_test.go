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

func TestAudiobookSingleFileAuthorTitleLayout(t *testing.T) {
	plans, err := AnalyzeBooks(context.Background(), Inventory{Roots: []InventoryRoot{{
		Root: "/audiobooks",
		Files: []InventoryFile{{
			Root: "/audiobooks", Path: "/audiobooks/Andy Weir/Project Hail Mary.m4b",
			RelPath: "Andy Weir/Project Hail Mary.m4b", Ext: ".m4b", Class: ClassPrimaryMedia,
		}},
	}}}, &captureEmitter{})
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Equal(t, "Project Hail Mary", plans[0].Title)
	require.Equal(t, "Andy Weir", plans[0].Author)
	require.Equal(t, "audiobook_author_file", plans[0].Source)
}

func TestAudiobookMultiFileFolderGroupsNumberedAndNamedChapters(t *testing.T) {
	root := t.TempDir()
	bookDir := filepath.Join(root, "A. G. Riddle", "The Long Winter")
	require.NoError(t, os.MkdirAll(bookDir, 0o755))
	for _, name := range []string{"01 - Prologue.mp3", "002.mp3", "Chapter 03 - Winter.mp3"} {
		require.NoError(t, os.WriteFile(filepath.Join(bookDir, name), []byte(name), 0o600))
	}
	inv, err := WalkInventory(context.Background(), []string{root}, &captureEmitter{})
	require.NoError(t, err)
	plans, err := AnalyzeBooks(context.Background(), inv, &captureEmitter{})
	require.NoError(t, err)
	require.Len(t, plans, 1, "chapter leaves must not become separate audiobook works")
	require.Equal(t, "The Long Winter", plans[0].Title)
	require.Equal(t, "A. G. Riddle", plans[0].Author)
	require.Equal(t, "audiobook_author_folder", plans[0].Source)
	require.Len(t, plans[0].Files, 3)
}

func TestAudiobookMultiDiscFoldersGroupAtWorkDirectory(t *testing.T) {
	root := t.TempDir()
	files := []string{
		filepath.Join("Author Name", "Book Title", "Disc 1", "01.mp3"),
		filepath.Join("Author Name", "Book Title", "Disc 2", "02.mp3"),
		filepath.Join("Other Author", "Series Name", "Other Book", "CD 1", "Chapter 01.mp3"),
		filepath.Join("Other Author", "Series Name", "Other Book", "CD 2", "Chapter 02.mp3"),
	}
	for _, rel := range files {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(rel), 0o600))
	}
	inv, err := WalkInventory(context.Background(), []string{root}, &captureEmitter{})
	require.NoError(t, err)
	plans, err := AnalyzeBooks(context.Background(), inv, &captureEmitter{})
	require.NoError(t, err)
	require.Len(t, plans, 2, "disc/CD containers must not become separate work titles")
	byTitle := make(map[string]BookPlan)
	for _, plan := range plans {
		byTitle[plan.Title] = plan
	}
	require.Equal(t, "Author Name", byTitle["Book Title"].Author)
	require.Len(t, byTitle["Book Title"].Files, 2)
	require.Equal(t, "Other Author", byTitle["Other Book"].Author)
	require.Len(t, byTitle["Other Book"].Files, 2)
}

func TestAudiobookSingleNamedChapterUsesContainingWork(t *testing.T) {
	plans, err := AnalyzeBooks(context.Background(), Inventory{Roots: []InventoryRoot{{
		Root: "/audiobooks",
		Files: []InventoryFile{{
			Root: "/audiobooks", Path: "/audiobooks/Author Name/Book Title/Chapter 01 - Prologue.mp3",
			RelPath: "Author Name/Book Title/Chapter 01 - Prologue.mp3", Ext: ".mp3", Class: ClassPrimaryMedia,
		}},
	}}}, &captureEmitter{})
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Equal(t, "Book Title", plans[0].Title)
	require.Equal(t, "Author Name", plans[0].Author)
}

func TestAudiobookSeriesHierarchyDoesNotCallSeriesTheAuthor(t *testing.T) {
	plans, err := AnalyzeBooks(context.Background(), Inventory{Roots: []InventoryRoot{{
		Root: "/audiobooks",
		Files: []InventoryFile{{
			Root: "/audiobooks", Path: "/audiobooks/A. G. Riddle/The Long Winter/The Solar War/Chapter 01.mp3",
			RelPath: "A. G. Riddle/The Long Winter/The Solar War/Chapter 01.mp3", Ext: ".mp3", Class: ClassPrimaryMedia,
		}},
	}}}, &captureEmitter{})
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Equal(t, "The Solar War", plans[0].Title)
	require.Equal(t, "A. G. Riddle", plans[0].Author)
	require.NotEqual(t, "The Long Winter", plans[0].Author)
	require.Equal(t, "audiobook_author_series_folder", plans[0].Source)
	require.LessOrEqual(t, plans[0].Confidence, 0.72, "inferred series nesting must remain review-level")
}

func TestBookIdentityKeyPreservesArticlesAndAuthorInitials(t *testing.T) {
	want := "book:book|a g riddle|the martian|2021"
	require.Equal(t, want, bookIdentityKey("A. G. Riddle", "The Martian", "2021", "book"))
	require.NotEqual(t,
		bookIdentityKey("A. G. Riddle", "The Martian", "2021", "book"),
		bookIdentityKey("G. Riddle", "Martian", "2021", "book"),
		"durable identities must not apply article-insensitive search normalization",
	)
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
	search, err := SearchBookPlans(context.Background(), plans, provider, emit, 0)
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

func TestSearchBookPlansUsesAuthorEvidenceToDisambiguateExactTitles(t *testing.T) {
	provider := &fakeBookSearchProvider{results: map[string][]metadata.SearchResult{
		"Artemis|Andy Weir||audiobook": {
			{
				ProviderID:     "heya:book:wrong-artemis",
				ProviderName:   "heya",
				Title:          "Artemis",
				Confidence:     0.80,
				RequiresReview: true,
				Evidence: []metadata.SearchEvidence{
					{Field: "title", Outcome: "exact", Detail: "Artemis"},
					{Field: "authors", Outcome: "0_of_1", Detail: "Julian Stockwin"},
				},
			},
			{
				ProviderID:     "heya:book:andy-weir-artemis",
				ProviderName:   "heya",
				Title:          "Artemis",
				Confidence:     0.75,
				RequiresReview: false,
				Evidence: []metadata.SearchEvidence{
					{Field: "title", Outcome: "exact", Detail: "Artemis"},
					{Field: "authors", Outcome: "1_of_1", Detail: "Andy Weir"},
				},
			},
		},
	}}
	plan := BookPlan{
		Key:   bookIdentityKey("Andy Weir", "Artemis", "", "audiobook"),
		Title: "Artemis", Author: "Andy Weir", Format: "audiobook", Confidence: 0.72,
	}

	search, err := SearchBookPlans(context.Background(), []BookPlan{plan}, provider, &captureEmitter{}, 0)
	require.NoError(t, err)
	require.Len(t, search, 1)
	require.True(t, search[0].Accepted)
	require.Equal(t, "heya:book:andy-weir-artemis", search[0].ProviderID)
	require.Equal(t, "Andy Weir", search[0].Author)
	require.Equal(t, "Andy Weir", search[0].Candidates[0].Author)
	require.Greater(t, search[0].Candidates[0].Confidence, search[0].Candidates[1].Confidence)
}

func TestSearchBookPlansRequiresAuthorCorroborationForExactTitle(t *testing.T) {
	provider := &fakeBookSearchProvider{results: map[string][]metadata.SearchResult{
		"Artemis|Andy Weir||audiobook": {{
			ProviderID: "heya:book:same-title-no-author", ProviderName: "heya",
			Title: "Artemis", Confidence: 1,
		}},
	}}
	plan := BookPlan{
		Key:   bookIdentityKey("Andy Weir", "Artemis", "", "audiobook"),
		Title: "Artemis", Author: "Andy Weir", Format: "audiobook", Confidence: 0.84,
	}

	search, err := SearchBookPlans(context.Background(), []BookPlan{plan}, provider, &captureEmitter{}, 0)
	require.NoError(t, err)
	require.Len(t, search, 1)
	require.False(t, search[0].Accepted, "an exact title without remote author evidence is not an identity")
	require.Equal(t, "ambiguous_or_low_confidence", search[0].Reason)
}

func TestBookAuthorCorroborationUsesNameBoundariesAndCompactsInitials(t *testing.T) {
	require.True(t, bookAuthorCorroborated("A. G. Riddle", "AG Riddle"))
	require.True(t, bookAuthorCorroborated("A. G. Riddle", "A G Riddle"))
	require.True(t, bookAuthorCorroborated("Local Author", "Local Author, Coauthor"))
	require.False(t, bookAuthorCorroborated("Ann Smith", "Joanne Smith"))
	require.False(t, bookAuthorCorroborated("Andy Weir", "Andy Griffith"))
}

func TestSearchBookPlansAcceptsOneAuthorFromCandidateAuthorList(t *testing.T) {
	plan := BookPlan{
		Key:   bookIdentityKey("Local Author", "Shared Work", "", "audiobook"),
		Title: "Shared Work", Author: "Local Author", Format: "audiobook", Confidence: 0.84,
	}
	provider := &fakeBookSearchProvider{results: map[string][]metadata.SearchResult{
		"Shared Work|Local Author||audiobook": {{
			ProviderID: "heya:book:shared-work", ProviderName: "heya", Title: "Shared Work", Confidence: 0.8,
			Evidence: []metadata.SearchEvidence{{Field: "authors", Outcome: "1_of_2", Detail: "Local Author, Coauthor"}},
		}},
	}}
	search, err := SearchBookPlans(context.Background(), []BookPlan{plan}, provider, &captureEmitter{}, 0)
	require.NoError(t, err)
	require.Len(t, search, 1)
	require.True(t, search[0].Accepted)
	require.Equal(t, "heya:book:shared-work", search[0].ProviderID)
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
