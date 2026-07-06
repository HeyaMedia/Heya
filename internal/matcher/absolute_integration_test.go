package matcher

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestReconcileAbsoluteEpisodesIntegration exercises the full resolve-and-store
// path against a real DB: an absolute-numbered anime file gets its real
// season/episode written into parse_result, a special-numbered file is left
// alone, and a second run is a no-op (idempotent).
func TestReconcileAbsoluteEpisodesIntegration(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)

	var libraryID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO libraries (name, media_type, created_by) VALUES ('abs-reconcile-test','tv',$1) RETURNING id`,
		userID).Scan(&libraryID))
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, libraryID) })

	var itemID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO media_items (library_id, media_type, title, slug) VALUES ($1,'tv','Yamato','yamato-abs-test') RETURNING id`,
		libraryID).Scan(&itemID))

	var seriesID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO tv_series (media_item_id) VALUES ($1) RETURNING id`, itemID).Scan(&seriesID))

	var seasonMain, seasonSpecials int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO tv_seasons (series_id, season_number) VALUES ($1, 2) RETURNING id`, seriesID).Scan(&seasonMain))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO tv_seasons (series_id, season_number) VALUES ($1, 0) RETURNING id`, seriesID).Scan(&seasonSpecials))

	// Main episode: absolute 24 lives at season 2, episode 2.
	_, err := pool.Exec(ctx,
		`INSERT INTO tv_episodes (season_id, episode_number, absolute_number) VALUES ($1, 2, 24)`, seasonMain)
	require.NoError(t, err)
	// Special with a stray absolute number 99 — must be excluded from the map.
	_, err = pool.Exec(ctx,
		`INSERT INTO tv_episodes (season_id, episode_number, absolute_number, is_special) VALUES ($1, 3, 99, true)`, seasonSpecials)
	require.NoError(t, err)

	mkFile := func(path string) int64 {
		entry := parser.ParseStoragePath(path)
		require.NotNil(t, entry.Release, "parser should produce a release for %s", path)
		require.NotEmpty(t, entry.Release.AbsoluteEpisodes, "expected an absolute episode for %s", path)
		blob, err := json.Marshal(map[string]any{"parsed": entry})
		require.NoError(t, err)
		var id int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO library_files (library_id, path, parse_result, status, media_item_id) VALUES ($1,$2,$3,'matched',$4) RETURNING id`,
			libraryID, path, blob, itemID).Scan(&id))
		return id
	}
	fMain := mkFile("/data/Anime/Yamato {anidb-2662}/Yamato - 24 - Real Episode.mkv")
	fSpecial := mkFile("/data/Anime/Yamato {anidb-2662}/Yamato - 99 - Only A Special.mkv")

	resolved := func(fileID int64) ([]int, []int) {
		var raw []byte
		require.NoError(t, pool.QueryRow(ctx, `SELECT parse_result FROM library_files WHERE id=$1`, fileID).Scan(&raw))
		var pr releaseArrays
		require.NoError(t, json.Unmarshal(raw, &pr))
		return pr.Parsed.Release.Seasons, pr.Parsed.Release.Episodes
	}

	n, err := ReconcileAbsoluteEpisodes(ctx, q, itemID)
	require.NoError(t, err)
	require.Equal(t, 1, n, "only the main-numbered file should be resolved")

	sMain, eMain := resolved(fMain)
	require.Equal(t, []int{2}, sMain, "absolute 24 -> season 2")
	require.Equal(t, []int{2}, eMain, "absolute 24 -> episode 2")

	sSpec, eSpec := resolved(fSpecial)
	require.Empty(t, sSpec, "absolute 99 maps only to a special and must stay unresolved")
	require.Empty(t, eSpec)

	// Idempotent: nothing left to write on a second pass.
	n2, err := ReconcileAbsoluteEpisodes(ctx, q, itemID)
	require.NoError(t, err)
	require.Equal(t, 0, n2, "second reconcile should be a no-op")
}
