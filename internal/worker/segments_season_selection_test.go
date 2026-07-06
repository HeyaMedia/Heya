package worker

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/require"
)

// Exercises the season-selection queries behind local skip-segment
// detection against a real Postgres inside a rolled-back transaction (no
// DB mutation). The load-bearing shape is the LONE GAP: community data
// covered 12 of 13 episodes, so exactly one file is pending — the season
// must still be listed (>= 2 floor counts ALL eligible files, not just
// pending ones) and the episode listing must return the covered partners
// alongside the pending target, or the gap can never be filled.

type seasonQueryFixture struct {
	tx pgx.Tx
	q  *sqlc.Queries
}

func (f *seasonQueryFixture) tvItem(t *testing.T, ctx context.Context, libID int64, title string) int64 {
	t.Helper()
	item, err := f.q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: libID, MediaType: sqlc.MediaTypeTv, Title: title, SortTitle: title,
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	return item.ID
}

// episodeFile creates an eligible episode file: parsed season/episode,
// probed media_info, community pass already run (segments_analyzed_at).
func (f *seasonQueryFixture) episodeFile(t *testing.T, ctx context.Context, libID, itemID int64, season, episode int, path string) int64 {
	t.Helper()
	lf, err := f.q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID: libID, Path: path,
		ParseResult: fmt.Appendf(nil, `{"parsed":{"release":{"seasons":[%d],"episodes":[%d]}}}`, season, episode),
		Status:      sqlc.FileStatusMatched,
	})
	require.NoError(t, err)
	_, err = f.tx.Exec(ctx,
		`UPDATE library_files SET media_item_id = $1, media_info = '{"duration": 1200}', segments_analyzed_at = now() WHERE id = $2`,
		itemID, lf.ID,
	)
	require.NoError(t, err)
	return lf.ID
}

// cover inserts community intro + credits rows, making the file fully
// covered (not pending).
func (f *seasonQueryFixture) cover(t *testing.T, ctx context.Context, fileID int64) {
	t.Helper()
	for _, segType := range []string{"intro", "credits"} {
		require.NoError(t, f.q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
			LibraryFileID: fileID, SegmentType: segType, StartMs: 1_000, EndMs: 31_000, Source: "community:theintrodb",
		}))
	}
}

func newSeasonQueryFixture(t *testing.T, ctx context.Context) (*seasonQueryFixture, int64) {
	t.Helper()
	pool := personMergeTestPool(t)
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	q := sqlc.New(pool).WithTx(tx)

	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		Username: fmt.Sprintf("seasonsel-%s", t.Name()[len(t.Name())-8:]), Email: t.Name() + "@example.com", PasswordHash: "x", IsAdmin: true,
	})
	require.NoError(t, err)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "TV", MediaType: sqlc.MediaTypeTv, Paths: []string{"/tv"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    user.ID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	return &seasonQueryFixture{tx: tx, q: q}, lib.ID
}

func TestListSeasonsPendingDetectionLoneGap(t *testing.T) {
	ctx := context.Background()
	f, libID := newSeasonQueryFixture(t, ctx)

	// Item A — the lone-gap season: 3 eligible files, community covered
	// e1 and e2 fully, e3 has nothing. Must be listed with pending=1.
	itemA := f.tvItem(t, ctx, libID, "Lone Gap Show")
	a1 := f.episodeFile(t, ctx, libID, itemA, 1, 1, "/tv/lonegap/s01e01.mkv")
	a2 := f.episodeFile(t, ctx, libID, itemA, 1, 2, "/tv/lonegap/s01e02.mkv")
	a3 := f.episodeFile(t, ctx, libID, itemA, 1, 3, "/tv/lonegap/s01e03.mkv")
	f.cover(t, ctx, a1)
	f.cover(t, ctx, a2)

	// Item B — single-episode season with a gap: pending but nothing to
	// pair against. Must NOT be listed (the >= 2 total-files floor), so
	// the pump never loops on it.
	itemB := f.tvItem(t, ctx, libID, "Single Episode Show")
	f.episodeFile(t, ctx, libID, itemB, 1, 1, "/tv/single/s01e01.mkv")

	// Item C — fully covered season: 2 eligible files, zero pending.
	// Must NOT be listed (nothing to fill).
	itemC := f.tvItem(t, ctx, libID, "Covered Show")
	c1 := f.episodeFile(t, ctx, libID, itemC, 1, 1, "/tv/covered/s01e01.mkv")
	c2 := f.episodeFile(t, ctx, libID, itemC, 1, 2, "/tv/covered/s01e02.mkv")
	f.cover(t, ctx, c1)
	f.cover(t, ctx, c2)

	// itemA has the smallest id of the three (created first); starting the
	// cursor just below it scopes the sweep to this test's rows even on a
	// shared dev database.
	afterKey := itemA*100000 - 1
	list := func() map[int64]sqlc.ListSeasonsPendingDetectionRow {
		rows, err := f.q.ListSeasonsPendingDetection(ctx, sqlc.ListSeasonsPendingDetectionParams{
			AfterKey: afterKey, RowLimit: 1000,
		})
		require.NoError(t, err)
		out := map[int64]sqlc.ListSeasonsPendingDetectionRow{}
		for _, r := range rows {
			if r.MediaItemID.Valid {
				out[r.MediaItemID.Int64] = r
			}
		}
		return out
	}

	got := list()
	rowA, ok := got[itemA]
	require.True(t, ok, "lone-gap season (1 pending + 2 covered partners) must be listed")
	require.Equal(t, int32(1), rowA.PendingFiles, "pending_files counts only the pending file, not partners")
	require.Equal(t, int32(1), rowA.Season)
	_, ok = got[itemB]
	require.False(t, ok, "a single-episode season has nothing to pair against and must be excluded")
	_, ok = got[itemC]
	require.False(t, ok, "a fully covered season has no gap and must be excluded")

	// Stamping the lone pending file (detection attempted) empties the
	// season's pending set — it must drop out of the listing.
	require.NoError(t, f.q.MarkFileSegmentsDetected(ctx, []int64{a3}))
	got = list()
	_, ok = got[itemA]
	require.False(t, ok, "after the lone gap is stamped the season must no longer be listed")
}

func TestListEpisodeFilesForSeasonDetectionIncludesCoveredPartners(t *testing.T) {
	ctx := context.Background()
	f, libID := newSeasonQueryFixture(t, ctx)

	item := f.tvItem(t, ctx, libID, "Partner Show")
	e1 := f.episodeFile(t, ctx, libID, item, 1, 1, "/tv/partner/s01e01.mkv")
	e2 := f.episodeFile(t, ctx, libID, item, 1, 2, "/tv/partner/s01e02.mkv")
	e3 := f.episodeFile(t, ctx, libID, item, 1, 3, "/tv/partner/s01e03.mkv")
	f.cover(t, ctx, e1)
	f.cover(t, ctx, e2)
	// e3: community found only an intro — still pending (credits gap),
	// and has_intro must report the partial coverage so the worker skips
	// the intro window for it.
	require.NoError(t, f.q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: e3, SegmentType: "intro", StartMs: 1_000, EndMs: 31_000, Source: "community:aniskip",
	}))

	rows, err := f.q.ListEpisodeFilesForSeasonDetection(ctx, sqlc.ListEpisodeFilesForSeasonDetectionParams{
		MediaItemID: item, Season: 1,
	})
	require.NoError(t, err)
	require.Len(t, rows, 3, "covered partners must be listed alongside the pending target")

	byID := map[int64]sqlc.ListEpisodeFilesForSeasonDetectionRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	require.False(t, byID[e1].Pending.Bool, "fully covered file is a partner, not pending")
	require.False(t, byID[e2].Pending.Bool, "fully covered file is a partner, not pending")
	require.True(t, byID[e3].Pending.Bool, "file missing credits must be pending")
	require.True(t, byID[e3].HasIntro, "partial coverage must be reported so the intro window is skipped")
	require.False(t, byID[e3].HasCredits)

	// Ordered by episode number for nearest-neighbor pairing.
	require.Equal(t, []int64{e1, e2, e3}, []int64{rows[0].ID, rows[1].ID, rows[2].ID})

	// Once the pending file is stamped, the season has no targets left —
	// the worker's targets-empty early return handles the race; the rows
	// themselves simply all report pending=false.
	require.NoError(t, f.q.MarkFileSegmentsDetected(ctx, []int64{e3}))
	rows, err = f.q.ListEpisodeFilesForSeasonDetection(ctx, sqlc.ListEpisodeFilesForSeasonDetectionParams{
		MediaItemID: item, Season: 1,
	})
	require.NoError(t, err)
	for _, r := range rows {
		require.False(t, r.Pending.Bool, "stamped files must not be pending")
	}
}
