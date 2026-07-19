package scanner

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestObservePendingAnalysisFilesMakesFirstScanMusicAddressable(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	lib := createObservationTestLibrary(t, ctx, q, pool, "scanner-observe-first-scan")
	mtime := time.Date(2026, 7, 19, 8, 9, 10, 123456789, time.UTC)
	path := "/media/music/Ado/Kyougen/01 - Usseewa.flac"

	observed, err := ObservePendingAnalysisFiles(ctx, pool, lib, Result{Inventory: Inventory{Roots: []InventoryRoot{{
		Root: "/media/music",
		Files: []InventoryFile{
			{Path: path, RelPath: "Ado/Kyougen/01 - Usseewa.flac", Class: ClassPrimaryMedia, Size: 1234, MTime: mtime},
			{Path: "/media/music/Ado/Kyougen/cover.jpg", RelPath: "Ado/Kyougen/cover.jpg", Class: ClassArtwork, Size: 42, MTime: mtime},
		},
	}}}})
	require.NoError(t, err)
	require.Equal(t, 1, observed)

	file, err := q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{LibraryID: lib.ID, Path: path})
	require.NoError(t, err)
	require.Equal(t, sqlc.FileStatusPending, file.Status)
	require.Equal(t, int64(1234), file.Size)
	require.True(t, file.Mtime.Time.Truncate(time.Microsecond).Equal(mtime.Truncate(time.Microsecond)))
	_, err = q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{LibraryID: lib.ID, Path: "/media/music/Ado/Kyougen/cover.jpg"})
	require.Error(t, err, "non-media sidecars are replay inputs, not library-file rows")
}

func TestObservePendingLibraryFileInvalidatesChangedDerivedState(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	lib := createObservationTestLibrary(t, ctx, q, pool, "scanner-observe-changed-source")
	path := "/media/music/Ado/Kyougen/01 - Usseewa.flac"
	mtime := time.Date(2026, 7, 19, 8, 9, 10, 0, time.UTC)

	file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID: lib.ID, Path: path, Size: 100,
		Mtime: pgtype.Timestamptz{Time: mtime, Valid: true}, ParseResult: []byte("{}"), Status: sqlc.FileStatusMatched,
	})
	require.NoError(t, err)
	require.NoError(t, q.UpdateLibraryFileMediaInfo(ctx, sqlc.UpdateLibraryFileMediaInfoParams{
		ID: file.ID, MediaInfo: []byte(`{"streams":[{"codec_type":"audio","codec_name":"flac"}]}`),
	}))
	require.NoError(t, q.UpdateLibraryFileContentHash(ctx, sqlc.UpdateLibraryFileContentHashParams{ID: file.ID, ContentHash: "old-hash"}))

	same, err := q.ObservePendingLibraryFile(ctx, sqlc.ObservePendingLibraryFileParams{
		LibraryID: lib.ID, Path: path, Size: 100,
		Mtime: pgtype.Timestamptz{Time: mtime, Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, sqlc.FileStatusMatched, same.Status, "unchanged applied bytes remain terminal")
	require.NotEmpty(t, same.AudioFormats)
	require.Equal(t, "old-hash", same.ContentHash)

	changed, err := q.ObservePendingLibraryFile(ctx, sqlc.ObservePendingLibraryFileParams{
		LibraryID: lib.ID, Path: path, Size: 200,
		Mtime: pgtype.Timestamptz{Time: mtime.Add(time.Second), Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, sqlc.FileStatusPending, changed.Status)
	require.JSONEq(t, `{}`, string(changed.MediaInfo))
	require.Empty(t, changed.AudioFormats)
	require.Empty(t, changed.VideoFormats)
	require.Empty(t, changed.ContentHash)

	unchanged, err := q.ObservePendingLibraryFile(ctx, sqlc.ObservePendingLibraryFileParams{
		LibraryID: lib.ID, Path: path, Size: 200,
		Mtime: pgtype.Timestamptz{Time: mtime.Add(time.Second), Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, sqlc.FileStatusPending, unchanged.Status, "an interrupted observation remains retryable")
}

func createObservationTestLibrary(t *testing.T, ctx context.Context, q *sqlc.Queries, pool *pgxpool.Pool, name string) sqlc.Library {
	t.Helper()
	// Keep creation inline with the scanner DB tests so each test owns cleanup.
	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: name, MediaType: sqlc.MediaTypeMusic, Paths: []string{"/media/music"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
	return lib
}
