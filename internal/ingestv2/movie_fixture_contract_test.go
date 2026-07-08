package ingestv2

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

type movieFixtureRemote struct {
	QueryTitle string
	QueryYear  string
	Title      string
	Year       string
	TMDB       string
	IMDB       string
	Collection string
}

func TestMovieFixtureV2ContractPreApplyAndIdempotentMaterialization(t *testing.T) {
	ctx := context.Background()
	movieDir := filepath.Join(testdataRoot(t), "library", "movies")
	if _, err := os.Stat(movieDir); os.IsNotExist(err) {
		t.Skip("testdata/library/movies not found")
	}

	emit := &captureEmitter{}
	inv, err := WalkInventory(ctx, []string{movieDir}, emit)
	if err != nil {
		t.Fatalf("walk inventory: %v", err)
	}
	if got := countInventoryFiles(inv); got != 109 {
		t.Fatalf("classified files: got %d, want 109", got)
	}
	if got := len(inventoryFilesByClass(inv, ClassExtraMedia)); got != 6 {
		t.Fatalf("local extras: got %d, want 6", got)
	}
	if got := countEvents(emit.events, "movie.file.unplanned"); got != 0 {
		t.Fatalf("movie.file.unplanned should not exist before movie analysis: got %d", got)
	}

	plans, err := AnalyzeMovies(ctx, inv, emit)
	if err != nil {
		t.Fatalf("analyze movies: %v", err)
	}
	if len(plans) != 25 {
		t.Fatalf("movie plans: got %d, want 25", len(plans))
	}
	if got := countEvents(emit.events, "movie.file.unplanned"); got != 4 {
		t.Fatalf("unplanned movie media events: got %d, want 4", got)
	}
	if got := countEvents(emit.events, "nfo.parse_failed"); got != 1 {
		t.Fatalf("nfo parse failures: got %d, want 1", got)
	}

	matches, err := AnalyzeMovieMatches(ctx, plans, emit)
	if err != nil {
		t.Fatalf("match movies: %v", err)
	}
	if len(matches) != 25 {
		t.Fatalf("local identities: got %d, want 25", len(matches))
	}

	remotes := movieFixtureRemotes()
	searchProvider := movieFixtureSearchProvider(remotes)
	search, err := SearchMovieMatches(ctx, matches, searchProvider, emit)
	if err != nil {
		t.Fatalf("search movies: %v", err)
	}
	if got := countAcceptedMovieSearch(search); got != 23 {
		t.Fatalf("accepted search results: got %d, want 23", got)
	}
	if got := countRejectedMovieSearch(search); got != 2 {
		t.Fatalf("rejected search results: got %d, want 2", got)
	}
	assertSearchRejected(t, search, "tmdb:999001", "no_candidates")
	assertSearchRejected(t, search, "tmdb:1234567", "no_candidates")
	assertSearchSelected(t, search, "title_year:jackass presents bad grandpa 5|2014", "heya:movie:tmdb:273641")

	detailProvider := movieFixtureDetailProvider(remotes)
	fetch, err := FetchMovieMetadataPreviews(ctx, search, detailProvider, emit)
	if err != nil {
		t.Fatalf("fetch metadata previews: %v", err)
	}
	if got := countFetchedMovieMetadata(fetch); got != 23 {
		t.Fatalf("fetched metadata: got %d, want 23", got)
	}

	result := Result{
		Inventory:     inv,
		Movies:        plans,
		MovieMatches:  matches,
		MovieSearch:   search,
		MovieMetadata: fetch,
	}

	preApply, err := PlanMovieMaterialization(ctx, sqlc.Library{ID: 3, Name: "DevMovies", MediaType: sqlc.MediaTypeMovie}, result, movieFixturePreApplyStore(t, inv, matches, remotes), emit)
	if err != nil {
		t.Fatalf("pre-apply materialization: %v", err)
	}
	assertMovieMaterializeCounts(t, preApply, map[string]int{
		"blocked": 2,
		"create":  10,
		"repair":  1,
		"update":  12,
	})
	assertMaterializeAction(t, preApply, "title_year:jackass presents bad grandpa 5|2014", "repair", "reassign_library_file")
	assertMaterializeAction(t, preApply, "tmdb:999001", "blocked", "")
	assertMaterializeAction(t, preApply, "tmdb:1234567", "blocked", "")

	result.MovieMaterialize = preApply
	var report bytes.Buffer
	WriteReport(&report, sqlc.Library{ID: 3, Name: "DevMovies", MediaType: sqlc.MediaTypeMovie}, result, emit.events)
	for _, want := range []string{
		"Classified files: 109",
		"Movie plans:      25",
		"Local identities: 25",
		"Search selected:  23/25",
		"Search review:    2 rejected, 0 suspicious selected",
		"Unplanned media:  4",
		"Local extras:     6",
		"NFO failures:     1",
		"Metadata fetched: 23/23",
		"Materialize:      10 create, 12 update, 1 repair, 2 blocked",
		"repair Jackass Presents: Bad Grandpa .5 (2014) provider=heya:movie:tmdb:273641",
		"Actually Corrected By NFO (2021) [tmdb:999001] rejected reason=no_candidates",
		"TMDb Only Folder (2024) [tmdb:1234567] rejected reason=no_candidates",
		"Mad Max Fury Road (2015)/sample.mkv type=sample",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("pre-apply report missing %q:\n%s", want, report.String())
		}
	}

	postApply, err := PlanMovieMaterialization(ctx, sqlc.Library{ID: 3, Name: "DevMovies", MediaType: sqlc.MediaTypeMovie}, result, movieFixturePostApplyStore(t, inv, matches, remotes), emit)
	if err != nil {
		t.Fatalf("post-apply materialization: %v", err)
	}
	assertMovieMaterializeCounts(t, postApply, map[string]int{
		"blocked": 2,
		"create":  0,
		"repair":  0,
		"update":  23,
	})
	assertMaterializeAction(t, postApply, "title_year:jackass presents bad grandpa 5|2014", "update", "already_attached")
}

func movieFixtureRemotes() []movieFixtureRemote {
	return []movieFixtureRemote{
		{QueryTitle: "A Goofy Movie", QueryYear: "1995", Title: "A Goofy Movie", Year: "1995", TMDB: "15789", IMDB: "tt0113198", Collection: "A Goofy Movie Collection"},
		{QueryTitle: "Alien", QueryYear: "1979", Title: "Alien", Year: "1979", TMDB: "348", IMDB: "tt0078748", Collection: "Alien Collection"},
		{QueryTitle: "Aliens", QueryYear: "1986", Title: "Aliens", Year: "1986", TMDB: "679", IMDB: "tt0090605", Collection: "Alien Collection"},
		{QueryTitle: "Alien", QueryYear: "1992", Title: "Alien³", Year: "1992", TMDB: "8077", IMDB: "tt0103644", Collection: "Alien Collection"},
		{QueryTitle: "Anora", QueryYear: "2024", Title: "Anora", Year: "2024", TMDB: "1064213", IMDB: "tt28607951"},
		{QueryTitle: "Avatar The Way of Water", QueryYear: "2022", Title: "Avatar: The Way of Water", Year: "2022", TMDB: "76600", IMDB: "tt1630029", Collection: "Avatar Collection"},
		{QueryTitle: "Blade Runner", QueryYear: "1982", Title: "Blade Runner", Year: "1982", TMDB: "78", IMDB: "tt0083658", Collection: "Blade Runner Collection"},
		{QueryTitle: "Blade Runner 2049", QueryYear: "2017", Title: "Blade Runner 2049", Year: "2017", TMDB: "335984", IMDB: "tt1856101", Collection: "Blade Runner Collection"},
		{QueryTitle: "Dune", QueryYear: "2021", Title: "Dune", Year: "2021", TMDB: "438631", IMDB: "tt1160419", Collection: "Dune Collection"},
		{QueryTitle: "Dune Part Two", QueryYear: "2024", Title: "Dune: Part Two", Year: "2024", TMDB: "693134", IMDB: "tt15239678", Collection: "Dune Collection"},
		{QueryTitle: "Everything Everywhere All at Once", QueryYear: "2022", Title: "Everything Everywhere All at Once", Year: "2022", TMDB: "545611", IMDB: "tt6710474"},
		{QueryTitle: "Jackass Presents Bad Grandpa .5", QueryYear: "2014", Title: "Jackass Presents: Bad Grandpa .5", Year: "2014", TMDB: "273641", IMDB: "tt3766424", Collection: "Jackass Presents: Bad Grandpa Collection"},
		{QueryTitle: "Kill Bill Vol 1", QueryYear: "2003", Title: "Kill Bill: Vol. 1", Year: "2003", TMDB: "24", IMDB: "tt0266697", Collection: "Kill Bill Collection"},
		{QueryTitle: "Mad Max: Fury Road", QueryYear: "2015", Title: "Mad Max: Fury Road", Year: "2015", TMDB: "76341", IMDB: "tt1392190", Collection: "Mad Max Collection"},
		{QueryTitle: "Naked Gun 33 1/3: The Final Insult", QueryYear: "1994", Title: "Naked Gun 33⅓: The Final Insult", Year: "1994", TMDB: "36593", IMDB: "tt0110622", Collection: "The Naked Gun Collection"},
		{QueryTitle: "Nope", QueryYear: "2022", Title: "Nope", Year: "2022", TMDB: "762504", IMDB: "tt10954984"},
		{QueryTitle: "Oppenheimer", QueryYear: "2023", Title: "Oppenheimer", Year: "2023", TMDB: "872585", IMDB: "tt15398776"},
		{QueryTitle: "The Lord of the Rings: The Fellowship of the Ring", QueryYear: "2001", Title: "The Lord of the Rings: The Fellowship of the Ring", Year: "2001", TMDB: "120", IMDB: "tt0120737", Collection: "The Lord of the Rings Collection"},
		{QueryTitle: "The Matrix", QueryYear: "1999", Title: "The Matrix", Year: "1999", TMDB: "603", IMDB: "tt0133093", Collection: "The Matrix Collection"},
		{QueryTitle: "The Matrix Reloaded", QueryYear: "2003", Title: "The Matrix Reloaded", Year: "2003", TMDB: "604", IMDB: "tt0234215", Collection: "The Matrix Collection"},
		{QueryTitle: "The Naked Gun", QueryYear: "2025", Title: "The Naked Gun", Year: "2025", TMDB: "1035259", IMDB: "tt3402138", Collection: "The Naked Gun Collection"},
		{QueryTitle: "Thunderbolts", QueryYear: "2025", Title: "Thunderbolts*", Year: "2025", TMDB: "986056", IMDB: "tt20969586"},
		{QueryTitle: "Your Name.", QueryYear: "2016", Title: "Your Name.", Year: "2016", TMDB: "372058", IMDB: "tt5311514"},
	}
}

func movieFixtureSearchProvider(remotes []movieFixtureRemote) *fakeMovieSearchProvider {
	results := map[string][]metadata.SearchResult{}
	for _, remote := range remotes {
		results[remote.QueryTitle+"\x00"+remote.QueryYear] = []metadata.SearchResult{{
			ProviderID:   "heya:movie:tmdb:" + remote.TMDB,
			ProviderName: "heya",
			Title:        remote.Title,
			Year:         remote.Year,
			ExternalIDs:  movieFixtureExternalIDs(remote),
		}}
	}
	return &fakeMovieSearchProvider{results: results}
}

func movieFixtureDetailProvider(remotes []movieFixtureRemote) *fakeMovieDetailProvider {
	details := map[string]*metadata.MediaDetail{}
	for i, remote := range remotes {
		providerID := "heya:movie:tmdb:" + remote.TMDB
		detail := &metadata.MediaDetail{
			Title:          remote.Title,
			Year:           remote.Year,
			SortTitle:      strings.ToLower(remote.Title),
			Description:    remote.Title + " fixture overview.",
			ExternalIDs:    movieFixtureExternalIDs(remote),
			RuntimeMinutes: 90 + i,
			Genres:         []string{"Fixture"},
			PosterURL:      "https://img.heya.test/" + remote.TMDB + "/poster.jpg",
			BackdropURL:    "https://img.heya.test/" + remote.TMDB + "/backdrop.jpg",
			ProviderKind:   "tmdb",
			HeyaSlug:       "movie-" + remote.TMDB,
			Artwork: []metadata.ArtworkResult{
				{URL: "https://img.heya.test/" + remote.TMDB + "/logo.png", AssetType: "logo"},
				{URL: "https://img.heya.test/" + remote.TMDB + "/backdrop-extra.jpg", AssetType: "backdrop"},
			},
			Cast: []metadata.CastMember{{Name: "Fixture Cast", Character: "Self"}},
			Crew: []metadata.CrewMember{{Name: "Fixture Director", Job: "Director", Department: "Directing"}},
		}
		if remote.Collection != "" {
			detail.Collection = &metadata.CollectionDetail{Name: remote.Collection}
		}
		details[providerID] = detail
	}
	return &fakeMovieDetailProvider{details: details}
}

func movieFixtureExternalIDs(remote movieFixtureRemote) map[string]string {
	ids := map[string]string{"tmdb": remote.TMDB}
	if remote.IMDB != "" {
		ids["imdb"] = remote.IMDB
	}
	return ids
}

func movieFixturePreApplyStore(t *testing.T, inv Inventory, matches []MovieMatch, remotes []movieFixtureRemote) *fakeMovieMaterializeStore {
	t.Helper()
	store := newMovieFixtureMaterializeStore()
	remoteByIdentity := movieFixtureRemoteByIdentity(remotes)
	filesByRel := inventoryFilesByRel(inv)
	preExisting := map[string]int64{
		"A Goofy Movie\x001995":                                     114,
		"Anora\x002024":                                             117,
		"Avatar The Way of Water\x002022":                           116,
		"Everything Everywhere All at Once\x002022":                 118,
		"Kill Bill Vol 1\x002003":                                   126,
		"Mad Max: Fury Road\x002015":                                122,
		"Naked Gun 33 1/3: The Final Insult\x001994":                120,
		"Nope\x002022":                                              130,
		"Oppenheimer\x002023":                                       128,
		"The Lord of the Rings: The Fellowship of the Ring\x002001": 125,
		"The Naked Gun\x002025":                                     119,
		"Thunderbolts\x002025":                                      115,
	}

	fileID := int64(1000)
	for _, match := range matches {
		identity := match.Title + "\x00" + match.Year
		remote, ok := remoteByIdentity[identity]
		if !ok {
			continue
		}
		if itemID, exists := preExisting[identity]; exists {
			store.addExistingMovie(itemID, remote)
			for _, relPath := range match.Files {
				fileID++
				store.addLibraryFile(t, filesByRel, relPath, fileID, itemID)
			}
			continue
		}
		if identity == "Jackass Presents Bad Grandpa .5\x002014" {
			store.itemsByID[121] = sqlc.MediaItemCard{
				ID:          121,
				MediaType:   sqlc.MediaTypeMovie,
				Title:       "Jackass Presents: Bad Grandpa",
				Year:        "2013",
				ExternalIds: mustJSONBytes(map[string]string{"tmdb": "208134"}),
			}
			for _, relPath := range match.Files {
				fileID++
				store.addLibraryFile(t, filesByRel, relPath, fileID, 121)
			}
		}
	}
	return store
}

func movieFixturePostApplyStore(t *testing.T, inv Inventory, matches []MovieMatch, remotes []movieFixtureRemote) *fakeMovieMaterializeStore {
	t.Helper()
	store := newMovieFixtureMaterializeStore()
	remoteByIdentity := movieFixtureRemoteByIdentity(remotes)
	filesByRel := inventoryFilesByRel(inv)

	itemID := int64(2000)
	fileID := int64(3000)
	for _, match := range matches {
		remote, ok := remoteByIdentity[match.Title+"\x00"+match.Year]
		if !ok {
			continue
		}
		itemID++
		store.addExistingMovie(itemID, remote)
		for _, relPath := range match.Files {
			fileID++
			store.addLibraryFile(t, filesByRel, relPath, fileID, itemID)
		}
	}
	return store
}

func newMovieFixtureMaterializeStore() *fakeMovieMaterializeStore {
	return &fakeMovieMaterializeStore{
		itemsByTMDB: map[string]sqlc.MediaItemCard{},
		itemsByID:   map[int64]sqlc.MediaItemCard{},
		movies:      map[int64]sqlc.Movie{},
		files:       map[string]sqlc.LibraryFile{},
	}
}

func (f *fakeMovieMaterializeStore) addExistingMovie(id int64, remote movieFixtureRemote) {
	item := sqlc.MediaItemCard{
		ID:          id,
		MediaType:   sqlc.MediaTypeMovie,
		Title:       remote.Title,
		Year:        remote.Year,
		ExternalIds: mustJSONBytes(movieFixtureExternalIDs(remote)),
	}
	f.itemsByTMDB[remote.TMDB] = item
	f.itemsByID[id] = item
	f.movies[id] = sqlc.Movie{ID: id, MediaItemID: id}
}

func (f *fakeMovieMaterializeStore) addLibraryFile(t *testing.T, filesByRel map[string][]InventoryFile, relPath string, fileID, mediaItemID int64) {
	t.Helper()
	invFile, ok := singleInventoryFile(filesByRel, relPath)
	if !ok {
		t.Fatalf("missing fixture inventory file %s", relPath)
	}
	f.files[invFile.Path] = sqlc.LibraryFile{
		ID:          fileID,
		LibraryID:   3,
		Path:        invFile.Path,
		Status:      sqlc.FileStatusMatched,
		MediaItemID: pgtype.Int8{Int64: mediaItemID, Valid: true},
	}
}

func movieFixtureRemoteByIdentity(remotes []movieFixtureRemote) map[string]movieFixtureRemote {
	out := map[string]movieFixtureRemote{}
	for _, remote := range remotes {
		out[remote.QueryTitle+"\x00"+remote.QueryYear] = remote
	}
	return out
}

func assertMovieMaterializeCounts(t *testing.T, previews []MovieMaterializePreview, want map[string]int) {
	t.Helper()
	got := map[string]int{"blocked": 0, "create": 0, "repair": 0, "update": 0}
	for _, preview := range previews {
		got[preview.Action]++
	}
	for action, wantCount := range want {
		if got[action] != wantCount {
			t.Fatalf("materialize %s count: got %d, want %d (all=%#v)", action, got[action], wantCount, got)
		}
	}
}

func assertMaterializeAction(t *testing.T, previews []MovieMaterializePreview, key, action, fileAction string) {
	t.Helper()
	for _, preview := range previews {
		if preview.Key != key {
			continue
		}
		if preview.Action != action {
			t.Fatalf("%s action: got %q, want %q: %#v", key, preview.Action, action, preview)
		}
		if fileAction == "" {
			return
		}
		if !hasMovieFileAction(preview.FileActions, fileAction) {
			t.Fatalf("%s file actions missing %q: %#v", key, fileAction, preview.FileActions)
		}
		return
	}
	t.Fatalf("missing materialization preview %s", key)
}

func assertSearchRejected(t *testing.T, search []MovieSearchMatch, key, reason string) {
	t.Helper()
	for _, item := range search {
		if item.Key != key {
			continue
		}
		if item.Accepted || item.Reason != reason {
			t.Fatalf("%s search rejection: got accepted=%t reason=%q, want false/%q", key, item.Accepted, item.Reason, reason)
		}
		return
	}
	t.Fatalf("missing search result %s", key)
}

func assertSearchSelected(t *testing.T, search []MovieSearchMatch, key, providerID string) {
	t.Helper()
	for _, item := range search {
		if item.Key != key {
			continue
		}
		if !item.Accepted || item.ProviderID != providerID {
			t.Fatalf("%s search selection: got accepted=%t provider=%q, want true/%q", key, item.Accepted, item.ProviderID, providerID)
		}
		return
	}
	t.Fatalf("missing search result %s", key)
}

func countRejectedMovieSearch(search []MovieSearchMatch) int {
	n := 0
	for _, item := range search {
		if !item.Accepted {
			n++
		}
	}
	return n
}

func countEvents(events []Event, name string) int {
	n := 0
	for _, event := range events {
		if event.Event == name {
			n++
		}
	}
	return n
}
