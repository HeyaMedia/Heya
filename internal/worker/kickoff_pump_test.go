package worker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPumpStatePatchPreservesSource is the invariant the mid-run "upgrade to
// manual" depends on: the pump's metadata patch must never carry the source
// key, so the jsonb || merge can't undo a MarkActiveKickoffManual that landed
// between the wake's read and its write.
func TestPumpStatePatchPreservesSource(t *testing.T) {
	st := readPumpState([]byte(`{"source": "manual", "enqueued": 7, "track_cursor": 42}`))
	assert.Equal(t, "manual", st.Source)
	assert.Equal(t, 7, st.Enqueued)
	assert.Equal(t, int64(42), st.TrackCursor)

	st.Enqueued = 12
	st.ErrStreak = 0

	var patch map[string]any
	require.NoError(t, json.Unmarshal(st.patch(), &patch))
	_, hasSource := patch["source"]
	assert.False(t, hasSource, "patch must not contain source — it would clobber a concurrent manual upgrade")
	assert.EqualValues(t, 12, patch["enqueued"])
	assert.EqualValues(t, 42, patch["track_cursor"])
	// err_streak must always be written (it's the only field that decreases,
	// so omitempty-style patching would leave a stale streak behind).
	assert.EqualValues(t, 0, patch["err_streak"])

	// Unknown keys (River's own metadata like cancel_attempted_at) are ignored.
	st2 := readPumpState([]byte(`{"cancel_attempted_at": "2026-01-01T00:00:00Z", "album_cursor": 9}`))
	assert.Equal(t, int64(9), st2.AlbumCursor)
	assert.Empty(t, st2.Source)
}

// TestPumpRestartSweep pins the bounded re-sweep: items skipped during the
// run (coalesced with another owner's job, or insert errors) earn exactly
// one verification pass from cursor zero — never a second, so a
// permanently-contested item can't loop the run forever.
func TestPumpRestartSweep(t *testing.T) {
	st := pumpState{Skipped: 3, TrackCursor: 500, AlbumCursor: 20}
	require.True(t, st.restartSweep())
	assert.True(t, st.FinalSweep)
	assert.Zero(t, st.Skipped)
	assert.Zero(t, st.TrackCursor)
	assert.Zero(t, st.AlbumCursor)

	// Skips accumulated during the final sweep don't trigger another pass.
	st.Skipped = 2
	require.False(t, st.restartSweep())

	// Nothing skipped → no re-sweep at all.
	clean := pumpState{TrackCursor: 500}
	require.False(t, clean.restartSweep())
}

// seedPumpMusicTree creates user + music library + media_item/artist/album and
// returns (libID, albumID). Tracks are added by the callers.
func seedPumpMusicTree(t *testing.T, ctx context.Context, qtx *sqlc.Queries) (libID, albumID int64) {
	t.Helper()
	user, err := qtx.CreateUser(ctx, sqlc.CreateUserParams{
		Username: "pumptest", Email: "pumptest@example.com", PasswordHash: "x", IsAdmin: true,
	})
	require.NoError(t, err)
	lib, err := qtx.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "Music", MediaType: sqlc.MediaTypeMusic, Paths: []string{"/music"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    user.ID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	item, err := qtx.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: sqlc.MediaTypeMusic, Title: "Pump Artist", SortTitle: "Pump Artist",
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	artist, err := qtx.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: "Pump Artist"})
	require.NoError(t, err)
	album, err := qtx.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID: artist.ID, Title: "Pump Album", Year: "2026", Genres: []string{}, Tags: []string{},
	})
	require.NoError(t, err)
	return lib.ID, album.ID
}

// seedPumpTrackFile adds one track + backing file and returns the track_files id.
func seedPumpTrackFile(t *testing.T, ctx context.Context, qtx *sqlc.Queries, libID, albumID int64, num int32, path string) (trackID, trackFileID int64) {
	t.Helper()
	track, err := qtx.CreateTrack(ctx, sqlc.CreateTrackParams{
		AlbumID: albumID, DiscNumber: 1, TrackNumber: num, Title: path, FilePath: path,
	})
	require.NoError(t, err)
	lf, err := qtx.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID: libID, Path: path, ParseResult: []byte("{}"), Status: sqlc.FileStatusMatched,
	})
	require.NoError(t, err)
	tf, err := qtx.UpsertTrackFile(ctx, sqlc.UpsertTrackFileParams{
		TrackID: track.ID, LibraryFileID: lf.ID, Format: "flac",
	})
	require.NoError(t, err)
	return track.ID, tf.ID
}

func markTrackFileMeasured(t *testing.T, ctx context.Context, qtx *sqlc.Queries, trackFileID int64) {
	t.Helper()
	require.NoError(t, qtx.UpdateTrackFileLoudness(ctx, sqlc.UpdateTrackFileLoudnessParams{
		ID:             trackFileID,
		IntegratedLufs: pgNumericFromFloat(-14.5), TruePeakDb: pgNumericFromFloat(-0.3),
		LoudnessRangeDb: pgNumericFromFloat(8.1), SamplePeakDb: pgNumericFromFloat(-0.6),
	}))
	require.NoError(t, qtx.UpdateTrackFileBoundaries(ctx, sqlc.UpdateTrackFileBoundariesParams{ID: trackFileID}))
}

// TestListTrackFilesPendingLoudnessCursor covers the pump's track sweep: the
// after_id cursor must be strictly exclusive (so a permanently-failing file is
// visited exactly once per run) and fully-measured files must drop out.
func TestListTrackFilesPendingLoudnessCursor(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(pool).WithTx(tx)

	baseTrackFileID := maxTableID(t, ctx, tx, "track_files")
	libID, albumID := seedPumpMusicTree(t, ctx, qtx)
	_, tfDone := seedPumpTrackFile(t, ctx, qtx, libID, albumID, 1, "/music/a/01.flac")
	_, tfPending1 := seedPumpTrackFile(t, ctx, qtx, libID, albumID, 2, "/music/a/02.flac")
	_, tfPending2 := seedPumpTrackFile(t, ctx, qtx, libID, albumID, 3, "/music/a/03.flac")
	markTrackFileMeasured(t, ctx, qtx, tfDone)

	ids := func(rows []sqlc.ListTrackFilesPendingLoudnessRow) []int64 {
		out := make([]int64, len(rows))
		for i, r := range rows {
			out[i] = r.ID
		}
		return out
	}

	// Full sweep from zero: only the two unmeasured files, in id order.
	rows, err := qtx.ListTrackFilesPendingLoudness(ctx, sqlc.ListTrackFilesPendingLoudnessParams{AfterID: baseTrackFileID, RowLimit: 100})
	require.NoError(t, err)
	assert.Equal(t, []int64{tfPending1, tfPending2}, ids(rows))

	// Cursor is exclusive: after the first pending id, only the second remains.
	rows, err = qtx.ListTrackFilesPendingLoudness(ctx, sqlc.ListTrackFilesPendingLoudnessParams{AfterID: tfPending1, RowLimit: 100})
	require.NoError(t, err)
	assert.Equal(t, []int64{tfPending2}, ids(rows))

	// Past the last id the sweep is exhausted — the pump's finish condition.
	rows, err = qtx.ListTrackFilesPendingLoudness(ctx, sqlc.ListTrackFilesPendingLoudnessParams{AfterID: tfPending2, RowLimit: 100})
	require.NoError(t, err)
	assert.Empty(t, rows)

	// RowLimit bounds the wave.
	rows, err = qtx.ListTrackFilesPendingLoudness(ctx, sqlc.ListTrackFilesPendingLoudnessParams{AfterID: baseTrackFileID, RowLimit: 1})
	require.NoError(t, err)
	assert.Equal(t, []int64{tfPending1}, ids(rows))
}

// TestListAlbumsPendingLoudnessCursor covers the pump's album phase: an album
// only becomes listable once every track file is measured, and the cursor is
// strictly exclusive.
func TestListAlbumsPendingLoudnessCursor(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(pool).WithTx(tx)

	baseAlbumID := maxTableID(t, ctx, tx, "albums")
	libID, albumID := seedPumpMusicTree(t, ctx, qtx)
	_, tf1 := seedPumpTrackFile(t, ctx, qtx, libID, albumID, 1, "/music/a/01.flac")
	_, tf2 := seedPumpTrackFile(t, ctx, qtx, libID, albumID, 2, "/music/a/02.flac")
	markTrackFileMeasured(t, ctx, qtx, tf1)

	// One track still unmeasured → album not eligible.
	rows, err := qtx.ListAlbumsPendingLoudness(ctx, sqlc.ListAlbumsPendingLoudnessParams{AfterID: baseAlbumID, RowLimit: 100})
	require.NoError(t, err)
	assert.Empty(t, rows)

	// All tracks measured → eligible.
	markTrackFileMeasured(t, ctx, qtx, tf2)
	rows, err = qtx.ListAlbumsPendingLoudness(ctx, sqlc.ListAlbumsPendingLoudnessParams{AfterID: baseAlbumID, RowLimit: 100})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, albumID, rows[0].ID)

	// Cursor past the album → exhausted.
	rows, err = qtx.ListAlbumsPendingLoudness(ctx, sqlc.ListAlbumsPendingLoudnessParams{AfterID: albumID, RowLimit: 100})
	require.NoError(t, err)
	assert.Empty(t, rows)

	// Album-level measurement done → drops out entirely.
	require.NoError(t, qtx.UpdateAlbumLoudness(ctx, sqlc.UpdateAlbumLoudnessParams{
		ID: albumID, IntegratedLufs: pgNumericFromFloat(-13.9),
		TruePeakDb: pgNumericFromFloat(-0.2), LoudnessRangeDb: pgNumericFromFloat(7.7),
	}))
	rows, err = qtx.ListAlbumsPendingLoudness(ctx, sqlc.ListAlbumsPendingLoudnessParams{AfterID: baseAlbumID, RowLimit: 100})
	require.NoError(t, err)
	assert.Empty(t, rows)
}

// TestListPendingAnalysisTracksCursor covers the sonic pump's sweep: cursor
// exclusivity and the "stub facets row at current version hides the track"
// contract that keeps permanently-broken files from churning.
func TestListPendingAnalysisTracksCursor(t *testing.T) {
	pool := personMergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(pool).WithTx(tx)

	baseTrackID := maxTableID(t, ctx, tx, "tracks")
	libID, albumID := seedPumpMusicTree(t, ctx, qtx)
	track1, _ := seedPumpTrackFile(t, ctx, qtx, libID, albumID, 1, "/music/a/01.flac")
	track2, _ := seedPumpTrackFile(t, ctx, qtx, libID, albumID, 2, "/music/a/02.flac")

	list := func(after int64) []int64 {
		rows, err := qtx.ListPendingAnalysisTracks(ctx, sqlc.ListPendingAnalysisTracksParams{
			AfterID:            after,
			MaxDurationSeconds: sonicanalysis.MaxAnalysisDurationSeconds,
			AnalyzerVersion:    sonicanalysis.AnalyzerVersion,
			LimitCount:         100,
		})
		require.NoError(t, err)
		return rows
	}

	assert.Equal(t, []int64{track1, track2}, list(baseTrackID))
	assert.Equal(t, []int64{track2}, list(track1))
	assert.Empty(t, list(track2))

	// A stub facets row at the current analyzer version (what the worker
	// writes when analysis fails permanently) hides the track from the sweep.
	require.NoError(t, qtx.UpsertTrackFacetsStub(ctx, sqlc.UpsertTrackFacetsStubParams{
		TrackID: track1, AnalyzerVersion: sonicanalysis.AnalyzerVersion,
	}))
	assert.Equal(t, []int64{track2}, list(baseTrackID))
}

func maxTableID(t *testing.T, ctx context.Context, tx pgx.Tx, table string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, tx.QueryRow(ctx, "SELECT COALESCE(MAX(id), 0) FROM "+table).Scan(&id))
	return id
}
