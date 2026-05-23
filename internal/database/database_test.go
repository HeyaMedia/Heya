package database_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

func getTestDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://kura:kura@localhost:5440/kura?sslmode=disable"
	}
	return url
}

func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	pool, err := database.Connect(ctx, getTestDatabaseURL(t))
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	defer pool.Close()

	q := sqlc.New(pool)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	qtx := q.WithTx(tx)

	user, err := qtx.CreateUser(ctx, sqlc.CreateUserParams{
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: "$2a$10$fakehash",
		IsAdmin:      true,
	})
	if err != nil {
		t.Fatalf("creating user: %v", err)
	}
	if user.Username != "testuser" {
		t.Errorf("expected username testuser, got %s", user.Username)
	}
	if !user.IsAdmin {
		t.Error("expected user to be admin")
	}

	lib, err := qtx.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "Movies",
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{"/media/movies"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    user.ID,
		Settings:     []byte("{}"),
	})
	if err != nil {
		t.Fatalf("creating library: %v", err)
	}
	if lib.Name != "Movies" {
		t.Errorf("expected library name Movies, got %s", lib.Name)
	}

	item, err := qtx.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:   lib.ID,
		MediaType:   sqlc.MediaTypeMovie,
		Title:       "Dune: Part Two",
		SortTitle:   "dune part two",
		Year:        "2024",
		Description: "Paul Atreides unites with the Fremen.",
		ExternalIds: []byte(`{"tmdb_id": 693134}`),
	})
	if err != nil {
		t.Fatalf("creating media item: %v", err)
	}
	if item.Title != "Dune: Part Two" {
		t.Errorf("expected title 'Dune: Part Two', got %s", item.Title)
	}

	got, err := qtx.GetMediaItemByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("getting media item: %v", err)
	}
	if got.Year != "2024" {
		t.Errorf("expected year 2024, got %s", got.Year)
	}

	movie, err := qtx.CreateMovie(ctx, sqlc.CreateMovieParams{
		MediaItemID:     item.ID,
		RuntimeMinutes:  166,
		Tagline:         "Long live the fighters.",
		Genres:          []string{"Science Fiction", "Adventure"},
		Rating:          pgtype.Numeric{Valid: true},
		Popularity:      pgtype.Numeric{Valid: true},
		SpokenLanguages: []string{},
		OriginCountry:   []string{},
	})
	if err != nil {
		t.Fatalf("creating movie: %v", err)
	}
	if movie.RuntimeMinutes != 166 {
		t.Errorf("expected runtime 166, got %d", movie.RuntimeMinutes)
	}

	gotMovie, err := qtx.GetMovieByMediaItemID(ctx, item.ID)
	if err != nil {
		t.Fatalf("getting movie by media item id: %v", err)
	}
	if gotMovie.RuntimeMinutes != 166 {
		t.Errorf("expected runtime 166, got %d", gotMovie.RuntimeMinutes)
	}

	results, err := qtx.SearchAllMedia(ctx, sqlc.SearchAllMediaParams{
		Lower:  "dune",
		Limit:  10,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 search result, got 0")
	}
	found := false
	for _, r := range results {
		if r.Title == "Dune: Part Two" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected search to find 'Dune: Part Two'")
	}

	count, err := qtx.CountMediaItemsByLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	t.Log("integration test passed: user → library → media item → movie → search → count")
}
