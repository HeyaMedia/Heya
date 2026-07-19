package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/generatedwrite"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

func TestMusicArtistDirMatches(t *testing.T) {
	artist := sqlc.Artist{
		Name:     "Asaco",
		SortName: "Asaco",
		Aliases:  []string{"Asako"},
	}
	for _, dir := range []string{
		"/storage/NewMusic/Asaco",
		"/storage/NewMusic/Asaco (Japanese artist)",
		"/storage/NewMusic/Asako",
	} {
		if !musicArtistDirMatches(dir, artist) {
			t.Errorf("expected matching artist directory: %s", dir)
		}
	}
	if musicArtistDirMatches("/storage/NewMusic/DJ Paul", artist) {
		t.Fatal("different artist directory was accepted")
	}
	if musicArtistDirMatches("/storage/NewMusic", artist) {
		t.Fatal("library root was accepted as the artist directory")
	}
}

func TestSaveMusicNFOWorkerRetriesFailedAckWithExactAttestation(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	root := t.TempDir()
	artistDir := filepath.Join(root, "Ado")
	albumDir := filepath.Join(artistDir, "Kyougen")
	require.NoError(t, os.MkdirAll(albumDir, 0o755))

	userID := testutil.TestUserID(t, pool)
	library, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "generated-music-nfo-test",
		MediaType:    sqlc.MediaTypeMusic,
		Paths:        []string{root},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    userID,
		Settings:     []byte(`{"save_nfo":true}`),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, library.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:   library.ID,
		MediaType:   sqlc.MediaTypeMusic,
		Title:       "Ado",
		SortTitle:   "Ado",
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	artist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: "Ado"})
	require.NoError(t, err)
	album, err := q.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID: artist.ID,
		Title:    "Kyougen",
		Year:     "2022",
		Genres:   []string{},
		Tags:     []string{},
	})
	require.NoError(t, err)
	track, err := q.CreateTrack(ctx, sqlc.CreateTrackParams{
		AlbumID:     album.ID,
		DiscNumber:  1,
		TrackNumber: 1,
		Title:       "New Genesis",
	})
	require.NoError(t, err)
	file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID:   library.ID,
		Path:        filepath.Join(albumDir, "01 - New Genesis.flac"),
		ParseResult: []byte("{}"),
		Status:      sqlc.FileStatusMatched,
	})
	require.NoError(t, err)
	_, err = q.UpsertTrackFile(ctx, sqlc.UpsertTrackFileParams{TrackID: track.ID, LibraryFileID: file.ID})
	require.NoError(t, err)
	detachedAlbum, err := q.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID: artist.ID,
		Title:    "Detached Catalog Album",
		Year:     "2025",
		Genres:   []string{},
		Tags:     []string{},
	})
	require.NoError(t, err)
	_, err = q.CreateTrack(ctx, sqlc.CreateTrackParams{
		AlbumID: detachedAlbum.ID, DiscNumber: 1, TrackNumber: 1, Title: "Catalog Only",
	})
	require.NoError(t, err)

	ackErr := errors.New("provenance unavailable")
	generatedWrites := &recordingGeneratedWriteSuppressor{err: ackErr, failures: 1}
	worker := &SaveMusicNFOWorker{
		DB:              pool,
		Progress:        NewTaskProgressBroadcaster(nil),
		GeneratedWrites: generatedWrites,
	}
	job := &river.Job[SaveMusicNFOArgs]{
		JobRow: &rivertype.JobRow{},
		Args:   SaveMusicNFOArgs{ArtistID: artist.ID},
	}
	require.ErrorIs(t, worker.Work(ctx, job), ackErr)
	require.Len(t, generatedWrites.outputs, 1)
	wantAlbumPath, err := generatedwrite.CanonicalPath(filepath.Join(albumDir, "album.nfo"))
	require.NoError(t, err)
	require.Equal(t, wantAlbumPath, generatedWrites.outputs[0].Path)
	require.True(t, generatedWrites.outputs[0].Written)
	require.True(t, generatedWrites.outputs[0].Attested)
	require.FileExists(t, filepath.Join(albumDir, "album.nfo"))
	require.NoFileExists(t, filepath.Join(artistDir, "artist.nfo"), "worker must stop after failed album acknowledgement")

	// The retry does not rewrite the album. It attests the exact desired bytes,
	// acknowledges them, then continues to the artist sidecar.
	require.NoError(t, worker.Work(ctx, job))
	require.Len(t, generatedWrites.outputs, 2)
	wantArtistPath, err := generatedwrite.CanonicalPath(filepath.Join(artistDir, "artist.nfo"))
	require.NoError(t, err)
	require.Equal(t, wantArtistPath, generatedWrites.outputs[1].Path)
	require.True(t, generatedWrites.outputs[1].Written)
	artistNFO, err := os.ReadFile(filepath.Join(artistDir, "artist.nfo"))
	require.NoError(t, err)
	require.NotContains(t, string(artistNFO), detachedAlbum.Title, "an album without a physical release directory must be skipped, not treated as a DB failure")

	// Stable future retries still re-attest both paths so a previously failed
	// durable acknowledgement can always self-heal without a rewrite.
	require.NoError(t, worker.Work(ctx, job))
	require.Len(t, generatedWrites.outputs, 2, "stable retries do not emit filesystem events")

	// A job that was queued while exports were enabled must honor the current
	// setting when it eventually starts; otherwise a large stale queue can
	// recreate sidecars after an administrator deliberately removed them.
	_, err = q.UpdateLibrarySettings(ctx, sqlc.UpdateLibrarySettingsParams{
		ID:       library.ID,
		Settings: []byte(`{"save_nfo":false}`),
	})
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(albumDir, "album.nfo")))
	require.NoError(t, os.Remove(filepath.Join(artistDir, "artist.nfo")))
	require.NoError(t, worker.Work(ctx, job))
	require.NoFileExists(t, filepath.Join(albumDir, "album.nfo"))
	require.NoFileExists(t, filepath.Join(artistDir, "artist.nfo"))
	require.Len(t, generatedWrites.outputs, 2, "disabled queued job must not publish provenance")
}
