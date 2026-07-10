package scanner

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestParkUnmatchedFilesMarksOnlyUnclaimedTrackedFiles(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scanner-park-unmatched-test",
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{"/media/movies"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	mtime := time.Date(2026, 7, 10, 4, 0, 0, 123456789, time.UTC)
	result := Result{
		Inventory: Inventory{Roots: []InventoryRoot{{
			Root: "/media/movies",
			Files: []InventoryFile{
				{Path: "/media/movies/Accepted (2006)/Accepted.mkv", RelPath: "Accepted (2006)/Accepted.mkv", Class: ClassPrimaryMedia, Size: 100, MTime: mtime},
				{Path: "/media/movies/Mystery (1999)/Mystery.mkv", RelPath: "Mystery (1999)/Mystery.mkv", Class: ClassPrimaryMedia, Size: 200, MTime: mtime},
				{Path: "/media/movies/Mystery (1999)/poster.jpg", RelPath: "Mystery (1999)/poster.jpg", Class: ClassArtwork, Size: 5, MTime: mtime},
			},
		}}},
		MovieMatches: []MovieMatch{
			{Key: "tmdb:1", Files: []string{"Accepted (2006)/Accepted.mkv"}},
			{Key: "mystery|1999", Files: []string{"Mystery (1999)/Mystery.mkv"}},
		},
		MovieSearch: []MovieSearchMatch{
			{Key: "tmdb:1", Accepted: true, ProviderID: "1"},
			{Key: "mystery|1999", Accepted: false},
		},
	}

	parked, err := ParkUnmatchedFiles(ctx, pool, lib, result)
	require.NoError(t, err)
	require.Equal(t, 1, parked, "only the unaccepted identity's file parks")

	row, err := q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{
		LibraryID: lib.ID,
		Path:      "/media/movies/Mystery (1999)/Mystery.mkv",
	})
	require.NoError(t, err)
	require.Equal(t, sqlc.FileStatusUnmatched, row.Status)
	require.Equal(t, int64(200), row.Size)
	require.True(t, row.Mtime.Valid)
	require.True(t, row.Mtime.Time.Truncate(time.Microsecond).Equal(mtime.Truncate(time.Microsecond)),
		"parked mtime must round-trip at µs precision")

	_, err = q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{
		LibraryID: lib.ID,
		Path:      "/media/movies/Accepted (2006)/Accepted.mkv",
	})
	require.Error(t, err, "accepted identity's file must be left to the apply phase")
}

func TestParkUnmatchedFilesPreservesMatchedRows(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scanner-park-preserve-test",
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{"/media/movies"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	oldMtime := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	existing, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID:   lib.ID,
		Path:        "/media/movies/Flipped (2010)/Flipped.mkv",
		Size:        100,
		Mtime:       pgtype.Timestamptz{Time: oldMtime, Valid: true},
		ParseResult: []byte("{}"),
		Status:      sqlc.FileStatusMatched,
	})
	require.NoError(t, err)

	newMtime := oldMtime.Add(48 * time.Hour)
	result := Result{
		Inventory: Inventory{Roots: []InventoryRoot{{
			Root: "/media/movies",
			Files: []InventoryFile{
				{Path: "/media/movies/Flipped (2010)/Flipped.mkv", RelPath: "Flipped (2010)/Flipped.mkv", Class: ClassPrimaryMedia, Size: 150, MTime: newMtime},
			},
		}}},
		MovieMatches: []MovieMatch{{Key: "flipped|2010", Files: []string{"Flipped (2010)/Flipped.mkv"}}},
		MovieSearch:  []MovieSearchMatch{{Key: "flipped|2010", Accepted: false}},
	}

	parked, err := ParkUnmatchedFiles(ctx, pool, lib, result)
	require.NoError(t, err)
	require.Equal(t, 1, parked)

	row, err := q.GetLibraryFileByID(ctx, existing.ID)
	require.NoError(t, err)
	require.Equal(t, sqlc.FileStatusMatched, row.Status, "a previously matched row keeps its status")
	require.Equal(t, int64(150), row.Size, "seen-marker still refreshes to current bytes")
	require.True(t, row.Mtime.Time.Equal(newMtime), "seen-marker still refreshes to current mtime")
}

func TestMatchThresholdForLibrary(t *testing.T) {
	lib := sqlc.Library{MediaType: sqlc.MediaTypeMovie, Settings: []byte(`{}`)}
	require.InDelta(t, 0.85, MatchThresholdForLibrary(lib), 1e-9, "absent setting uses the built-in default")

	lib.MediaType = sqlc.MediaTypeBook
	require.InDelta(t, 0.70, MatchThresholdForLibrary(lib), 1e-9, "books default lower")

	lib.Settings = []byte(`{"match_threshold":0.92}`)
	require.InDelta(t, 0.92, MatchThresholdForLibrary(lib), 1e-9)

	lib.Settings = []byte(`{"match_threshold":0.05}`)
	require.InDelta(t, 0.3, MatchThresholdForLibrary(lib), 1e-9, "clamped low")

	lib.Settings = []byte(`{"match_threshold":1.5}`)
	require.InDelta(t, 0.99, MatchThresholdForLibrary(lib), 1e-9, "clamped high")
}

func TestParkUnappliedFilesParksSkippedIdentities(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scanner-park-unapplied-test",
		MediaType:    sqlc.MediaTypeBook,
		Paths:        []string{"/media/books"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	mtime := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	result := Result{
		Inventory: Inventory{Roots: []InventoryRoot{{
			Root: "/media/books",
			Files: []InventoryFile{
				{Path: "/media/books/Frank Herbert/Dune (1965)/Dune.epub", RelPath: "Frank Herbert/Dune (1965)/Dune.epub", Class: ClassPrimaryMedia, Size: 33, MTime: mtime},
				{Path: "/media/books/Frank Herbert/Dune Messiah (1969)/Messiah.pdf", RelPath: "Frank Herbert/Dune Messiah (1969)/Messiah.pdf", Class: ClassPrimaryMedia, Size: 44, MTime: mtime},
			},
		}}},
		BookPlans: []BookPlan{
			{Key: "book:frank herbert|dune|1965", Files: []string{"Frank Herbert/Dune (1965)/Dune.epub"}},
			{Key: "book:frank herbert|dune messiah|1969", Files: []string{"Frank Herbert/Dune Messiah (1969)/Messiah.pdf"}},
		},
		BookApply: []BookApplyResult{
			{Key: "book:frank herbert|dune|1965", Action: "skipped", Reason: "metadata_mismatch"},
			{Key: "book:frank herbert|dune messiah|1969", Action: "create"},
		},
	}

	parked, err := ParkUnappliedFiles(ctx, pool, lib, result)
	require.NoError(t, err)
	require.Equal(t, 1, parked, "only the refused identity's file parks")

	row, err := q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{
		LibraryID: lib.ID,
		Path:      "/media/books/Frank Herbert/Dune (1965)/Dune.epub",
	})
	require.NoError(t, err)
	require.Equal(t, sqlc.FileStatusUnmatched, row.Status)
	require.Equal(t, int64(33), row.Size)

	_, err = q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{
		LibraryID: lib.ID,
		Path:      "/media/books/Frank Herbert/Dune Messiah (1969)/Messiah.pdf",
	})
	require.Error(t, err, "successfully applied identities are left to the apply writes")
}
