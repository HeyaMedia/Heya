package service

import (
	"context"
	"testing"

	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestJFFileHasSegments exercises the query backing MediaSourceInfo.HasSegments
// — jellyfin-web refuses to ever fetch /MediaSegments when this flag is
// false, so a wrong answer here silently disables skip-intro/outro for that
// client. Covers: a file with a stored segment, a file with none, and an id
// that doesn't exist at all.
func TestJFFileHasSegments(t *testing.T) {
	ctx := context.Background()
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)

	var libraryID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO libraries (name, media_type, created_by) VALUES ('jf-has-segments-test', 'tv', $1) RETURNING id`,
		userID,
	).Scan(&libraryID))
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, libraryID) })

	var withSegment, withoutSegment int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO library_files (library_id, path) VALUES ($1, '/test/jf-has-segments/with.mkv') RETURNING id`,
		libraryID,
	).Scan(&withSegment))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO library_files (library_id, path) VALUES ($1, '/test/jf-has-segments/without.mkv') RETURNING id`,
		libraryID,
	).Scan(&withoutSegment))

	_, err := pool.Exec(ctx,
		`INSERT INTO media_segments (library_file_id, segment_type, start_ms, end_ms, source) VALUES ($1, 'intro', 0, 90000, 'manual')`,
		withSegment,
	)
	require.NoError(t, err)

	if !app.JFFileHasSegments(ctx, withSegment) {
		t.Error("JFFileHasSegments = false for a file with a stored segment, want true")
	}
	if app.JFFileHasSegments(ctx, withoutSegment) {
		t.Error("JFFileHasSegments = true for a file with no segments, want false")
	}
	if app.JFFileHasSegments(ctx, -1) {
		t.Error("JFFileHasSegments = true for a nonexistent file id, want false")
	}
}
