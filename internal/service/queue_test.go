package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/pgvector/pgvector-go"
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
		_, err := pool.Exec(ctx,
			`INSERT INTO track_files (track_id, library_file_id) VALUES ($1, $2)`, trackID, fileID)
		require.NoError(t, err)
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

	view, err := app.ReplaceQueue(ctx, userID, "test", QueueSource{Kind: "album", ID: f.albumID}, 0, false, "local:test")
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
	view, err = app.ReplaceQueue(ctx, userID, "test", QueueSource{Kind: "album", ID: f.albumID}, f.trackIDs[4], false, "local:test")
	require.NoError(t, err)
	require.EqualValues(t, 4, view.CurrentIndex)

	// Shuffled start: the chosen track leads.
	view, err = app.ReplaceQueue(ctx, userID, "test", QueueSource{Kind: "album", ID: f.albumID}, f.trackIDs[7], true, "local:test")
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

	view, err := app.ReplaceQueue(ctx, userID, "test", QueueSource{Kind: "album", ID: f.albumID}, 0, false, "local:test")
	require.NoError(t, err)
	first := view.CurrentItemID

	// Normal advance.
	view, err = app.AdvanceQueue(ctx, userID, "test", first, "ended")
	require.NoError(t, err)
	second := view.CurrentItemID
	require.NotEqual(t, first, second)
	require.EqualValues(t, 1, view.CurrentIndex)

	// Stale double-fire: advancing "from" the already-passed item is a no-op.
	view, err = app.AdvanceQueue(ctx, userID, "test", first, "ended")
	require.NoError(t, err)
	require.Equal(t, second, view.CurrentItemID)

	// prev returns to the first track.
	view, err = app.AdvanceQueue(ctx, userID, "test", second, "prev")
	require.NoError(t, err)
	require.Equal(t, first, view.CurrentItemID)

	// repeat one: ended stays put.
	require.NoError(t, app.SetQueueRepeat(ctx, userID, "test", "one"))
	view, err = app.AdvanceQueue(ctx, userID, "test", first, "ended")
	require.NoError(t, err)
	require.Equal(t, first, view.CurrentItemID)
	require.True(t, view.Playing)

	// repeat off, run to the end: pointer stays on the last, playing stops.
	require.NoError(t, app.SetQueueRepeat(ctx, userID, "test", "off"))
	view, err = app.AdvanceQueue(ctx, userID, "test", first, "skip")
	require.NoError(t, err)
	view, err = app.AdvanceQueue(ctx, userID, "test", view.CurrentItemID, "skip")
	require.NoError(t, err)
	last := view.CurrentItemID
	view, err = app.AdvanceQueue(ctx, userID, "test", last, "ended")
	require.NoError(t, err)
	require.Equal(t, last, view.CurrentItemID)
	require.False(t, view.Playing)

	// repeat all: wraps to the head.
	require.NoError(t, app.SetQueueRepeat(ctx, userID, "test", "all"))
	view, err = app.AdvanceQueue(ctx, userID, "test", last, "ended")
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

	view, err := app.ReplaceQueue(ctx, userID, "test", QueueSource{Kind: "album", ID: f.albumID}, 0, false, "local:test")
	require.NoError(t, err)
	// Advance twice so there's a played head that must not move.
	view, err = app.AdvanceQueue(ctx, userID, "test", view.CurrentItemID, "skip")
	require.NoError(t, err)
	view, err = app.AdvanceQueue(ctx, userID, "test", view.CurrentItemID, "skip")
	require.NoError(t, err)
	current := view.CurrentItemID
	require.Equal(t, f.trackIDs[2], view.Items[2].TrackID)

	require.NoError(t, app.SetQueueShuffle(ctx, userID, "test", true))
	view, err = app.GetQueue(ctx, userID, "test", nil, 100)
	require.NoError(t, err)
	require.True(t, view.Shuffled)
	require.Equal(t, current, view.CurrentItemID)
	require.EqualValues(t, 2, view.CurrentIndex)
	// Played head untouched.
	require.Equal(t, f.trackIDs[0], view.Items[0].TrackID)
	require.Equal(t, f.trackIDs[1], view.Items[1].TrackID)

	require.NoError(t, app.SetQueueShuffle(ctx, userID, "test", false))
	view, err = app.GetQueue(ctx, userID, "test", nil, 100)
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

	_, err := app.ReplaceQueue(ctx, userID, "test", QueueSource{Kind: "album", ID: f.albumID}, 0, false, "local:test")
	require.NoError(t, err)

	// Duplicate of an upcoming track is dropped; fresh ones land at the end.
	added, err := app.EnqueueTracks(ctx, userID, "test", []int64{f.trackIDs[3], extra.trackIDs[0], extra.trackIDs[1]}, "end")
	require.NoError(t, err)
	require.EqualValues(t, 2, added)

	// Play-next slots directly after the current item.
	added, err = app.EnqueueTracks(ctx, userID, "test", []int64{extra.trackIDs[2]}, "next")
	require.NoError(t, err)
	require.EqualValues(t, 1, added)

	view, err := app.GetQueue(ctx, userID, "test", nil, 100)
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

	view, err := app.ReplaceQueue(ctx, userID, "test", QueueSource{Kind: "album", ID: f.albumID}, 0, false, "local:test")
	require.NoError(t, err)

	// Move the last item to the head of the upcoming slice (after current).
	lastItem := view.Items[5].ItemID
	require.NoError(t, app.MoveQueueItem(ctx, userID, "test", lastItem, 0))
	view, err = app.GetQueue(ctx, userID, "test", nil, 100)
	require.NoError(t, err)
	require.Equal(t, lastItem, view.Items[1].ItemID)

	// Move it after a specific item. It currently sits BEFORE that anchor
	// (index 1 vs 3), so extracting it shifts the anchor left one slot —
	// the moved item lands at the anchor's original index.
	require.NoError(t, app.MoveQueueItem(ctx, userID, "test", lastItem, view.Items[3].ItemID))
	view, err = app.GetQueue(ctx, userID, "test", nil, 100)
	require.NoError(t, err)
	require.Equal(t, lastItem, view.Items[3].ItemID)
}

func TestQueueClaimAndHeartbeat(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "claim", 2)
	ctx := context.Background()

	_, err := app.ReplaceQueue(ctx, userID, "test", QueueSource{Kind: "album", ID: f.albumID}, 0, false, "local:tab-a")
	require.NoError(t, err)

	// The active output heartbeats fine.
	require.NoError(t, app.QueueHeartbeat(ctx, userID, "test", "local:tab-a", 42, true))
	view, err := app.GetQueue(ctx, userID, "test", nil, 10)
	require.NoError(t, err)
	require.InDelta(t, 42, view.PositionSeconds, 0.01)

	// A non-active output is rejected.
	err = app.QueueHeartbeat(ctx, userID, "test", "local:tab-b", 50, true)
	require.True(t, errors.Is(err, ErrQueueNotActiveOutput))

	// Until it claims.
	require.NoError(t, app.ClaimQueueOutput(ctx, userID, "test", "local:tab-b"))
	require.NoError(t, app.QueueHeartbeat(ctx, userID, "test", "local:tab-b", 50, true))
}

func TestQueueHistoryPruning(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "prune", queueHistoryKeep+20)
	ctx := context.Background()

	view, err := app.ReplaceQueue(ctx, userID, "test", QueueSource{Kind: "album", ID: f.albumID}, 0, false, "local:test")
	require.NoError(t, err)
	require.EqualValues(t, queueHistoryKeep+20, view.Total)

	// Jump deep into the queue (no pruning on jump)...
	viewDeep, err := app.GetQueue(ctx, userID, "test", ptrInt64(int64((queueHistoryKeep+10)*queueOrdGap)), 10)
	require.NoError(t, err)
	require.NotEmpty(t, viewDeep.Items)
	deep := viewDeep.Items[0].ItemID
	_, err = app.JumpToQueueItem(ctx, userID, "test", deep)
	require.NoError(t, err)

	// ...then one advance prunes history down to the keep window.
	view, err = app.AdvanceQueue(ctx, userID, "test", deep, "ended")
	require.NoError(t, err)
	require.Less(t, view.Total, int64(queueHistoryKeep+20))
	require.EqualValues(t, queueHistoryKeep, view.CurrentIndex, "exactly queueHistoryKeep played items remain")
}

func TestQueuesAreIsolatedPerDevice(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "devices", 3)
	ctx := context.Background()
	_, err := app.ReplaceQueue(ctx, userID, "client:desktop", QueueSource{Kind: "tracks", TrackIDs: []int64{f.trackIDs[0]}}, 0, false, "client:desktop")
	require.NoError(t, err)
	_, err = app.ReplaceQueue(ctx, userID, "client:phone", QueueSource{Kind: "tracks", TrackIDs: []int64{f.trackIDs[1], f.trackIDs[2]}}, 0, false, "client:phone")
	require.NoError(t, err)
	desktop, err := app.GetQueue(ctx, userID, "client:desktop", nil, 10)
	require.NoError(t, err)
	phone, err := app.GetQueue(ctx, userID, "client:phone", nil, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), desktop.Total)
	require.Equal(t, int64(2), phone.Total)
	require.Equal(t, f.trackIDs[0], desktop.Items[0].TrackID)
	require.Equal(t, f.trackIDs[1], phone.Items[0].TrackID)
}

func TestQueueDJOwnsAndCleansOnlyGeneratedTracks(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "dj-encore", 5)
	ctx := context.Background()

	// Start with a deliberately short user queue while leaving more tracks by
	// the same artist available for Encore to discover.
	view, err := app.ReplaceQueue(ctx, userID, "dj-test", QueueSource{
		Kind: "tracks", TrackIDs: []int64{f.trackIDs[0]},
	}, 0, false, "local:dj-test")
	require.NoError(t, err)
	require.EqualValues(t, 1, view.Total)

	view, err = app.SetQueueDJ(ctx, userID, "dj-test", DJModeEncore)
	require.NoError(t, err)
	require.Equal(t, DJModeEncore, view.DJMode)
	require.EqualValues(t, 3, view.Total)
	require.False(t, view.Items[0].DJGenerated)
	require.True(t, view.Items[1].DJGenerated)
	require.True(t, view.Items[2].DJGenerated)
	require.Equal(t, DJModeEncore, view.Items[1].DJMode)
	require.Equal(t, DJModeEncore, view.Items[2].DJMode)

	// Turning it off removes the future generated items but not the user's
	// current item. The queue remains playable at the same pointer.
	current := view.CurrentItemID
	view, err = app.SetQueueDJ(ctx, userID, "dj-test", DJModeOff)
	require.NoError(t, err)
	require.Equal(t, DJModeOff, view.DJMode)
	require.Equal(t, current, view.CurrentItemID)
	require.EqualValues(t, 1, view.Total)
	require.Equal(t, f.trackIDs[0], view.Items[0].TrackID)
}

func TestDJQueueSchedulingPolicies(t *testing.T) {
	const session = int64(42)
	userCurrent := sqlc.PlayQueueItem{ID: 1, TrackID: 101}
	userNext := sqlc.PlayQueueItem{ID: 9, TrackID: 109}
	generated := sqlc.PlayQueueItem{ID: 2, TrackID: 102, DjSession: session}
	generatedNext := sqlc.PlayQueueItem{ID: 3, TrackID: 103, DjSession: session}

	tests := []struct {
		name        string
		mode        string
		current     sqlc.PlayQueueItem
		next        []sqlc.PlayQueueItem
		nextUser    *sqlc.PlayQueueItem
		process     bool
		need        int
		targetTrack int64
	}{
		{"echo takes over past a user track", DJModeEcho, generated, []sqlc.PlayQueueItem{userNext}, &userNext, true, 2, 0},
		{"echo refills its rolling runway", DJModeEcho, generated, []sqlc.PlayQueueItem{generatedNext}, nil, true, 1, 0},
		{"flow adds two after a user track", DJModeFlow, userCurrent, []sqlc.PlayQueueItem{userNext}, &userNext, true, 2, 0},
		{"flow yields after its pair", DJModeFlow, generated, []sqlc.PlayQueueItem{generatedNext, userNext}, &userNext, false, 0, 0},
		{"flow extends its own tail", DJModeFlow, generated, nil, nil, true, 2, 0},
		{"flow refills its rolling runway", DJModeFlow, generated, []sqlc.PlayQueueItem{generatedNext}, nil, true, 1, 0},
		{"voyage targets the next user track", DJModeVoyage, userCurrent, []sqlc.PlayQueueItem{userNext}, &userNext, true, 3, userNext.TrackID},
		{"voyage waits for its path", DJModeVoyage, generated, []sqlc.PlayQueueItem{generatedNext, userNext}, &userNext, false, 0, 0},
		{"voyage heads toward chill at the tail", DJModeVoyage, generated, nil, nil, true, 3, 0},
		{"encore adds one after a user track", DJModeEncore, userCurrent, []sqlc.PlayQueueItem{userNext}, &userNext, true, 1, 0},
		{"encore yields to the listener queue", DJModeEncore, generated, []sqlc.PlayQueueItem{userNext}, &userNext, false, 0, 0},
		{"encore keeps two ready when the queue ends", DJModeEncore, generated, nil, nil, true, 2, 0},
		{"encore refills its empty-queue runway", DJModeEncore, generated, []sqlc.PlayQueueItem{generatedNext}, nil, true, 1, 0},
		{"spotlight keeps control", DJModeSpotlight, generated, []sqlc.PlayQueueItem{userNext}, &userNext, true, 2, 0},
		{"spotlight refills its rolling runway", DJModeSpotlight, generated, []sqlc.PlayQueueItem{generatedNext}, nil, true, 1, 0},
		{"timewarp keeps control", DJModeTimewarp, generated, []sqlc.PlayQueueItem{userNext}, &userNext, true, 2, 0},
		{"timewarp refills its rolling runway", DJModeTimewarp, generated, []sqlc.PlayQueueItem{generatedNext}, nil, true, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := planDJQueue(tt.mode, session, tt.current, tt.next, tt.nextUser)
			require.Equal(t, tt.process, plan.process)
			require.Equal(t, tt.need, plan.need)
			require.Equal(t, tt.targetTrack, plan.targetTrack)
		})
	}
}

func TestQueueEchoChainsFromEachGeneratedTrackWithoutRepeating(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	seed := setupQueueFixture(t, pool, userID, "dj-echo-seed", 1)
	neighborA := setupQueueFixture(t, pool, userID, "dj-echo-a", 1)
	neighborB := setupQueueFixture(t, pool, userID, "dj-echo-b", 1)
	neighborC := setupQueueFixture(t, pool, userID, "dj-echo-c", 1)
	neighborD := setupQueueFixture(t, pool, userID, "dj-echo-d", 1)
	ctx := context.Background()

	// Five distinct artists on a gently curving line in embedding space.
	// Each generated track should therefore seed the next nearest unplayed
	// artist, while the queue-wide exclusion set prevents walking backwards.
	tracks := []struct {
		id int64
		x  float32
		y  float32
	}{
		{seed.trackIDs[0], 1, 0},
		{neighborA.trackIDs[0], 0.995, 0.10},
		{neighborB.trackIDs[0], 0.980, 0.20},
		{neighborC.trackIDs[0], 0.955, 0.30},
		{neighborD.trackIDs[0], 0.920, 0.40},
	}
	for _, track := range tracks {
		vector := make([]float32, 512)
		vector[0], vector[1] = track.x, track.y
		_, err := pool.Exec(ctx,
			`INSERT INTO track_facets (track_id, track_embedding) VALUES ($1, $2)`,
			track.id, pgvector.NewVector(vector))
		require.NoError(t, err)
	}

	_, err := app.ReplaceQueue(ctx, userID, "dj-echo-test", QueueSource{
		Kind: "tracks", TrackIDs: []int64{seed.trackIDs[0]},
	}, 0, false, "local:dj-echo-test")
	require.NoError(t, err)
	view, err := app.SetQueueDJ(ctx, userID, "dj-echo-test", DJModeEcho)
	require.NoError(t, err)
	require.EqualValues(t, 3, view.Total, "Echo immediately keeps two recommendations ready")
	require.Equal(t, neighborA.trackIDs[0], view.Items[1].TrackID)
	require.True(t, view.Items[1].DJGenerated)
	require.Equal(t, neighborB.trackIDs[0], view.Items[2].TrackID)
	require.True(t, view.Items[2].DJGenerated)

	// Skipping consumes the same rolling slot as natural completion and must
	// immediately restore the two-track runway.
	view, err = app.AdvanceQueue(ctx, userID, "dj-echo-test", view.CurrentItemID, "skip")
	require.NoError(t, err)
	require.Equal(t, neighborA.trackIDs[0], view.Items[view.CurrentIndex].TrackID)
	require.EqualValues(t, 4, view.Total)
	require.Equal(t, neighborB.trackIDs[0], view.Items[int(view.CurrentIndex)+1].TrackID)
	require.Equal(t, neighborC.trackIDs[0], view.Items[int(view.CurrentIndex)+2].TrackID)

	view, err = app.AdvanceQueue(ctx, userID, "dj-echo-test", view.CurrentItemID, "ended")
	require.NoError(t, err)
	require.Equal(t, neighborB.trackIDs[0], view.Items[view.CurrentIndex].TrackID)
	require.EqualValues(t, 5, view.Total)
	require.Equal(t, neighborC.trackIDs[0], view.Items[int(view.CurrentIndex)+1].TrackID)
	require.Equal(t, neighborD.trackIDs[0], view.Items[int(view.CurrentIndex)+2].TrackID)

	seen := map[int64]bool{}
	for _, item := range view.Items {
		require.False(t, seen[item.TrackID], "Echo must not repeat track %d", item.TrackID)
		seen[item.TrackID] = true
	}
}

func TestQueueVoyageFallsBackToChillAndReplansForNewDestination(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "dj-voyage", 4)
	ctx := context.Background()

	vectors := [][2]float32{{1, 0}, {0.9, 0.2}, {0.4, 0.7}, {0, 1}}
	for i, trackID := range f.trackIDs {
		vector := make([]float32, 512)
		vector[0], vector[1] = vectors[i][0], vectors[i][1]
		moods := `{}`
		if i == len(f.trackIDs)-1 {
			moods = `{"mood_relaxed":0.95,"mood_aggressive":0.02,"mood_party":0.05}`
		}
		_, err := pool.Exec(ctx,
			`INSERT INTO track_facets
			 (track_id, track_embedding, artist_embedding, release_embedding, text_embedding, mood_tags)
			 VALUES ($1, $2, $2, $2, $2, $3::jsonb)`,
			trackID, pgvector.NewVector(vector), moods)
		require.NoError(t, err)
	}

	_, err := app.ReplaceQueue(ctx, userID, "dj-voyage-test", QueueSource{
		Kind: "tracks", TrackIDs: []int64{f.trackIDs[0]},
	}, 0, false, "local:dj-voyage-test")
	require.NoError(t, err)
	view, err := app.SetQueueDJ(ctx, userID, "dj-voyage-test", DJModeVoyage)
	require.NoError(t, err)
	require.EqualValues(t, 4, view.Total)
	require.Equal(t, f.trackIDs[3], view.Items[3].TrackID, "the third fallback step is the relaxed destination")

	// A listener-owned destination arriving mid-voyage invalidates the chill
	// path and immediately replaces it with three steps toward the new track.
	destination := setupQueueFixture(t, pool, userID, "dj-voyage-target", 1)
	targetVector := make([]float32, 512)
	targetVector[0], targetVector[1] = -1, 0
	_, err = pool.Exec(ctx,
		`INSERT INTO track_facets
		 (track_id, track_embedding, artist_embedding, release_embedding, text_embedding)
		 VALUES ($1, $2, $2, $2, $2)`,
		destination.trackIDs[0], pgvector.NewVector(targetVector))
	require.NoError(t, err)
	added, err := app.EnqueueTracks(ctx, userID, "dj-voyage-test", destination.trackIDs, "end")
	require.NoError(t, err)
	require.EqualValues(t, 1, added)
	view, err = app.GetQueue(ctx, userID, "dj-voyage-test", nil, 20)
	require.NoError(t, err)
	require.EqualValues(t, 5, view.Total)
	require.Equal(t, destination.trackIDs[0], view.Items[4].TrackID)
	for _, item := range view.Items[1:4] {
		require.True(t, item.DJGenerated)
		require.Equal(t, DJModeVoyage, item.DJMode)
	}
}

func TestSpotlightRanksSameArtistTracksBySonicProximity(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "dj-spotlight-sonic", 4)
	ctx := context.Background()

	vectors := [][2]float32{{1, 0}, {0.99, 0.08}, {0.7, 0.7}, {0, 1}}
	for i, trackID := range f.trackIDs {
		vector := make([]float32, 512)
		vector[0], vector[1] = vectors[i][0], vectors[i][1]
		_, err := pool.Exec(ctx,
			`INSERT INTO track_facets (track_id, track_embedding) VALUES ($1, $2)`,
			trackID, pgvector.NewVector(vector))
		require.NoError(t, err)
	}

	ids, err := app.spotlightDJCandidates(ctx, userID, f.trackIDs[0], []int64{f.trackIDs[0]}, 1, 10)
	require.NoError(t, err)
	require.NotEmpty(t, ids)
	require.Equal(t, f.trackIDs[1], ids[0])
}

func TestTimewarpPrioritizesGenreWithinTheEra(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	seed := setupQueueFixture(t, pool, userID, "dj-timewarp-seed", 1)
	overlap := setupQueueFixture(t, pool, userID, "dj-timewarp-overlap", 1)
	other := setupQueueFixture(t, pool, userID, "dj-timewarp-other", 1)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `UPDATE albums SET year = '2000', genres = '{Synthpop}' WHERE id = $1`, seed.albumID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE albums SET year = '2001', genres = '{Synthpop,Electronic}' WHERE id = $1`, overlap.albumID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE albums SET year = '2000', genres = '{Death Metal}' WHERE id = $1`, other.albumID)
	require.NoError(t, err)

	ids, err := app.timewarpDJCandidates(ctx, userID, seed.trackIDs[0], []int64{seed.trackIDs[0]}, 1, 500)
	require.NoError(t, err)
	overlapIndex, otherIndex := -1, -1
	for i, id := range ids {
		if id == overlap.trackIDs[0] {
			overlapIndex = i
		}
		if id == other.trackIDs[0] {
			otherIndex = i
		}
	}
	require.GreaterOrEqual(t, overlapIndex, 0)
	require.GreaterOrEqual(t, otherIndex, 0)
	require.Less(t, overlapIndex, otherIndex, "genre/style overlap outranks an exact-year mismatch")
}

func TestQueueEncoreYieldsToListenerOwnedTrack(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "dj-alternate", 6)
	ctx := context.Background()

	_, err := app.ReplaceQueue(ctx, userID, "dj-test", QueueSource{
		Kind: "tracks", TrackIDs: []int64{f.trackIDs[0], f.trackIDs[1]},
	}, 0, false, "local:dj-test")
	require.NoError(t, err)
	view, err := app.SetQueueDJ(ctx, userID, "dj-test", DJModeEncore)
	require.NoError(t, err)
	require.EqualValues(t, 3, view.Total)
	require.False(t, view.Items[0].DJGenerated)
	require.True(t, view.Items[1].DJGenerated)
	require.False(t, view.Items[2].DJGenerated)

	// Playing the generated bridge must not recursively add another bridge.
	view, err = app.AdvanceQueue(ctx, userID, "dj-test", view.CurrentItemID, "ended")
	require.NoError(t, err)
	require.True(t, view.Items[view.CurrentIndex].DJGenerated)
	require.EqualValues(t, 3, view.Total)

	// The next user-owned track becomes a fresh anchor. With no listener-owned
	// track behind it, Encore switches to its two-track continuous buffer.
	view, err = app.AdvanceQueue(ctx, userID, "dj-test", view.CurrentItemID, "ended")
	require.NoError(t, err)
	require.False(t, view.Items[view.CurrentIndex].DJGenerated)
	require.EqualValues(t, 5, view.Total)
	require.True(t, view.Items[int(view.CurrentIndex)+1].DJGenerated)
	require.True(t, view.Items[int(view.CurrentIndex)+2].DJGenerated)
}

func TestQueueIncrementalDJMaintainsRunwayAndPreservesPlayingGeneratedItem(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "dj-spotlight", 7)
	ctx := context.Background()

	_, err := app.ReplaceQueue(ctx, userID, "dj-test", QueueSource{
		Kind: "tracks", TrackIDs: []int64{f.trackIDs[0]},
	}, 0, false, "local:dj-test")
	require.NoError(t, err)
	view, err := app.SetQueueDJ(ctx, userID, "dj-test", DJModeSpotlight)
	require.NoError(t, err)
	require.EqualValues(t, 3, view.Total, "current plus two-track DJ runway")
	require.True(t, view.Items[1].DJGenerated)
	require.True(t, view.Items[2].DJGenerated)

	view, err = app.AdvanceQueue(ctx, userID, "dj-test", view.CurrentItemID, "ended")
	require.NoError(t, err)
	require.Equal(t, DJModeSpotlight, view.DJMode)
	require.True(t, view.Items[view.CurrentIndex].DJGenerated)
	require.GreaterOrEqual(t, len(view.Items)-int(view.CurrentIndex)-1, 2, "runway is replenished at the boundary")

	playingGenerated := view.CurrentItemID
	view, err = app.SetQueueDJ(ctx, userID, "dj-test", DJModeOff)
	require.NoError(t, err)
	require.Equal(t, playingGenerated, view.CurrentItemID, "the playing DJ track is history, not disposable future work")
	require.EqualValues(t, 2, view.Total, "original history plus the current generated track remain")
	require.Empty(t, view.Items[int(view.CurrentIndex)+1:])
}

func TestQueueReplacementInvalidatesDJSession(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "dj-replace", 4)
	ctx := context.Background()

	_, err := app.ReplaceQueue(ctx, userID, "dj-test", QueueSource{
		Kind: "tracks", TrackIDs: []int64{f.trackIDs[0]},
	}, 0, false, "local:dj-test")
	require.NoError(t, err)
	_, err = app.SetQueueDJ(ctx, userID, "dj-test", DJModeEncore)
	require.NoError(t, err)

	view, err := app.ReplaceQueue(ctx, userID, "dj-test", QueueSource{
		Kind: "tracks", TrackIDs: []int64{f.trackIDs[2], f.trackIDs[3]},
	}, 0, false, "local:dj-test")
	require.NoError(t, err)
	require.Equal(t, DJModeOff, view.DJMode)
	require.EqualValues(t, 2, view.Total)
	for _, item := range view.Items {
		require.False(t, item.DJGenerated)
	}
}

func ptrInt64(v int64) *int64 { return &v }
