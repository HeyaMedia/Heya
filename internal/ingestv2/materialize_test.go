package ingestv2

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

type fakeMovieMaterializeStore struct {
	itemsByTMDB map[string]sqlc.MediaItemCard
	itemsByID   map[int64]sqlc.MediaItemCard
	movies      map[int64]sqlc.Movie
	files       map[string]sqlc.LibraryFile
}

func (f *fakeMovieMaterializeStore) FindMediaItemByExternalIDs(_ context.Context, _ int64, ids map[string]string) (sqlc.MediaItemCard, bool, error) {
	if item, ok := f.itemsByTMDB[ids["tmdb"]]; ok {
		return item, true, nil
	}
	return sqlc.MediaItemCard{}, false, nil
}

func (f *fakeMovieMaterializeStore) FindMediaItemByIdentity(_ context.Context, _ int64, title, year string) (sqlc.MediaItemCard, bool, error) {
	return sqlc.MediaItemCard{}, false, nil
}

func (f *fakeMovieMaterializeStore) GetMovieByMediaItemID(_ context.Context, mediaItemID int64) (sqlc.Movie, bool, error) {
	movie, ok := f.movies[mediaItemID]
	return movie, ok, nil
}

func (f *fakeMovieMaterializeStore) GetMediaItemByID(_ context.Context, mediaItemID int64) (sqlc.MediaItemCard, bool, error) {
	item, ok := f.itemsByID[mediaItemID]
	return item, ok, nil
}

func (f *fakeMovieMaterializeStore) GetLibraryFileByPath(_ context.Context, _ int64, path string) (sqlc.LibraryFile, bool, error) {
	file, ok := f.files[path]
	return file, ok, nil
}

func TestPlanMovieMaterializationPreviewsWritesAndRepairsStaleAttachments(t *testing.T) {
	result := Result{
		Inventory: Inventory{Roots: []InventoryRoot{{
			Root: "/library",
			Files: []InventoryFile{
				{RelPath: "Dune (2021)/Dune.mkv", Path: "/library/Dune (2021)/Dune.mkv"},
				{RelPath: "Alien (1979)/Alien.mkv", Path: "/library/Alien (1979)/Alien.mkv"},
			},
		}}},
		MovieMatches: []MovieMatch{
			{Key: "tmdb:438631", Title: "Dune", Year: "2021", ExternalIDs: map[string]string{"tmdb": "438631"}, Files: []string{"Dune (2021)/Dune.mkv"}},
			{Key: "tmdb:348", Title: "Alien", Year: "1979", ExternalIDs: map[string]string{"tmdb": "348"}, Files: []string{"Alien (1979)/Alien.mkv"}},
			{Key: "title_year:bad|2024", Title: "Bad", Year: "2024"},
		},
		MovieSearch: []MovieSearchMatch{
			{Accepted: true, Key: "tmdb:438631", ProviderID: "heya:movie:tmdb:438631", Title: "Dune", Year: "2021", ExternalIDs: map[string]string{"tmdb": "438631"}},
			{Accepted: true, Key: "tmdb:348", ProviderID: "heya:movie:tmdb:348", Title: "Alien", Year: "1979", ExternalIDs: map[string]string{"tmdb": "348"}},
			{Accepted: false, Key: "title_year:bad|2024", Title: "Bad", Year: "2024", Reason: "no_candidates"},
		},
		MovieMetadata: []MovieFetchPreview{
			{Key: "tmdb:438631", ProviderID: "heya:movie:tmdb:438631", Title: "Dune", Year: "2021", ExternalIDs: map[string]string{"tmdb": "438631"}, WouldApply: []string{"title", "year"}, Collection: "Dune Collection", Artwork: 2, Cast: 3},
			{Key: "tmdb:348", ProviderID: "heya:movie:tmdb:348", Title: "Alien", Year: "1979", ExternalIDs: map[string]string{"tmdb": "348"}, WouldApply: []string{"title", "year"}},
		},
	}
	store := &fakeMovieMaterializeStore{
		itemsByTMDB: map[string]sqlc.MediaItemCard{
			"438631": {ID: 42, Title: "Dune", Year: "2021"},
		},
		itemsByID: map[int64]sqlc.MediaItemCard{
			99: {
				ID:           99,
				MediaType:    sqlc.MediaTypeMovie,
				Title:        "Wrong Alien",
				Year:         "1980",
				ExternalIds:  mustJSONBytes(map[string]string{"tmdb": "1"}),
				ProviderKind: "heya",
			},
		},
		movies: map[int64]sqlc.Movie{},
		files: map[string]sqlc.LibraryFile{
			"/library/Dune (2021)/Dune.mkv":   {ID: 7, Status: sqlc.FileStatusPending},
			"/library/Alien (1979)/Alien.mkv": {ID: 8, Status: sqlc.FileStatusMatched, MediaItemID: pgtype.Int8{Int64: 99, Valid: true}},
		},
	}

	previews, err := PlanMovieMaterialization(context.Background(), sqlc.Library{ID: 3, MediaType: sqlc.MediaTypeMovie}, result, store, &captureEmitter{})
	if err != nil {
		t.Fatalf("plan materialization: %v", err)
	}
	if len(previews) != 3 {
		t.Fatalf("previews: got %d, want 3: %#v", len(previews), previews)
	}

	byKey := map[string]MovieMaterializePreview{}
	for _, preview := range previews {
		byKey[preview.Key] = preview
	}
	dune := byKey["tmdb:438631"]
	if dune.Action != "update" || dune.MediaItemAction != "update_media_item" || dune.MovieRowAction != "create_movie_row" {
		t.Fatalf("dune actions: %#v", dune)
	}
	if len(dune.FileActions) != 1 || dune.FileActions[0].Action != "attach_existing_library_file" {
		t.Fatalf("dune file actions: %#v", dune.FileActions)
	}
	if dune.Collection != "Dune Collection" || dune.RemoteArtwork != 2 || dune.Cast != 3 {
		t.Fatalf("dune metadata summary: %#v", dune)
	}

	alien := byKey["tmdb:348"]
	if alien.Action != "repair" || alien.Reason != "stale_file_attachment" {
		t.Fatalf("alien should be planned as stale attachment repair: %#v", alien)
	}
	if len(alien.FileActions) != 1 || alien.FileActions[0].Action != "reassign_library_file" {
		t.Fatalf("alien file actions: %#v", alien.FileActions)
	}
	if alien.FileActions[0].ExistingItem == nil || alien.FileActions[0].ExistingItem.Title != "Wrong Alien" {
		t.Fatalf("alien repair should include existing media item detail: %#v", alien.FileActions[0])
	}

	rejected := byKey["title_year:bad|2024"]
	if rejected.Action != "blocked" || rejected.Reason != "search_rejected" {
		t.Fatalf("rejected search should block materialization: %#v", rejected)
	}
}
