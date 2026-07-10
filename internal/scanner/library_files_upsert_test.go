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

// Re-applies of unchanged files (force rescans, relocated-file scope
// re-pipelines, review re-identifies) flow through UpsertLibraryFile's
// conflict branch; probe artifacts must survive unless the bytes actually
// changed, or every such pass triggers a library-wide ffprobe sweep.
func TestUpsertLibraryFileKeepsProbeDataForUnchangedBytes(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scanner-upsert-probe-preserve-test",
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{"/media/movies"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	mtime := time.Date(2026, 7, 10, 4, 0, 0, 123456789, time.UTC) // ns residue on purpose
	upsert := func(size int64, at time.Time) sqlc.LibraryFile {
		row, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
			LibraryID:   lib.ID,
			Path:        "/media/movies/Probe (2020)/Probe.mkv",
			Size:        size,
			Mtime:       pgtype.Timestamptz{Time: at, Valid: true},
			ParseResult: []byte("{}"),
			Status:      sqlc.FileStatusPending,
		})
		require.NoError(t, err)
		return row
	}

	file := upsert(100, mtime)
	require.NoError(t, q.UpdateLibraryFileMediaInfo(ctx, sqlc.UpdateLibraryFileMediaInfoParams{
		ID:        file.ID,
		MediaInfo: []byte(`{"format":{"duration":"12.3"}}`),
	}))
	_, err = pool.Exec(ctx, `UPDATE library_files SET has_trickplay = true, segments_analyzed_at = now() WHERE id = $1`, file.ID)
	require.NoError(t, err)

	same := upsert(100, mtime)
	require.Equal(t, file.ID, same.ID)
	require.JSONEq(t, `{"format":{"duration":"12.3"}}`, string(same.MediaInfo),
		"unchanged bytes must keep probe data through a re-apply")
	require.True(t, same.HasTrickplay, "unchanged bytes keep trickplay")
	require.True(t, same.SegmentsAnalyzedAt.Valid, "unchanged bytes keep segments")

	changed := upsert(200, mtime)
	require.JSONEq(t, `{}`, string(changed.MediaInfo),
		"a size change must still clear stale probe data")
	require.False(t, changed.HasTrickplay, "byte change invalidates trickplay")
	require.False(t, changed.SegmentsAnalyzedAt.Valid, "byte change invalidates segments")

	require.NoError(t, q.UpdateLibraryFileMediaInfo(ctx, sqlc.UpdateLibraryFileMediaInfoParams{
		ID:        file.ID,
		MediaInfo: []byte(`{"format":{"duration":"12.3"}}`),
	}))
	touched := upsert(200, mtime.Add(3*time.Second))
	require.JSONEq(t, `{}`, string(touched.MediaInfo),
		"an mtime change must still clear stale probe data")
}
