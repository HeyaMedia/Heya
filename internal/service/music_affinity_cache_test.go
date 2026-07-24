package service

import (
	"context"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestEnsureUserPlayAffinityMaterializesAndCaches(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	f := setupQueueFixture(t, pool, userID, "affinity-cache", 3)
	ctx := context.Background()

	clear := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM play_events WHERE user_id=$1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM user_play_affinity WHERE user_id=$1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM user_play_affinity_state WHERE user_id=$1`, userID)
	}
	clear() // shared test user — start from a known-empty affinity state
	t.Cleanup(clear)

	// Two completed plays for track 0, one for track 1, none for track 2.
	for _, tc := range []struct {
		track, n int
	}{{0, 2}, {1, 1}} {
		for i := 0; i < tc.n; i++ {
			_, err := pool.Exec(ctx,
				`INSERT INTO play_events (user_id, track_id, completed, listened_seconds, played_at) VALUES ($1, $2, true, 180, now())`,
				userID, f.trackIDs[tc.track])
			require.NoError(t, err)
		}
	}

	require.NoError(t, app.ensureUserPlayAffinity(ctx, userID))

	scores := map[int64]float64{}
	rows, err := pool.Query(ctx, `SELECT track_id, score FROM user_play_affinity WHERE user_id=$1`, userID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id int64
		var s float64
		require.NoError(t, rows.Scan(&id, &s))
		scores[id] = s
	}
	require.NoError(t, rows.Err())
	require.Contains(t, scores, f.trackIDs[0])
	require.Contains(t, scores, f.trackIDs[1])
	require.NotContains(t, scores, f.trackIDs[2], "a track with no completed play earns no affinity row")
	require.Greater(t, scores[f.trackIDs[0]], scores[f.trackIDs[1]], "two recent completions outweigh one")
	require.LessOrEqual(t, scores[f.trackIDs[0]], 2.0, "the implicit completion signal stays bounded")

	// A second call inside the TTL must not recompute (refreshed_at unchanged).
	var before, after time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT refreshed_at FROM user_play_affinity_state WHERE user_id=$1`, userID).Scan(&before))
	require.NoError(t, app.ensureUserPlayAffinity(ctx, userID))
	require.NoError(t, pool.QueryRow(ctx, `SELECT refreshed_at FROM user_play_affinity_state WHERE user_id=$1`, userID).Scan(&after))
	require.Equal(t, before, after, "a fresh cache must not be recomputed")
}
