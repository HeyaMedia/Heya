package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestBackfillAbsoluteEpisodesIntegration exercises the startup backfill against
// a real DB: an already-matched anime file with an unresolved absolute number
// (parse_result seasons = JSON null — the case that would trip up a naive
// jsonb_array_length) gets discovered and resolved, while an already-resolved
// file and a normal SxxExx file are left alone. A second run is a no-op.
func TestBackfillAbsoluteEpisodesIntegration(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)

	var libraryID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO libraries (name, media_type, created_by) VALUES ('abs-backfill-test','tv',$1) RETURNING id`,
		userID).Scan(&libraryID))
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, libraryID) })

	var itemID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO media_items (library_id, media_type, title, slug) VALUES ($1,'tv','Yamato','yamato-backfill') RETURNING id`,
		libraryID).Scan(&itemID))
	var seriesID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO tv_series (media_item_id) VALUES ($1) RETURNING id`, itemID).Scan(&seriesID))
	var seasonMain int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO tv_seasons (series_id, season_number) VALUES ($1, 2) RETURNING id`, seriesID).Scan(&seasonMain))
	_, err := pool.Exec(ctx,
		`INSERT INTO tv_episodes (season_id, episode_number, absolute_number) VALUES ($1, 2, 24)`, seasonMain)
	require.NoError(t, err)

	insert := func(path string, blob []byte) int64 {
		var id int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO library_files (library_id, path, parse_result, status, media_item_id) VALUES ($1,$2,$3,'matched',$4) RETURNING id`,
			libraryID, path, blob, itemID).Scan(&id))
		return id
	}
	rawParse := func(seasons, episodes, absolute []int) []byte {
		b, mErr := json.Marshal(map[string]any{"parsed": map[string]any{"release": map[string]any{
			"seasons": seasons, "episodes": episodes, "absoluteEpisodes": absolute,
		}}})
		require.NoError(t, mErr)
		return b
	}

	// Unresolved absolute file straight from the parser — seasons marshals to
	// JSON null, the case NULLIF must handle in the discovery query.
	entry := parser.ParseStoragePath("/data/Anime/Yamato {anidb-2662}/Yamato - 24 - Real Episode.mkv")
	require.NotNil(t, entry.Release)
	require.NotEmpty(t, entry.Release.AbsoluteEpisodes)
	unresolvedBlob, err := json.Marshal(map[string]any{"parsed": entry})
	require.NoError(t, err)
	fUnresolved := insert("/data/Anime/Yamato {anidb-2662}/Yamato - 24 - Real Episode.mkv", unresolvedBlob)

	// Already resolved — must be ignored by both the discovery query and the write.
	fResolved := insert("/data/Anime/Yamato {anidb-2662}/Yamato - 25 - Already.mkv", rawParse([]int{2}, []int{3}, []int{25}))
	// Normal SxxExx file — not absolute, must be ignored.
	insert("/data/TV/Other/Other - S01E01.mkv", rawParse([]int{1}, []int{1}, nil))

	n, err := app.BackfillAbsoluteEpisodes(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n, "only the unresolved absolute file should be resolved")

	seasons, episodes := readReleaseArrays(t, pool, fUnresolved)
	require.Equal(t, []int{2}, seasons)
	require.Equal(t, []int{2}, episodes)

	// The already-resolved file must be untouched (still s2e3).
	rSeasons, rEpisodes := readReleaseArrays(t, pool, fResolved)
	require.Equal(t, []int{2}, rSeasons)
	require.Equal(t, []int{3}, rEpisodes)

	// Self-limiting: nothing left to resolve.
	n2, err := app.BackfillAbsoluteEpisodes(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n2)
}

func readReleaseArrays(t *testing.T, pool *pgxpool.Pool, fileID int64) ([]int, []int) {
	t.Helper()
	var raw []byte
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT parse_result FROM library_files WHERE id=$1`, fileID).Scan(&raw))
	var pr struct {
		Parsed struct {
			Release struct {
				Seasons  []int `json:"seasons"`
				Episodes []int `json:"episodes"`
			} `json:"release"`
		} `json:"parsed"`
	}
	require.NoError(t, json.Unmarshal(raw, &pr))
	return pr.Parsed.Release.Seasons, pr.Parsed.Release.Episodes
}
