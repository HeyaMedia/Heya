package service

import (
	"context"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestQueryDetectionItems exercises the local-detection task modal's
// counts and per-row statuses. The load-bearing rule: a file fully
// covered by community/manual rows counts COMPLETE even though the pump
// never stamps segments_detected_at on it — the pump deliberately skips
// covered files (gap-filler policy), so counting them pending would show
// tens of thousands of eternally-pending rows in the task UI. Needs
// detection: TV → missing intro OR credits; movie → missing credits.
//
// The query is global (no library scoping), so on the shared dev DB the
// assertions are deltas against a baseline snapshot plus per-row status
// checks on this test's own files.
func TestQueryDetectionItems(t *testing.T) {
	ctx := context.Background()
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)

	mkLib := func(name, mediaType string) int64 {
		var id int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO libraries (name, media_type, created_by) VALUES ($1, $2, $3) RETURNING id`,
			name, mediaType, userID,
		).Scan(&id))
		return id
	}
	tvLib := mkLib("detection-items-tv", "tv")
	movLib := mkLib("detection-items-movie", "movie")
	t.Cleanup(func() {
		cctx := context.Background()
		libs := []int64{tvLib, movLib}
		pool.Exec(cctx, `DELETE FROM library_files WHERE library_id = ANY($1)`, libs)
		pool.Exec(cctx, `DELETE FROM media_items WHERE library_id = ANY($1)`, libs)
		pool.Exec(cctx, `DELETE FROM libraries WHERE id = ANY($1)`, libs)
	})

	mkItem := func(libID int64, mediaType sqlc.MediaType, title string) int64 {
		item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
			LibraryID: libID, MediaType: mediaType, Title: title, SortTitle: title,
			ExternalIds: []byte("{}"),
		})
		require.NoError(t, err)
		return item.ID
	}
	tvItem := mkItem(tvLib, sqlc.MediaTypeTv, "Detection Items Show")
	movItem := mkItem(movLib, sqlc.MediaTypeMovie, "Detection Items Film")

	mkFile := func(libID, itemID int64, path string, detected bool) int64 {
		var id int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO library_files (library_id, path, media_item_id, media_info, segments_analyzed_at, segments_detected_at)
			 VALUES ($1, $2, $3, '{"duration": 1200}', now(), CASE WHEN $4 THEN now() END)
			 RETURNING id`,
			libID, path, itemID, detected,
		).Scan(&id))
		return id
	}
	addSeg := func(fileID int64, segType string) {
		require.NoError(t, q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
			LibraryFileID: fileID, SegmentType: segType, StartMs: 1_000, EndMs: 31_000, Source: "community:theintrodb",
		}))
	}

	// Baseline BEFORE inserting this test's files (shared DB — assert
	// deltas, not absolutes).
	baseline, err := app.QueryDetectionItems(ctx, "", 1, 0)
	require.NoError(t, err)

	// TV: covered (intro+credits) → complete without any detection stamp.
	tvCovered := mkFile(tvLib, tvItem, "/detection-items/tv-covered.mkv", false)
	addSeg(tvCovered, "intro")
	addSeg(tvCovered, "credits")
	// TV: partial coverage (intro only) → still a credits gap → pending.
	tvPartial := mkFile(tvLib, tvItem, "/detection-items/tv-partial.mkv", false)
	addSeg(tvPartial, "intro")
	// TV: no rows, not detected → pending.
	tvUncovered := mkFile(tvLib, tvItem, "/detection-items/tv-uncovered.mkv", false)
	// TV: no rows but detection ran (stamped, found nothing) → complete.
	tvDetected := mkFile(tvLib, tvItem, "/detection-items/tv-detected.mkv", true)
	// Movie: credits row present → complete (movies need no intro).
	movCovered := mkFile(movLib, movItem, "/detection-items/mov-covered.mkv", false)
	addSeg(movCovered, "credits")
	// Movie: no credits row, not detected → pending.
	movUncovered := mkFile(movLib, movItem, "/detection-items/mov-uncovered.mkv", false)

	after, err := app.QueryDetectionItems(ctx, "", 1, 0)
	require.NoError(t, err)
	require.Equal(t, baseline.Total+6, after.Total, "all six analyzed files enter the total")
	require.Equal(t, baseline.Complete+3, after.Complete, "covered TV + detected TV + covered movie count complete")
	require.Equal(t, baseline.Pending+3, after.Pending, "partial TV + uncovered TV + uncovered movie count pending")

	// The listing's per-row status and the status filters must agree with
	// the counts' definition.
	statusOf := func(status string) map[int64]TaskItem {
		res, err := app.QueryDetectionItems(ctx, status, 1_000_000, 0)
		require.NoError(t, err)
		out := map[int64]TaskItem{}
		for _, it := range res.Items {
			out[it.ID] = it
		}
		return out
	}

	all := statusOf("")
	require.Equal(t, "complete", all[tvCovered].Status)
	require.Equal(t, "covered by community/manual markers", all[tvCovered].Detail)
	require.Equal(t, "pending", all[tvPartial].Status)
	require.Equal(t, "pending", all[tvUncovered].Status)
	require.Equal(t, "complete", all[tvDetected].Status)
	require.Equal(t, "no local markers found", all[tvDetected].Detail)
	require.Equal(t, "complete", all[movCovered].Status)
	require.Equal(t, "pending", all[movUncovered].Status)

	pending := statusOf("pending")
	for _, id := range []int64{tvPartial, tvUncovered, movUncovered} {
		_, ok := pending[id]
		require.True(t, ok, "file %d must appear under the pending filter", id)
	}
	for _, id := range []int64{tvCovered, tvDetected, movCovered} {
		_, ok := pending[id]
		require.False(t, ok, "file %d must not appear under the pending filter", id)
	}

	complete := statusOf("complete")
	for _, id := range []int64{tvCovered, tvDetected, movCovered} {
		_, ok := complete[id]
		require.True(t, ok, "file %d must appear under the complete filter", id)
	}
	for _, id := range []int64{tvPartial, tvUncovered, movUncovered} {
		_, ok := complete[id]
		require.False(t, ok, "file %d must not appear under the complete filter", id)
	}
}
