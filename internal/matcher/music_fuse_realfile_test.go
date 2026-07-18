package matcher

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/stretchr/testify/require"
)

// scanRealAlbum inserts real library_files (with the parser's real output) for
// every FLAC in an album dir and runs the actual matcher over them (real
// ffprobe of the real, MusicBrainz-tagged files). Returns the artist id and the
// number of files. Skips the whole test when the album dir is absent.
func scanRealAlbum(t *testing.T, ctx context.Context, m *Matcher, qtx *sqlc.Queries, libID int64, albumDir string) (artistID int64, nFiles int) {
	t.Helper()
	entries, err := os.ReadDir(albumDir)
	if err != nil {
		t.Skipf("fulldata album not present: %v", err)
	}
	var files []sqlc.LibraryFile
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".flac" {
			continue
		}
		p := filepath.Join(albumDir, e.Name())
		parsed := parser.ParseStoragePath(p)
		pr, err := json.Marshal(parsed)
		require.NoError(t, err)
		f, err := qtx.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
			LibraryID: libID, Path: p, ParseResult: pr, Status: sqlc.FileStatusPending,
		})
		require.NoError(t, err)
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Skipf("no flac files in %s", albumDir)
	}
	matched, unmatched, errored, aID := m.matchMusicGroup(ctx, libID, files)
	require.Equal(t, len(files), matched, "every real track must match")
	require.Zero(t, unmatched)
	require.Zero(t, errored)
	require.NotZero(t, aID)
	return aID, len(files)
}

// TestMusicTagFusion_RealFulldata is the definitive non-destructive check: it
// runs the actual matcher (real path parser + real ffprobe of the real,
// MusicBrainz-tagged FLACs) over curated fulldata albums inside a rolled-back
// transaction. Skips when fulldata or ffprobe is unavailable (e.g. CI).
func TestMusicTagFusion_RealFulldata(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not on PATH")
	}
	musicRoot, err := filepath.Abs(filepath.Join("..", "..", "fulldata", "Music"))
	require.NoError(t, err)
	if _, err := os.Stat(musicRoot); err != nil {
		t.Skipf("fulldata not present: %v", err)
	}

	pool := mergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(pool).WithTx(tx)

	_, libID := seedUserAndMusicLib(t, ctx, qtx)
	m := &Matcher{q: qtx, probe: mediaprobe.Probe}

	findAlbum := func(artistID int64, title string) (sqlc.Album, bool) {
		albums, err := qtx.ListAlbumsByArtist(ctx, artistID)
		require.NoError(t, err)
		for _, a := range albums {
			if a.Title == title {
				return a, true
			}
		}
		return sqlc.Album{}, false
	}

	t.Run("NFO'd curated album is unchanged (no regression)", func(t *testing.T) {
		dir := filepath.Join(musicRoot, "3 Doors Down", "3 Doors Down - Album - 2000 - The Better Life")
		artistID, nFiles := scanRealAlbum(t, ctx, m, qtx, libID, dir)

		artist, err := qtx.GetArtistByNameAndDisambiguation(ctx, sqlc.GetArtistByNameAndDisambiguationParams{Lower: "3 Doors Down", Lower_2: ""})
		require.NoError(t, err)
		require.Equal(t, artistID, artist.ID)

		// NFO stays authoritative: the album keeps its NFO year (2021), and the
		// NFO MBIDs are present — exactly as before tag fusion.
		album, ok := findAlbum(artistID, "The Better Life")
		require.True(t, ok, "album must exist")
		require.Equal(t, "2021", album.Year, "NFO year must still win")
		require.NotEmpty(t, artist.MusicbrainzID)
		require.NotEmpty(t, album.MusicbrainzID)

		// Every distinct file owns its own track — no collapse across the two discs.
		var trackCount int
		require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE album_id=$1`, album.ID).Scan(&trackCount))
		require.Equal(t, nFiles, trackCount, "each real file must own a distinct track row")
	})

	t.Run("non-NFO album fuses year + MBID from real tags", func(t *testing.T) {
		dir := filepath.Join(musicRoot, "Ado (Japanese vocalist)", "Ado - Haru Ni Mau")
		artistID, _ := scanRealAlbum(t, ctx, m, qtx, libID, dir)

		album, ok := findAlbum(artistID, "Haru Ni Mau")
		require.True(t, ok, "album must be created from path + tags")
		// The sparse path carries no year; the embedded DATE tag fills it.
		require.Equal(t, "2026", album.Year, "album year must be filled from the tag DATE")
		// No NFO MBID here — it must come from the embedded MUSICBRAINZ_ALBUMID.
		require.Equal(t, "1e943853-7f81-4400-9e93-7f017c202c08", album.MusicbrainzID, "album MBID must be captured from tags")
	})
}
