package worker

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/require"
)

// TestListUnprobedProbeableFiles is the selection behind the scan re-probe pass:
// it must return files that are known (not pending) but never got media_info,
// and must exclude already-probed, still-pending, and soft-deleted files — so a
// probed-and-unchanged file is never needlessly re-probed.
func TestListUnprobedProbeableFiles(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		Username: "probetest", Email: "probetest@example.com", PasswordHash: "x", IsAdmin: true,
	})
	require.NoError(t, err)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "L", MediaType: sqlc.MediaTypeTv, Paths: []string{"/x"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    user.ID, Settings: []byte("{}"),
	})
	require.NoError(t, err)

	mk := func(path string, status sqlc.FileStatus, probed, deleted bool) int64 {
		f, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
			LibraryID: lib.ID, Path: path, ParseResult: []byte("{}"), Status: status,
		})
		require.NoError(t, err)
		if probed {
			require.NoError(t, q.UpdateLibraryFileMediaInfo(ctx, sqlc.UpdateLibraryFileMediaInfoParams{
				ID: f.ID, MediaInfo: []byte(`{"format":{"format_name":"matroska"}}`),
			}))
		}
		if deleted {
			_, err := tx.Exec(ctx, `UPDATE library_files SET deleted_at = now() WHERE id = $1`, f.ID)
			require.NoError(t, err)
		}
		return f.ID
	}

	want := mk("/x/A.mkv", sqlc.FileStatusMatched, false, false) // matched + unprobed → returned
	mk("/x/B.mkv", sqlc.FileStatusMatched, true, false)          // already probed → excluded
	mk("/x/C.mkv", sqlc.FileStatusPending, false, false)         // pending (ProcessFile handles it) → excluded
	mk("/x/D.mkv", sqlc.FileStatusMatched, false, true)          // soft-deleted → excluded

	got, err := q.ListUnprobedProbeableFiles(ctx, sqlc.ListUnprobedProbeableFilesParams{LibraryID: lib.ID, Limit: 100})
	require.NoError(t, err)
	require.Len(t, got, 1, "only the matched-but-unprobed file should be selected")
	require.Equal(t, want, got[0].ID)
}

// TestListRetryableUnmatchedFiles is the selection behind the re-match pass: it
// must return only files stranded by a TRANSIENT search error ("search error:
// ...") — not genuine "no results", not matched, not deleted.
func TestListRetryableUnmatchedFiles(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		Username: "rematchtest", Email: "rematchtest@example.com", PasswordHash: "x", IsAdmin: true,
	})
	require.NoError(t, err)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "L", MediaType: sqlc.MediaTypeMovie, Paths: []string{"/x"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    user.ID, Settings: []byte("{}"),
	})
	require.NoError(t, err)

	mk := func(path string, status sqlc.FileStatus, errMsg string) int64 {
		f, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
			LibraryID: lib.ID, Path: path, ParseResult: []byte("{}"), Status: sqlc.FileStatusPending,
		})
		require.NoError(t, err)
		require.NoError(t, q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
			ID: f.ID, Status: status, ErrorMessage: errMsg,
		}))
		return f.ID
	}

	want := mk("/x/A.mkv", sqlc.FileStatusUnmatched, "search error: dial tcp: timeout") // transient → returned
	mk("/x/B.mkv", sqlc.FileStatusUnmatched, "no provider results")                     // genuine no-match → excluded
	mk("/x/C.mkv", sqlc.FileStatusMatched, "")                                          // matched → excluded

	got, err := q.ListRetryableUnmatchedFiles(ctx, sqlc.ListRetryableUnmatchedFilesParams{LibraryID: lib.ID, Limit: 100})
	require.NoError(t, err)
	require.Len(t, got, 1, "only the transient-search-error file should be selected")
	require.Equal(t, want, got[0].ID)
}
