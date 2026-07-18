package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

func writeSidecarFixture(t *testing.T, path, content string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestSaveImagesWorkerWritesAlbumCoverSidecar(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	root := t.TempDir()
	albumDir := filepath.Join(root, "Sidecar Artist", "First Album")
	require.NoError(t, os.MkdirAll(albumDir, 0o755))
	cached := writeSidecarFixture(t, filepath.Join(t.TempDir(), "cover.jpg"), "cover-bytes")

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "sidecar-cover-test", MediaType: sqlc.MediaTypeMusic, Paths: []string{root},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID, Settings: []byte(`{"save_images":true}`),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: sqlc.MediaTypeMusic, Title: "Sidecar Artist", SortTitle: "Sidecar Artist",
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	artist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: "Sidecar Artist"})
	require.NoError(t, err)
	album, err := q.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID: artist.ID, Title: "First Album", Year: "2026", Genres: []string{}, Tags: []string{},
	})
	require.NoError(t, err)
	track, err := q.CreateTrack(ctx, sqlc.CreateTrackParams{
		AlbumID: album.ID, DiscNumber: 1, TrackNumber: 1, Title: "One",
	})
	require.NoError(t, err)
	file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID: lib.ID, Path: filepath.Join(albumDir, "01.flac"),
		ParseResult: []byte("{}"), Status: sqlc.FileStatusMatched,
	})
	require.NoError(t, err)
	_, err = q.UpsertTrackFile(ctx, sqlc.UpsertTrackFileParams{TrackID: track.ID, LibraryFileID: file.ID})
	require.NoError(t, err)

	w := &SaveImagesWorker{DB: pool, Progress: NewTaskProgressBroadcaster(nil)}
	job := &river.Job[SaveImagesArgs]{
		JobRow: &rivertype.JobRow{},
		Args:   SaveImagesArgs{MediaItemID: item.ID, AlbumID: album.ID, CachedPath: cached, AssetType: "cover"},
	}
	require.NoError(t, w.Work(ctx, job))

	body, err := os.ReadFile(filepath.Join(albumDir, "cover.jpg"))
	require.NoError(t, err, "cover.jpg must land in the album release directory")
	require.Equal(t, "cover-bytes", string(body))

	// A folder that already owns cover art is never overwritten or doubled.
	require.NoError(t, os.Remove(filepath.Join(albumDir, "cover.jpg")))
	writeSidecarFixture(t, filepath.Join(albumDir, "folder.jpg"), "user-art")
	require.NoError(t, w.Work(ctx, job))
	_, statErr := os.Stat(filepath.Join(albumDir, "cover.jpg"))
	require.Error(t, statErr, "existing folder.jpg means the export must skip")
}

func TestSaveImagesWorkerRoutesArtistArtToArtistDir(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	root := t.TempDir()
	artistDir := filepath.Join(root, "Routing Artist")
	albumDir := filepath.Join(artistDir, "Deep Album")
	require.NoError(t, os.MkdirAll(albumDir, 0o755))
	trackPath := filepath.Join(albumDir, "01.flac")
	cached := writeSidecarFixture(t, filepath.Join(t.TempDir(), "poster.jpg"), "artist-poster")

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "sidecar-artist-dir-test", MediaType: sqlc.MediaTypeMusic, Paths: []string{root},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID, Settings: []byte(`{"save_images":true}`),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: sqlc.MediaTypeMusic, Title: "Routing Artist", SortTitle: "Routing Artist",
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	_, err = q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: "Routing Artist"})
	require.NoError(t, err)

	w := &SaveImagesWorker{DB: pool, Progress: NewTaskProgressBroadcaster(nil)}
	job := &river.Job[SaveImagesArgs]{
		JobRow: &rivertype.JobRow{},
		Args:   SaveImagesArgs{MediaItemID: item.ID, FilePath: trackPath, CachedPath: cached, AssetType: "poster"},
	}
	require.NoError(t, w.Work(ctx, job))

	body, err := os.ReadFile(filepath.Join(artistDir, "poster.jpg"))
	require.NoError(t, err, "artist poster must land in the ARTIST directory, not the album's")
	require.Equal(t, "artist-poster", string(body))
	_, statErr := os.Stat(filepath.Join(albumDir, "poster.jpg"))
	require.Error(t, statErr, "album dir must not receive artist-level art")
}

func TestSaveImagesWorkerRefusesMismatchedArtistDir(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	root := t.TempDir()
	wrongDir := filepath.Join(root, "Somebody Else", "An Album")
	require.NoError(t, os.MkdirAll(wrongDir, 0o755))
	cached := writeSidecarFixture(t, filepath.Join(t.TempDir(), "poster.jpg"), "poster")

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "sidecar-mismatch-test", MediaType: sqlc.MediaTypeMusic, Paths: []string{root},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID, Settings: []byte(`{"save_images":true}`),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: sqlc.MediaTypeMusic, Title: "Mismatch Artist", SortTitle: "Mismatch Artist",
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	_, err = q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: "Mismatch Artist"})
	require.NoError(t, err)

	w := &SaveImagesWorker{DB: pool, Progress: NewTaskProgressBroadcaster(nil)}
	job := &river.Job[SaveImagesArgs]{
		JobRow: &rivertype.JobRow{},
		Args:   SaveImagesArgs{MediaItemID: item.ID, FilePath: filepath.Join(wrongDir, "01.flac"), CachedPath: cached, AssetType: "poster"},
	}
	require.NoError(t, w.Work(ctx, job))

	_, statErr := os.Stat(filepath.Join(root, "Somebody Else", "poster.jpg"))
	require.Error(t, statErr, "identity circuit breaker must refuse a non-matching artist directory")
}
