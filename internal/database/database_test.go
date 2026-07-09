package database_test

import (
	"context"
	"math/big"
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
		url = "postgres://heya:heya@localhost:5440/heya?sslmode=disable"
	}
	return url
}

func TestAllHostsLocalClassification(t *testing.T) {
	cases := []struct {
		name  string
		conn  string
		local bool
	}{
		{"localhost", "postgres://heya:heya@localhost:5440/heya_dev?sslmode=disable", true},
		{"loopback v4", "postgres://heya:heya@127.0.0.1:5440/heya", true},
		{"loopback v6", "postgres://heya:pw@[::1]:5432/heya", true},
		{"unix socket host param", "postgres:///heya?host=/var/run/postgresql", true},
		{"remote authority", "postgres://heya:pw@knas-heya-postgres.drum-ray.ts.net:5432/heya?sslmode=disable", false},
		{"remote dsn keyword", "host=knas-heya-postgres.drum-ray.ts.net port=5432 user=heya dbname=heya", false},
		// pgx dials a leading "@" host as TCP (only "/" is a unix socket), so it
		// must NOT be classified local.
		{"at-prefixed host is tcp", "host=@evil.example.com port=5432 user=heya dbname=heya", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			local, host, err := database.AllHostsLocal(c.conn)
			if err != nil {
				t.Fatalf("AllHostsLocal(%q): %v", c.conn, err)
			}
			if local != c.local {
				t.Errorf("AllHostsLocal(%q) = %v (host %q); want %v", c.conn, local, host, c.local)
			}
		})
	}
}

// TestAllHostsLocalSeesPGHOST is the bypass the old url.Parse check missed: a
// host-less URL parses to an empty (local-looking) host, but pgx resolves the
// real host from PGHOST. The guard must classify what pgx actually dials.
func TestAllHostsLocalSeesPGHOST(t *testing.T) {
	t.Setenv("PGHOST", "knas-prod.example.com")
	local, host, err := database.AllHostsLocal("postgres:///heya_dev")
	if err != nil {
		t.Fatalf("AllHostsLocal: %v", err)
	}
	if local {
		t.Errorf("expected non-local via PGHOST, got local (host=%q)", host)
	}
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

func TestUpdateMediaItemRawExternalIDsIsIdempotent(t *testing.T) {
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
		Username:     "externalidtest",
		Email:        "externalidtest@example.com",
		PasswordHash: "$2a$10$fakehash",
		IsAdmin:      true,
	})
	if err != nil {
		t.Fatalf("creating user: %v", err)
	}
	lib, err := qtx.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "Music",
		MediaType:    sqlc.MediaTypeMusic,
		Paths:        []string{"/media/music"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    user.ID,
		Settings:     []byte("{}"),
	})
	if err != nil {
		t.Fatalf("creating library: %v", err)
	}

	itemID, err := qtx.CreateMediaItemRaw(ctx, sqlc.CreateMediaItemRawParams{
		LibraryID:        lib.ID,
		MediaType:        sqlc.MediaTypeMusic,
		ProviderKind:     "artist",
		Title:            "Axwell Λ Ingrosso",
		SortTitle:        "axwell ingrosso",
		Year:             "",
		Description:      "",
		PosterPath:       "",
		BackdropPath:     "",
		Tagline:          "",
		OriginalTitle:    "",
		OriginalLanguage: "",
		Status:           "",
		ExternalIds:      []byte(`{"mbid":"old-mbid","discogs":"123"}`),
	})
	if err != nil {
		t.Fatalf("creating media item raw: %v", err)
	}

	params := sqlc.UpdateMediaItemRawParams{
		ID:               itemID,
		ProviderKind:     "artist",
		Title:            "Axwell Λ Ingrosso",
		SortTitle:        "axwell ingrosso",
		Year:             "",
		Description:      "",
		PosterPath:       "",
		BackdropPath:     "",
		Tagline:          "",
		OriginalTitle:    "",
		OriginalLanguage: "",
		Status:           "",
		ExternalIds:      []byte(`{"mbid":"new-mbid"}`),
	}
	for i := 0; i < 2; i++ {
		if _, err := qtx.UpdateMediaItemRaw(ctx, params); err != nil {
			t.Fatalf("updating media item raw pass %d: %v", i+1, err)
		}
	}

	var count int
	var mbid string
	if err := tx.QueryRow(ctx, `
		SELECT count(*), COALESCE(max(external_id) FILTER (WHERE provider = 'mbid'), '')
		FROM media_item_external_ids
		WHERE media_item_id = $1
	`, itemID).Scan(&count, &mbid); err != nil {
		t.Fatalf("querying external ids: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one external id after replacement, got %d", count)
	}
	if mbid != "new-mbid" {
		t.Fatalf("expected mbid to be updated, got %q", mbid)
	}
}

func TestRecommendedTVRailsIncludeAnime(t *testing.T) {
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
		Username:     "recommended-tv-anime",
		Email:        "recommended-tv-anime@example.com",
		PasswordHash: "$2a$10$fakehash",
		IsAdmin:      true,
	})
	if err != nil {
		t.Fatalf("creating user: %v", err)
	}
	tvLib, err := qtx.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "TV",
		MediaType:    sqlc.MediaTypeTv,
		Paths:        []string{"/media/tv"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    user.ID,
		Settings:     []byte("{}"),
	})
	if err != nil {
		t.Fatalf("creating tv library: %v", err)
	}
	animeLib, err := qtx.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "Anime",
		MediaType:    sqlc.MediaTypeAnime,
		Paths:        []string{"/media/anime"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    user.ID,
		Settings:     []byte("{}"),
	})
	if err != nil {
		t.Fatalf("creating anime library: %v", err)
	}

	const uniqueGenre = "RecommendedRailFixture"
	createSeries := func(title, slug, tmdb string, libID int64, mediaType sqlc.MediaType, rating int64) int64 {
		t.Helper()
		itemID, err := qtx.CreateMediaItemRaw(ctx, sqlc.CreateMediaItemRawParams{
			LibraryID:        libID,
			MediaType:        mediaType,
			ProviderKind:     "tv",
			Title:            title,
			SortTitle:        title,
			Year:             "2024",
			Description:      "",
			PosterPath:       "",
			BackdropPath:     "",
			Tagline:          "",
			OriginalTitle:    title,
			OriginalLanguage: "en",
			Status:           "",
			ExternalIds:      []byte(`{"tmdb":"` + tmdb + `"}`),
		})
		if err != nil {
			t.Fatalf("creating media item %s: %v", title, err)
		}
		if err := qtx.UpdateMediaItemSlug(ctx, sqlc.UpdateMediaItemSlugParams{ID: itemID, Slug: slug}); err != nil {
			t.Fatalf("setting slug for %s: %v", title, err)
		}
		if _, err := qtx.CreateTVSeries(ctx, sqlc.CreateTVSeriesParams{
			MediaItemID:      itemID,
			Status:           "returning",
			Genres:           []string{uniqueGenre, "Drama"},
			Rating:           pgtype.Numeric{Int: big.NewInt(rating), Valid: true},
			OriginalName:     title,
			OriginalLanguage: "en",
			NumberOfSeasons:  1,
			NumberOfEpisodes: 1,
			Popularity:       pgtype.Numeric{Int: big.NewInt(rating), Valid: true},
			SpokenLanguages:  []string{"en"},
			OriginCountry:    []string{"US"},
		}); err != nil {
			t.Fatalf("creating tv series %s: %v", title, err)
		}
		file, err := qtx.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
			LibraryID:   libID,
			Path:        slug + "/episode.mkv",
			Size:        1,
			ParseResult: []byte("{}"),
			Status:      sqlc.FileStatusMatched,
		})
		if err != nil {
			t.Fatalf("creating library file for %s: %v", title, err)
		}
		if err := qtx.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
			ID:           file.ID,
			Status:       sqlc.FileStatusMatched,
			MediaItemID:  pgtype.Int8{Int64: itemID, Valid: true},
			ErrorMessage: "",
		}); err != nil {
			t.Fatalf("attaching library file for %s: %v", title, err)
		}
		return itemID
	}

	tvID := createSeries("Ordinary Show", "ordinary-show", "900001", tvLib.ID, sqlc.MediaTypeTv, 98)
	animeID := createSeries("Anime Show", "anime-show", "900002", animeLib.ID, sqlc.MediaTypeAnime, 99)

	if err := qtx.CreateMediaRecommendation(ctx, sqlc.CreateMediaRecommendationParams{
		MediaItemID: tvID,
		ExternalIds: []byte(`{"tmdb":"900002"}`),
		Title:       "Anime Show",
		MediaType:   "tv",
		VoteAverage: pgtype.Numeric{Int: big.NewInt(9), Valid: true},
		ReleaseDate: "2024",
	}); err != nil {
		t.Fatalf("creating anime recommendation: %v", err)
	}
	if err := qtx.CreateMediaRecommendation(ctx, sqlc.CreateMediaRecommendationParams{
		MediaItemID: animeID,
		ExternalIds: []byte(`{"tmdb":"900001"}`),
		Title:       "Ordinary Show",
		MediaType:   "anime",
		VoteAverage: pgtype.Numeric{Int: big.NewInt(8), Valid: true},
		ReleaseDate: "2024",
	}); err != nil {
		t.Fatalf("creating tv recommendation: %v", err)
	}

	topRated, err := qtx.ListTopRatedTV(ctx, 10)
	if err != nil {
		t.Fatalf("listing top rated tv: %v", err)
	}
	topRatedTypes := make([]string, 0, len(topRated))
	for _, row := range topRated {
		topRatedTypes = append(topRatedTypes, row.MediaType)
	}
	if !mediaTypesContain(topRatedTypes, "tv", "anime") {
		t.Fatalf("expected top rated TV rail to include tv and anime, got %#v", topRated)
	}

	genreRows, err := qtx.ListTopTVInGenre(ctx, sqlc.ListTopTVInGenreParams{Genre: uniqueGenre, Lim: 10})
	if err != nil {
		t.Fatalf("listing tv genre rail: %v", err)
	}
	genreTypes := make([]string, 0, len(genreRows))
	for _, row := range genreRows {
		genreTypes = append(genreTypes, row.MediaType)
	}
	if !mediaTypesContain(genreTypes, "tv", "anime") {
		t.Fatalf("expected TV genre rail to include tv and anime, got %#v", genreRows)
	}

	recRows, err := qtx.ListLocalRecommendations(ctx, sqlc.ListLocalRecommendationsParams{
		ItemType: sqlc.MediaTypeTv,
		RecType:  "tv",
		UserID:   user.ID,
		Lim:      10,
	})
	if err != nil {
		t.Fatalf("listing local recommendations: %v", err)
	}
	recTypes := make([]string, 0, len(recRows))
	for _, row := range recRows {
		recTypes = append(recTypes, row.MediaType)
	}
	if !mediaTypesContain(recTypes, "tv", "anime") {
		t.Fatalf("expected recommended shows rail to include tv and anime, got %#v", recRows)
	}
}

func mediaTypesContain(types []string, want ...string) bool {
	seen := make(map[string]bool, len(types))
	for _, typ := range types {
		seen[typ] = true
	}
	for _, typ := range want {
		if !seen[typ] {
			return false
		}
	}
	return true
}
