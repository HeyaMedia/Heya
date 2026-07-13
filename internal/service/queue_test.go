package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

// queueFixture builds a minimal playable music album: library → media_item
// → artist → album → n tracks, each with a live library_file + track_file
// (the materializers only queue tracks whose playable-EXISTS passes).
type queueFixture struct {
	libraryID int64
	artistID  int64
	albumID   int64
	trackIDs  []int64
}

func setupQueueFixture(t *testing.T, pool *pgxpool.Pool, userID int64, tag string, n int) queueFixture {
	t.Helper()
	ctx := context.Background()
	var f queueFixture

	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO libraries (name, media_type, created_by) VALUES ($1, 'music', $2) RETURNING id`,
		"queue-test-"+tag, userID).Scan(&f.libraryID))
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, f.libraryID) })

	var itemID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO media_items (library_id, media_type, slug) VALUES ($1, 'music', $2) RETURNING id`,
		f.libraryID, "queue-artist-"+tag).Scan(&itemID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO artists (media_item_id, name) VALUES ($1, $2) RETURNING id`,
		itemID, "Queue Artist "+tag).Scan(&f.artistID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO albums (artist_id, title, slug, genres) VALUES ($1, $2, $3, '{Testwave}') RETURNING id`,
		f.artistID, "Queue Album "+tag, "queue-album-"+tag).Scan(&f.albumID))

	for i := 1; i <= n; i++ {
		var trackID, fileID int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO tracks (album_id, disc_number, track_number, title, duration)
			 VALUES ($1, 1, $2, $3, 180) RETURNING id`,
			f.albumID, i, fmt.Sprintf("Track %02d (%s)", i, tag)).Scan(&trackID))
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO library_files (library_id, path, media_item_id)
			 VALUES ($1, $2, $3) RETURNING id`,
			f.libraryID, fmt.Sprintf("/music/%s/%02d.flac", tag, i), itemID).Scan(&fileID))
		// Both file-link styles must count as playable: odd tracks get a
		// track_files row (multi-file model), even tracks only the legacy
		// tracks.library_file_id link (most of a pre-existing library).
		if i%2 == 1 {
			_, err := pool.Exec(ctx,
				`INSERT INTO track_files (track_id, library_file_id) VALUES ($1, $2)`, trackID, fileID)
			require.NoError(t, err)
		} else {
			_, err := pool.Exec(ctx,
				`UPDATE tracks SET library_file_id = $2 WHERE id = $1`, trackID, fileID)
			require.NoError(t, err)
		}
		f.trackIDs = append(f.trackIDs, trackID)
	}
	// library_files cascade via CleanupLibrary; the rest cascades from the
	// media item / album FKs when the library goes.
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM play_queues WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM media_items WHERE id = $1`, itemID)
	})
	return f
}

func TestQueueReplaceWindowAndStartTrack(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "replace", 12)
	ctx := context.Background()

	view, err := app.ReplaceQueue(ctx, userID, QueueSource{Kind: "album", ID: f.albumID}, 0, false, "local:test")
	require.NoError(t, err)
	require.EqualValues(t, 12, view.Total)
	require.True(t, view.Playing)
	require.Equal(t, "local:test", view.ActiveOutput)
	require.EqualValues(t, 0, view.CurrentIndex)
	require.Len(t, view.Items, 12)
	// Natural album order preserved.
	for i, it := range view.Items {
		require.Equal(t, f.trackIDs[i], it.TrackID, "window order at %d", i)
	}
	require.Equal(t, view.CurrentItemID, view.Items[0].ItemID)

	// Start mid-album without shuffle: pointer lands on the track, order intact.
	view, err = app.ReplaceQueue(ctx, userID, QueueSource{Kind: "album", ID: f.albumID}, f.trackIDs[4], false, "local:test")
	require.NoError(t, err)
	require.EqualValues(t, 4, view.CurrentIndex)

	// Shuffled start: the chosen track leads.
	view, err = app.ReplaceQueue(ctx, userID, QueueSource{Kind: "album", ID: f.albumID}, f.trackIDs[7], true, "local:test")
	require.NoError(t, err)
	require.EqualValues(t, 0, view.CurrentIndex)
	require.Equal(t, f.trackIDs[7], view.Items[0].TrackID)
	require.True(t, view.Shuffled)
}

func TestQueueAdvanceIdempotentAndRepeat(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "advance", 3)
	ctx := context.Background()

	view, err := app.ReplaceQueue(ctx, userID, QueueSource{Kind: "album", ID: f.albumID}, 0, false, "local:test")
	require.NoError(t, err)
	first := view.CurrentItemID

	// Normal advance.
	view, err = app.AdvanceQueue(ctx, userID, first, "ended")
	require.NoError(t, err)
	second := view.CurrentItemID
	require.NotEqual(t, first, second)
	require.EqualValues(t, 1, view.CurrentIndex)

	// Stale double-fire: advancing "from" the already-passed item is a no-op.
	view, err = app.AdvanceQueue(ctx, userID, first, "ended")
	require.NoError(t, err)
	require.Equal(t, second, view.CurrentItemID)

	// prev returns to the first track.
	view, err = app.AdvanceQueue(ctx, userID, second, "prev")
	require.NoError(t, err)
	require.Equal(t, first, view.CurrentItemID)

	// repeat one: ended stays put.
	require.NoError(t, app.SetQueueRepeat(ctx, userID, "one"))
	view, err = app.AdvanceQueue(ctx, userID, first, "ended")
	require.NoError(t, err)
	require.Equal(t, first, view.CurrentItemID)
	require.True(t, view.Playing)

	// repeat off, run to the end: pointer stays on the last, playing stops.
	require.NoError(t, app.SetQueueRepeat(ctx, userID, "off"))
	view, err = app.AdvanceQueue(ctx, userID, first, "skip")
	require.NoError(t, err)
	view, err = app.AdvanceQueue(ctx, userID, view.CurrentItemID, "skip")
	require.NoError(t, err)
	last := view.CurrentItemID
	view, err = app.AdvanceQueue(ctx, userID, last, "ended")
	require.NoError(t, err)
	require.Equal(t, last, view.CurrentItemID)
	require.False(t, view.Playing)

	// repeat all: wraps to the head.
	require.NoError(t, app.SetQueueRepeat(ctx, userID, "all"))
	view, err = app.AdvanceQueue(ctx, userID, last, "ended")
	require.NoError(t, err)
	require.Equal(t, first, view.CurrentItemID)
	require.True(t, view.Playing)
}

func TestQueueShuffleRestoresNaturalOrder(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "shuffle", 30)
	ctx := context.Background()

	view, err := app.ReplaceQueue(ctx, userID, QueueSource{Kind: "album", ID: f.albumID}, 0, false, "local:test")
	require.NoError(t, err)
	// Advance twice so there's a played head that must not move.
	view, err = app.AdvanceQueue(ctx, userID, view.CurrentItemID, "skip")
	require.NoError(t, err)
	view, err = app.AdvanceQueue(ctx, userID, view.CurrentItemID, "skip")
	require.NoError(t, err)
	current := view.CurrentItemID
	require.Equal(t, f.trackIDs[2], view.Items[2].TrackID)

	require.NoError(t, app.SetQueueShuffle(ctx, userID, true))
	view, err = app.GetQueue(ctx, userID, nil, 100)
	require.NoError(t, err)
	require.True(t, view.Shuffled)
	require.Equal(t, current, view.CurrentItemID)
	require.EqualValues(t, 2, view.CurrentIndex)
	// Played head untouched.
	require.Equal(t, f.trackIDs[0], view.Items[0].TrackID)
	require.Equal(t, f.trackIDs[1], view.Items[1].TrackID)

	require.NoError(t, app.SetQueueShuffle(ctx, userID, false))
	view, err = app.GetQueue(ctx, userID, nil, 100)
	require.NoError(t, err)
	require.False(t, view.Shuffled)
	// Full natural order restored via src_ord.
	for i, it := range view.Items {
		require.Equal(t, f.trackIDs[i], it.TrackID, "restored order at %d", i)
	}
}

func TestQueueEnqueueDedupeAndPlayNext(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "enqueue", 5)
	extra := setupQueueFixture(t, pool, userID, "enqueue-extra", 4)
	ctx := context.Background()

	_, err := app.ReplaceQueue(ctx, userID, QueueSource{Kind: "album", ID: f.albumID}, 0, false, "local:test")
	require.NoError(t, err)

	// Duplicate of an upcoming track is dropped; fresh ones land at the end.
	added, err := app.EnqueueTracks(ctx, userID, []int64{f.trackIDs[3], extra.trackIDs[0], extra.trackIDs[1]}, "end")
	require.NoError(t, err)
	require.EqualValues(t, 2, added)

	// Play-next slots directly after the current item.
	added, err = app.EnqueueTracks(ctx, userID, []int64{extra.trackIDs[2]}, "next")
	require.NoError(t, err)
	require.EqualValues(t, 1, added)

	view, err := app.GetQueue(ctx, userID, nil, 100)
	require.NoError(t, err)
	require.EqualValues(t, 8, view.Total)
	require.Equal(t, extra.trackIDs[2], view.Items[1].TrackID, "play-next must follow the current track")
	require.Equal(t, extra.trackIDs[0], view.Items[6].TrackID)
	require.Equal(t, extra.trackIDs[1], view.Items[7].TrackID)
}

func TestQueueMoveItem(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "move", 6)
	ctx := context.Background()

	view, err := app.ReplaceQueue(ctx, userID, QueueSource{Kind: "album", ID: f.albumID}, 0, false, "local:test")
	require.NoError(t, err)

	// Move the last item to the head of the upcoming slice (after current).
	lastItem := view.Items[5].ItemID
	require.NoError(t, app.MoveQueueItem(ctx, userID, lastItem, 0))
	view, err = app.GetQueue(ctx, userID, nil, 100)
	require.NoError(t, err)
	require.Equal(t, lastItem, view.Items[1].ItemID)

	// Move it after a specific item. It currently sits BEFORE that anchor
	// (index 1 vs 3), so extracting it shifts the anchor left one slot —
	// the moved item lands at the anchor's original index.
	require.NoError(t, app.MoveQueueItem(ctx, userID, lastItem, view.Items[3].ItemID))
	view, err = app.GetQueue(ctx, userID, nil, 100)
	require.NoError(t, err)
	require.Equal(t, lastItem, view.Items[3].ItemID)
}

func TestQueueClaimAndHeartbeat(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "claim", 2)
	ctx := context.Background()

	_, err := app.ReplaceQueue(ctx, userID, QueueSource{Kind: "album", ID: f.albumID}, 0, false, "local:tab-a")
	require.NoError(t, err)

	// The active output heartbeats fine.
	require.NoError(t, app.QueueHeartbeat(ctx, userID, "local:tab-a", 42, true))
	view, err := app.GetQueue(ctx, userID, nil, 10)
	require.NoError(t, err)
	require.InDelta(t, 42, view.PositionSeconds, 0.01)

	// A non-active output is rejected.
	err = app.QueueHeartbeat(ctx, userID, "local:tab-b", 50, true)
	require.True(t, errors.Is(err, ErrQueueNotActiveOutput))

	// Until it claims.
	require.NoError(t, app.ClaimQueueOutput(ctx, userID, "local:tab-b"))
	require.NoError(t, app.QueueHeartbeat(ctx, userID, "local:tab-b", 50, true))
}

func TestQueueHistoryPruning(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "prune", queueHistoryKeep+20)
	ctx := context.Background()

	view, err := app.ReplaceQueue(ctx, userID, QueueSource{Kind: "album", ID: f.albumID}, 0, false, "local:test")
	require.NoError(t, err)
	require.EqualValues(t, queueHistoryKeep+20, view.Total)

	// Jump deep into the queue (no pruning on jump)...
	viewDeep, err := app.GetQueue(ctx, userID, ptrInt64(int64((queueHistoryKeep+10)*queueOrdGap)), 10)
	require.NoError(t, err)
	require.NotEmpty(t, viewDeep.Items)
	deep := viewDeep.Items[0].ItemID
	_, err = app.JumpToQueueItem(ctx, userID, deep)
	require.NoError(t, err)

	// ...then one advance prunes history down to the keep window.
	view, err = app.AdvanceQueue(ctx, userID, deep, "ended")
	require.NoError(t, err)
	require.Less(t, view.Total, int64(queueHistoryKeep+20))
	require.EqualValues(t, queueHistoryKeep, view.CurrentIndex, "exactly queueHistoryKeep played items remain")
}

func ptrInt64(v int64) *int64 { return &v }
