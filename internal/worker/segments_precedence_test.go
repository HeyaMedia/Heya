package worker

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/require"
)

// Exercises the final skip-segment precedence policy against a real
// Postgres inside a rolled-back transaction (no DB mutation): manual beats
// everything; community and chromaprint are peers by arrival order
// (whichever writes first for a given (file, type) wins, and neither may
// clobber the other); blackframe loses to both. This replaced an earlier
// policy where chromaprint unconditionally outranked and replaced
// community data — see the precedence note atop queries/media_segments.sql
// for the incident that prompted the revert.

// segTestFile creates a bare library_file row to hang media_segments rows
// off of — the precedence helpers under test only need a library_file_id,
// not a fully populated media item.
func segTestFile(t *testing.T, ctx context.Context, q *sqlc.Queries, libID int64, path string) int64 {
	t.Helper()
	f, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID: libID, Path: path, ParseResult: []byte("{}"), Status: sqlc.FileStatusMatched,
	})
	require.NoError(t, err)
	return f.ID
}

func segTestLibrary(t *testing.T, ctx context.Context, q *sqlc.Queries, name string) int64 {
	t.Helper()
	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		Username: name, Email: name + "@example.com", PasswordHash: "x", IsAdmin: true,
	})
	require.NoError(t, err)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: name, MediaType: sqlc.MediaTypeTv, Paths: []string{"/x"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    user.ID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	return lib.ID
}

func segRowsByType(t *testing.T, ctx context.Context, q *sqlc.Queries, fileID int64) map[string][]sqlc.MediaSegment {
	t.Helper()
	rows, err := q.ListMediaSegmentsForFile(ctx, fileID)
	require.NoError(t, err)
	out := map[string][]sqlc.MediaSegment{}
	for _, r := range rows {
		out[r.SegmentType] = append(out[r.SegmentType], r)
	}
	return out
}

// --- insertChromaprintSegmentIfAbsent (season worker / local detection) ---

func TestInsertChromaprintSegmentIfAbsent_FreshInsert(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	lib := segTestLibrary(t, ctx, q, "chroma-fresh")
	fileID := segTestFile(t, ctx, q, lib, "/x/fresh.mkv")

	require.NoError(t, insertChromaprintSegmentIfAbsent(ctx, q, fileID, "intro", 1000, 2000))

	rows := segRowsByType(t, ctx, q, fileID)
	require.Len(t, rows["intro"], 1, "nothing existed yet — chromaprint should insert")
	require.Equal(t, "chromaprint", rows["intro"][0].Source)
	require.Equal(t, int64(1000), rows["intro"][0].StartMs)
	require.Equal(t, int64(2000), rows["intro"][0].EndMs)
}

func TestInsertChromaprintSegmentIfAbsent_SkipsWhenManualExists(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	lib := segTestLibrary(t, ctx, q, "chroma-manual")
	fileID := segTestFile(t, ctx, q, lib, "/x/manual.mkv")
	require.NoError(t, q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileID, SegmentType: "intro", StartMs: 0, EndMs: 90_000, Source: "manual",
	}))

	require.NoError(t, insertChromaprintSegmentIfAbsent(ctx, q, fileID, "intro", 1000, 2000))

	rows := segRowsByType(t, ctx, q, fileID)
	require.Len(t, rows["intro"], 1, "manual row must never be touched")
	require.Equal(t, "manual", rows["intro"][0].Source)
	require.Equal(t, int64(0), rows["intro"][0].StartMs)
	require.Equal(t, int64(90_000), rows["intro"][0].EndMs)
}

// TestInsertChromaprintSegmentIfAbsent_SkipsWhenCommunityExists is the core
// policy change under test: chromaprint used to replace an existing
// community row (it "outranked" community); it must now leave it alone —
// community and chromaprint are peers by arrival order.
func TestInsertChromaprintSegmentIfAbsent_SkipsWhenCommunityExists(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	lib := segTestLibrary(t, ctx, q, "chroma-community")
	fileID := segTestFile(t, ctx, q, lib, "/x/community.mkv")
	require.NoError(t, q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileID, SegmentType: "intro", StartMs: 5_000, EndMs: 35_000, Source: "community:theintrodb",
	}))

	require.NoError(t, insertChromaprintSegmentIfAbsent(ctx, q, fileID, "intro", 1000, 2000))

	rows := segRowsByType(t, ctx, q, fileID)
	require.Len(t, rows["intro"], 1, "a community row that arrived first must not be replaced by chromaprint")
	require.Equal(t, "community:theintrodb", rows["intro"][0].Source)
	require.Equal(t, int64(5_000), rows["intro"][0].StartMs)
	require.Equal(t, int64(35_000), rows["intro"][0].EndMs)
}

func TestInsertChromaprintSegmentIfAbsent_SkipsWhenChromaprintAlreadyExists(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	lib := segTestLibrary(t, ctx, q, "chroma-idempotent")
	fileID := segTestFile(t, ctx, q, lib, "/x/idempotent.mkv")
	require.NoError(t, insertChromaprintSegmentIfAbsent(ctx, q, fileID, "credits", 100_000, 110_000))

	// A second (e.g. retried) measurement with different numbers must not
	// overwrite the first — insert-if-absent, not replace.
	require.NoError(t, insertChromaprintSegmentIfAbsent(ctx, q, fileID, "credits", 200_000, 210_000))

	rows := segRowsByType(t, ctx, q, fileID)
	require.Len(t, rows["credits"], 1, "a second chromaprint attempt must not duplicate or overwrite the first")
	require.Equal(t, int64(100_000), rows["credits"][0].StartMs)
	require.Equal(t, int64(110_000), rows["credits"][0].EndMs)
}

func TestInsertChromaprintSegmentIfAbsent_ReplacesBlackframe(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	lib := segTestLibrary(t, ctx, q, "chroma-blackframe")
	fileID := segTestFile(t, ctx, q, lib, "/x/blackframe.mkv")
	require.NoError(t, q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileID, SegmentType: "credits", StartMs: 500_000, EndMs: 600_000, Source: "blackframe",
	}))

	require.NoError(t, insertChromaprintSegmentIfAbsent(ctx, q, fileID, "credits", 505_000, 600_000))

	rows := segRowsByType(t, ctx, q, fileID)
	require.Len(t, rows["credits"], 1, "chromaprint is a real measurement — it must still replace a blackframe guess")
	require.Equal(t, "chromaprint", rows["credits"][0].Source)
	require.Equal(t, int64(505_000), rows["credits"][0].StartMs)
}

// --- writeCommunitySegments (community worker) ---

func TestWriteCommunitySegments_SkipsWhenManualExists(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	lib := segTestLibrary(t, ctx, q, "community-manual")
	fileID := segTestFile(t, ctx, q, lib, "/x/manual.mkv")
	require.NoError(t, q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileID, SegmentType: "intro", StartMs: 0, EndMs: 90_000, Source: "manual",
	}))

	picked := []pickedSegment{{Type: "intro", StartMs: 1_000, EndMs: 2_000, Source: "community:skipmedb"}}
	require.NoError(t, writeCommunitySegments(ctx, q, fileID, picked))

	rows := segRowsByType(t, ctx, q, fileID)
	require.Len(t, rows["intro"], 1, "manual row must never be touched")
	require.Equal(t, "manual", rows["intro"][0].Source)
}

// TestWriteCommunitySegments_SkipsWhenChromaprintExists is the mirror-image
// guard: a fresh community fetch must not overwrite a chromaprint row that
// a local-detection pass already wrote for the same type — the two are
// peers, and this worker only ever clears its OWN (community:*) rows.
func TestWriteCommunitySegments_SkipsWhenChromaprintExists(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	lib := segTestLibrary(t, ctx, q, "community-chromaprint")
	fileID := segTestFile(t, ctx, q, lib, "/x/chromaprint.mkv")
	require.NoError(t, q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileID, SegmentType: "credits", StartMs: 3_000_000, EndMs: 3_600_000, Source: "chromaprint",
	}))

	picked := []pickedSegment{{Type: "credits", StartMs: 3_010_000, EndMs: 3_600_000, Source: "community:theintrodb"}}
	require.NoError(t, writeCommunitySegments(ctx, q, fileID, picked))

	rows := segRowsByType(t, ctx, q, fileID)
	require.Len(t, rows["credits"], 1, "a chromaprint row that arrived first must not be replaced by community")
	require.Equal(t, "chromaprint", rows["credits"][0].Source)
	require.Equal(t, int64(3_000_000), rows["credits"][0].StartMs)
}

func TestWriteCommunitySegments_ReplacesBlackframe(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	lib := segTestLibrary(t, ctx, q, "community-blackframe")
	fileID := segTestFile(t, ctx, q, lib, "/x/blackframe.mkv")
	require.NoError(t, q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileID, SegmentType: "credits", StartMs: 500_000, EndMs: 600_000, Source: "blackframe",
	}))

	picked := []pickedSegment{{Type: "credits", StartMs: 510_000, EndMs: 600_000, Source: "community:skipmedb"}}
	require.NoError(t, writeCommunitySegments(ctx, q, fileID, picked))

	rows := segRowsByType(t, ctx, q, fileID)
	require.Len(t, rows["credits"], 1, "community data must still replace a blackframe guess")
	require.Equal(t, "community:skipmedb", rows["credits"][0].Source)
	require.Equal(t, int64(510_000), rows["credits"][0].StartMs)
}

// TestWriteCommunitySegments_RefreshesItsOwnPriorRows exercises the
// re-check path: an earlier community winner for a type must be cleared
// and replaced when a fresh fetch picks a different winner, since this is
// this worker's own data (not a peer's).
func TestWriteCommunitySegments_RefreshesItsOwnPriorRows(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	lib := segTestLibrary(t, ctx, q, "community-refresh")
	fileID := segTestFile(t, ctx, q, lib, "/x/refresh.mkv")
	require.NoError(t, q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileID, SegmentType: "intro", StartMs: 1_000, EndMs: 2_000, Source: "community:aniskip",
	}))

	picked := []pickedSegment{{Type: "intro", StartMs: 1_500, EndMs: 2_500, Source: "community:skipmedb"}}
	require.NoError(t, writeCommunitySegments(ctx, q, fileID, picked))

	rows := segRowsByType(t, ctx, q, fileID)
	require.Len(t, rows["intro"], 1, "a stale community winner must be replaced by a fresh fetch, not accumulated")
	require.Equal(t, "community:skipmedb", rows["intro"][0].Source)
	require.Equal(t, int64(1_500), rows["intro"][0].StartMs)
}

func TestWriteCommunitySegments_NoCandidatesLeavesExistingRowsAlone(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	lib := segTestLibrary(t, ctx, q, "community-empty")
	fileID := segTestFile(t, ctx, q, lib, "/x/empty.mkv")
	require.NoError(t, q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileID, SegmentType: "intro", StartMs: 0, EndMs: 90_000, Source: "manual",
	}))

	require.NoError(t, writeCommunitySegments(ctx, q, fileID, nil))

	rows := segRowsByType(t, ctx, q, fileID)
	require.Len(t, rows["intro"], 1, "a re-check with nothing new must not disturb an unrelated manual row")
	require.Equal(t, "manual", rows["intro"][0].Source)
}

// --- UpsertMediaSegmentByRank (the DB-level race backstop) ---
//
// The workers' EXISTS guards are read-committed, so two writers on
// different queues can both see "no row yet" and race the insert; the
// partial unique index idx_media_segments_file_type forces the loser
// through the upsert's rank comparison. These tests drive the upsert
// directly, simulating the loser's statement AFTER the winner's row has
// already committed — the exact interleaving the EXISTS checks can't see.

func upsertSeg(t *testing.T, ctx context.Context, q *sqlc.Queries, fileID int64, segType, source string, startMs, endMs int64) {
	t.Helper()
	require.NoError(t, q.UpsertMediaSegmentByRank(ctx, sqlc.UpsertMediaSegmentByRankParams{
		LibraryFileID: fileID, SegmentType: segType, StartMs: startMs, EndMs: endMs, Source: source,
	}))
}

// TestUpsertMediaSegmentByRank_ChromaprintOverwritesBlackframe is the
// race that motivated replacing plain ON CONFLICT DO NOTHING: the movie
// blackframe worker commits first, the chromaprint tx (whose blackframe
// delete saw nothing — the row wasn't committed yet) then conflicts. DO
// NOTHING would let the heuristic guess permanently beat the
// measurement; the rank upsert overwrites it in place.
func TestUpsertMediaSegmentByRank_ChromaprintOverwritesBlackframe(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	lib := segTestLibrary(t, ctx, q, "upsert-cp-bf")
	fileID := segTestFile(t, ctx, q, lib, "/x/upsert-cp-bf.mkv")
	require.NoError(t, q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileID, SegmentType: "credits", StartMs: 500_000, EndMs: 600_000, Source: "blackframe",
	}))

	upsertSeg(t, ctx, q, fileID, "credits", "chromaprint", 505_000, 600_000)

	rows := segRowsByType(t, ctx, q, fileID)
	require.Len(t, rows["credits"], 1)
	require.Equal(t, "chromaprint", rows["credits"][0].Source, "a measurement must overwrite a committed blackframe guess, not lose to it")
	require.Equal(t, int64(505_000), rows["credits"][0].StartMs)
}

func TestUpsertMediaSegmentByRank_BlackframeNoOpsAgainstChromaprint(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	lib := segTestLibrary(t, ctx, q, "upsert-bf-cp")
	fileID := segTestFile(t, ctx, q, lib, "/x/upsert-bf-cp.mkv")
	require.NoError(t, q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileID, SegmentType: "credits", StartMs: 505_000, EndMs: 600_000, Source: "chromaprint",
	}))

	upsertSeg(t, ctx, q, fileID, "credits", "blackframe", 400_000, 600_000)

	rows := segRowsByType(t, ctx, q, fileID)
	require.Len(t, rows["credits"], 1)
	require.Equal(t, "chromaprint", rows["credits"][0].Source, "a blackframe guess must never displace a measurement")
	require.Equal(t, int64(505_000), rows["credits"][0].StartMs)
}

// TestUpsertMediaSegmentByRank_CommunityOverCommunityNoOps: equal rank
// no-ops, so a community insert meeting a committed community row keeps
// the existing one. Corollary: the weekly re-check can NOT refresh its
// own rows through the upsert — writeCommunitySegments deletes the
// file's community:% rows first in the same tx, so its inserts never
// meet their own old rows (see the ORDERING CONTRACT comment there).
func TestUpsertMediaSegmentByRank_CommunityOverCommunityNoOps(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	lib := segTestLibrary(t, ctx, q, "upsert-comm-comm")
	fileID := segTestFile(t, ctx, q, lib, "/x/upsert-comm-comm.mkv")
	require.NoError(t, q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileID, SegmentType: "intro", StartMs: 1_000, EndMs: 31_000, Source: "community:aniskip",
	}))

	upsertSeg(t, ctx, q, fileID, "intro", "community:skipmedb", 2_000, 32_000)

	rows := segRowsByType(t, ctx, q, fileID)
	require.Len(t, rows["intro"], 1)
	require.Equal(t, "community:aniskip", rows["intro"][0].Source, "equal rank keeps the committed row")
	require.Equal(t, int64(1_000), rows["intro"][0].StartMs)
}

// TestUpsertMediaSegmentByRank_PeersNoOpInBothOrders locks in the
// deliberate deviation from a strict manual > chromaprint > community >
// blackframe ranking: community and chromaprint are PEERS (equal rank),
// so whichever committed first wins regardless of which one races in
// second. Ranking chromaprint above community here would resurrect the
// replace-on-sight behavior the gap-filler revert removed.
func TestUpsertMediaSegmentByRank_PeersNoOpInBothOrders(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	lib := segTestLibrary(t, ctx, q, "upsert-peers")

	// Community committed first; chromaprint races in second → no-op.
	fileA := segTestFile(t, ctx, q, lib, "/x/upsert-peers-a.mkv")
	require.NoError(t, q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileA, SegmentType: "intro", StartMs: 5_000, EndMs: 35_000, Source: "community:theintrodb",
	}))
	upsertSeg(t, ctx, q, fileA, "intro", "chromaprint", 1_000, 30_000)
	rowsA := segRowsByType(t, ctx, q, fileA)
	require.Len(t, rowsA["intro"], 1)
	require.Equal(t, "community:theintrodb", rowsA["intro"][0].Source, "a racing chromaprint write must not overwrite a committed community row")

	// Chromaprint committed first; community races in second → no-op.
	fileB := segTestFile(t, ctx, q, lib, "/x/upsert-peers-b.mkv")
	require.NoError(t, q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileB, SegmentType: "intro", StartMs: 1_000, EndMs: 30_000, Source: "chromaprint",
	}))
	upsertSeg(t, ctx, q, fileB, "intro", "community:skipmedb", 5_000, 35_000)
	rowsB := segRowsByType(t, ctx, q, fileB)
	require.Len(t, rowsB["intro"], 1)
	require.Equal(t, "chromaprint", rowsB["intro"][0].Source, "a racing community write must not overwrite a committed chromaprint row")
}

func TestUpsertMediaSegmentByRank_ManualBeatsEverything(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(pool).WithTx(tx)

	lib := segTestLibrary(t, ctx, q, "upsert-manual")
	fileID := segTestFile(t, ctx, q, lib, "/x/upsert-manual.mkv")

	// Manual overwrites a community row (top rank wins)...
	require.NoError(t, q.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileID, SegmentType: "intro", StartMs: 5_000, EndMs: 35_000, Source: "community:theintrodb",
	}))
	upsertSeg(t, ctx, q, fileID, "intro", "manual", 0, 90_000)
	rows := segRowsByType(t, ctx, q, fileID)
	require.Len(t, rows["intro"], 1)
	require.Equal(t, "manual", rows["intro"][0].Source)

	// ...and nothing overwrites manual.
	upsertSeg(t, ctx, q, fileID, "intro", "chromaprint", 1_000, 30_000)
	rows = segRowsByType(t, ctx, q, fileID)
	require.Len(t, rows["intro"], 1)
	require.Equal(t, "manual", rows["intro"][0].Source, "no automated source may displace a manual correction")
	require.Equal(t, int64(0), rows["intro"][0].StartMs)
}
