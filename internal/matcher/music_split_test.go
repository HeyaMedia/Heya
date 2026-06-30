package matcher

import (
	"context"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFolderToNameDisambig(t *testing.T) {
	cases := []struct{ folder, name, disambig string }{
		{"Avicii", "Avicii", ""},
		{"Adaro (Dutch DJ & producer)", "Adaro", "Dutch DJ & producer"},
		{"666 (German techno+trance act)", "666", "German techno+trance act"},
		{"¥$ (Ye & Ty Dolla $ign)", "¥$", "Ye & Ty Dolla $ign"},
		{"(techno)", "(techno)", ""}, // peeling would empty the name → keep whole
		{"  Spaced  ", "Spaced", ""},
	}
	for _, c := range cases {
		n, d := folderToNameDisambig(c.folder)
		assert.Equal(t, c.name, n, "name for %q", c.folder)
		assert.Equal(t, c.disambig, d, "disambig for %q", c.folder)
	}
}

// seedAlbumWithFile creates an album + one track + one library_file at `path` +
// the track_file link, so the folder-segment query has a real path to match.
func seedAlbumWithFile(t *testing.T, ctx context.Context, qtx *sqlc.Queries, libID, artistID int64, title, year, path string) int64 {
	t.Helper()
	album, err := qtx.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID: artistID, Title: title, Year: year, Genres: []string{}, Tags: []string{},
	})
	require.NoError(t, err)
	track, err := qtx.CreateTrack(ctx, sqlc.CreateTrackParams{
		AlbumID: album.ID, DiscNumber: 1, TrackNumber: 1, Title: title + " t1",
	})
	require.NoError(t, err)
	lf, err := qtx.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID: libID, Path: path, ParseResult: []byte("{}"), Status: sqlc.FileStatusMatched,
	})
	require.NoError(t, err)
	_, err = qtx.UpsertTrackFile(ctx, sqlc.UpsertTrackFileParams{
		TrackID: track.ID, LibraryFileID: lf.ID, Format: "flac",
	})
	require.NoError(t, err)
	return album.ID
}

// addTrackFile attaches a second library_file (under a different folder) to an
// existing track — reproducing the fused state where one track carries copies
// of the same song from two artist folders.
func addTrackFile(t *testing.T, ctx context.Context, qtx *sqlc.Queries, libID, trackID int64, path string) {
	t.Helper()
	lf, err := qtx.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID: libID, Path: path, ParseResult: []byte("{}"), Status: sqlc.FileStatusMatched,
	})
	require.NoError(t, err)
	_, err = qtx.UpsertTrackFile(ctx, sqlc.UpsertTrackFileParams{
		TrackID: trackID, LibraryFileID: lf.ID, Format: "flac",
	})
	require.NoError(t, err)
}

// TestSplitArtistByFolder_MixedTrack covers the Ark Patrol / Bulletproof residual:
// a single track whose files span two folders (a prior bad merge fused two
// same-titled releases). Splitting one folder must peel only that folder's file
// onto a sibling track under the destination artist, leaving the rest behind.
func TestSplitArtistByFolder_MixedTrack(t *testing.T) {
	pool := mergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(pool).WithTx(tx)
	m := &Matcher{q: qtx}

	_, libID := seedUserAndMusicLib(t, ctx, qtx)
	const portland = "Ark Patrol (Electronic duo from Portland)"
	const hawaii = "Ark Patrol (Electronic artist born in Hawaii based in LA)"

	src := seedBareArtist(t, ctx, qtx, libID, "Ark Patrol", "Electronic duo from Portland", "")
	album := seedAlbumWithFile(t, ctx, qtx, libID, src, "Socialite", "2022",
		"/storage/Music/"+portland+"/Socialite/01.flac")
	srcTrack := trackAt(t, ctx, qtx, album, 1, 1)
	addTrackFile(t, ctx, qtx, libID, srcTrack, "/storage/Music/"+hawaii+"/Socialite/01.flac")

	// Split the Hawaii folder out.
	res, err := m.SplitArtistByFolder(ctx, src, hawaii)
	require.NoError(t, err)
	require.Equal(t, 0, res.AlbumsMoved)
	require.Equal(t, 1, res.AlbumsSplit)

	filesUnder := func(trackID int64, folder string) int {
		var n int
		require.NoError(t, tx.QueryRow(ctx,
			`SELECT count(*) FROM track_files tf JOIN library_files lf ON lf.id=tf.library_file_id
			 WHERE tf.track_id=$1 AND $2 = ANY(string_to_array(lf.path,'/'))`, trackID, folder).Scan(&n))
		return n
	}

	// Source track kept only the Portland file; the source album stayed put.
	require.Equal(t, 1, filesUnder(srcTrack, portland))
	require.Equal(t, 0, filesUnder(srcTrack, hawaii))
	srcAlbums, err := qtx.ListAlbumsByArtist(ctx, src)
	require.NoError(t, err)
	require.Len(t, srcAlbums, 1)
	require.Equal(t, album, srcAlbums[0].ID)

	// The Hawaii file landed on a sibling track under the new artist's album.
	newAlbums, err := qtx.ListAlbumsByArtist(ctx, res.NewArtistID)
	require.NoError(t, err)
	require.Len(t, newAlbums, 1)
	newTrack := trackAt(t, ctx, qtx, newAlbums[0].ID, 1, 1)
	require.NotEqual(t, srcTrack, newTrack)
	require.Equal(t, 1, filesUnder(newTrack, hawaii))
	require.Equal(t, 0, filesUnder(newTrack, portland))
}

func TestSplitArtistByFolder(t *testing.T) {
	pool := mergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(pool).WithTx(tx)
	m := &Matcher{q: qtx}

	_, libID := seedUserAndMusicLib(t, ctx, qtx)

	// Fused artist "Alicia Keys" wrongly owns an Avicii album (files under the
	// /…/Avicii/ folder) alongside its real one.
	src := seedBareArtist(t, ctx, qtx, libID, "Alicia Keys", "", "")
	keepAlbum := seedAlbumWithFile(t, ctx, qtx, libID, src, "Songs in A Minor", "2001",
		"/storage/Music/Alicia Keys/Songs in A Minor/01.flac")
	moveAlbum := seedAlbumWithFile(t, ctx, qtx, libID, src, "True", "2013",
		"/storage/Music/Avicii/True/01.flac")

	res, err := m.SplitArtistByFolder(ctx, src, "Avicii")
	require.NoError(t, err)
	require.Equal(t, 1, res.AlbumsMoved)
	require.Equal(t, "Avicii", res.NewArtistName)
	require.NotEqual(t, src, res.NewArtistID)

	// The Avicii album moved to the new artist; the Alicia album stayed.
	srcAlbums, err := qtx.ListAlbumsByArtist(ctx, src)
	require.NoError(t, err)
	require.Len(t, srcAlbums, 1)
	require.Equal(t, keepAlbum, srcAlbums[0].ID)

	newAlbums, err := qtx.ListAlbumsByArtist(ctx, res.NewArtistID)
	require.NoError(t, err)
	require.Len(t, newAlbums, 1)
	require.Equal(t, moveAlbum, newAlbums[0].ID)

	// Tracks rode along with the moved album (nothing recomputed).
	tracks, err := qtx.ListTracksByAlbum(ctx, moveAlbum)
	require.NoError(t, err)
	require.Len(t, tracks, 1)

	// The new artist starts un-enriched so the next pass re-enriches it.
	newArtist, err := qtx.GetArtistByID(ctx, res.NewArtistID)
	require.NoError(t, err)
	require.False(t, newArtist.DiscographyEnrichedAt.Valid)

	// Idempotent: nothing left under "Avicii" on a re-run.
	res2, err := m.SplitArtistByFolder(ctx, src, "Avicii")
	require.NoError(t, err)
	require.Equal(t, 0, res2.AlbumsMoved)
}
