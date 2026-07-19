package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/generatedwrite"
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
	require.NoError(t, q.UpdateAlbumCoverPath(ctx, sqlc.UpdateAlbumCoverPathParams{ID: album.ID, CoverPath: cached}))
	album.CoverPath = cached
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
	currentTarget, err := albumCoverTargetCurrent(ctx, q, item.ID, album.ID, albumDir)
	require.NoError(t, err)
	require.True(t, currentTarget)
	staleTarget, err := albumCoverTargetCurrent(ctx, q, item.ID, album.ID, filepath.Join(root, "Old Release Dir"))
	require.NoError(t, err)
	require.False(t, staleTarget, "post-stage validation must reject an obsolete release directory")

	ackErr := errors.New("provenance unavailable")
	generatedWrites := &recordingGeneratedWriteSuppressor{err: ackErr, failures: 1}
	w := &SaveImagesWorker{DB: pool, Progress: NewTaskProgressBroadcaster(nil), GeneratedWrites: generatedWrites}
	job := &river.Job[SaveImagesArgs]{
		JobRow: &rivertype.JobRow{},
		Args:   SaveImagesArgs{MediaItemID: item.ID, AlbumID: album.ID, CachedPath: cached, AssetType: "cover"},
	}
	require.ErrorIs(t, w.Work(ctx, job), ackErr)

	body, err := os.ReadFile(filepath.Join(albumDir, "cover.jpg"))
	require.NoError(t, err, "cover.jpg must land in the album release directory")
	require.Equal(t, "cover-bytes", string(body))
	require.Len(t, generatedWrites.outputs, 1)
	wantCoverPath, err := generatedwrite.CanonicalPath(filepath.Join(albumDir, "cover.jpg"))
	require.NoError(t, err)
	require.Equal(t, wantCoverPath, generatedWrites.outputs[0].Path)
	require.True(t, generatedWrites.outputs[0].Written)
	require.True(t, generatedWrites.outputs[0].Attested)

	// The retry attests the exact cached bytes already at cover.jpg and retries
	// durable acknowledgement without rewriting the sidecar.
	require.NoError(t, w.Work(ctx, job))
	require.Len(t, generatedWrites.outputs, 1, "attestation-only retry emits no watcher event")

	// A user edit between attempts is neither overwritten nor sent to the
	// acknowledger, even when it keeps the exact same byte length.
	userEdit := []byte("Cover-bytes")
	require.Len(t, userEdit, len("cover-bytes"))
	require.NoError(t, os.WriteFile(filepath.Join(albumDir, "cover.jpg"), userEdit, 0o644))
	require.NoError(t, w.Work(ctx, job))
	require.Len(t, generatedWrites.outputs, 1)
	body, err = os.ReadFile(filepath.Join(albumDir, "cover.jpg"))
	require.NoError(t, err)
	require.Equal(t, userEdit, body)

	// A folder that already owns cover art is never overwritten or doubled.
	require.NoError(t, os.Remove(filepath.Join(albumDir, "cover.jpg")))
	writeSidecarFixture(t, filepath.Join(albumDir, "folder.jpg"), "user-art")
	require.NoError(t, w.Work(ctx, job))
	require.Len(t, generatedWrites.outputs, 1, "skipped existing artwork must not register generated-write evidence")
	_, statErr := os.Stat(filepath.Join(albumDir, "cover.jpg"))
	require.Error(t, statErr, "existing folder.jpg means the export must skip")

	// A stale album-cover job must not write into an album owned by a different
	// artist/media item after review or rematching changes its association.
	require.NoError(t, os.Remove(filepath.Join(albumDir, "folder.jpg")))
	replacement := writeSidecarFixture(t, filepath.Join(t.TempDir(), "replacement.jpg"), "replacement")
	require.NoError(t, q.UpdateAlbumCoverPath(ctx, sqlc.UpdateAlbumCoverPathParams{ID: album.ID, CoverPath: replacement}))
	require.NoError(t, w.Work(ctx, job))
	require.NoFileExists(t, filepath.Join(albumDir, "cover.jpg"), "stale cached cover must not be published after the album image changes")
	require.Len(t, generatedWrites.outputs, 1)
	require.NoError(t, q.UpdateAlbumCoverPath(ctx, sqlc.UpdateAlbumCoverPathParams{ID: album.ID, CoverPath: cached}))
	otherItem, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: sqlc.MediaTypeMusic, Title: "Other Artist", SortTitle: "Other Artist",
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	_, err = q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: otherItem.ID, Name: "Other Artist"})
	require.NoError(t, err)
	staleJob := &river.Job[SaveImagesArgs]{
		JobRow: &rivertype.JobRow{},
		Args:   SaveImagesArgs{MediaItemID: otherItem.ID, AlbumID: album.ID, CachedPath: cached, AssetType: "cover"},
	}
	require.NoError(t, w.Work(ctx, staleJob))
	require.NoFileExists(t, filepath.Join(albumDir, "cover.jpg"))
	require.Len(t, generatedWrites.outputs, 1, "stale album relationship must not publish provenance")
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
	require.NoError(t, q.UpdateMediaItemPosterPath(ctx, sqlc.UpdateMediaItemPosterPathParams{ID: item.ID, PosterPath: cached}))
	_, err = q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: "Routing Artist"})
	require.NoError(t, err)
	file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID: lib.ID, Path: trackPath, ParseResult: []byte("{}"), Status: sqlc.FileStatusMatched,
	})
	require.NoError(t, err)
	require.NoError(t, q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID: file.ID, Status: sqlc.FileStatusMatched, MediaItemID: pgtype.Int8{Int64: item.ID, Valid: true},
	}))

	w := &SaveImagesWorker{DB: pool, Progress: NewTaskProgressBroadcaster(nil), GeneratedWrites: &recordingGeneratedWriteSuppressor{}}
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

	require.NoError(t, os.Remove(filepath.Join(artistDir, "poster.jpg")))
	require.NoError(t, q.UpdateMediaItemPosterPath(ctx, sqlc.UpdateMediaItemPosterPathParams{
		ID: item.ID, PosterPath: filepath.Join(t.TempDir(), "replacement.jpg"),
	}))
	require.NoError(t, w.Work(ctx, job))
	require.NoFileExists(t, filepath.Join(artistDir, "poster.jpg"), "stale cached artist art must not be republished")
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
	wrongPath := filepath.Join(wrongDir, "01.flac")
	file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID: lib.ID, Path: wrongPath, ParseResult: []byte("{}"), Status: sqlc.FileStatusMatched,
	})
	require.NoError(t, err)
	require.NoError(t, q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID: file.ID, Status: sqlc.FileStatusMatched, MediaItemID: pgtype.Int8{Int64: item.ID, Valid: true},
	}))

	w := &SaveImagesWorker{DB: pool, Progress: NewTaskProgressBroadcaster(nil)}
	job := &river.Job[SaveImagesArgs]{
		JobRow: &rivertype.JobRow{},
		Args:   SaveImagesArgs{MediaItemID: item.ID, FilePath: wrongPath, CachedPath: cached, AssetType: "poster"},
	}
	require.NoError(t, w.Work(ctx, job))

	_, statErr := os.Stat(filepath.Join(root, "Somebody Else", "poster.jpg"))
	require.Error(t, statErr, "identity circuit breaker must refuse a non-matching artist directory")
}

func TestSaveImagesWorkerSkipsQueuedWriteAfterSettingDisabled(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	root := t.TempDir()
	mediaDir := filepath.Join(root, "Queued Movie")
	require.NoError(t, os.MkdirAll(mediaDir, 0o755))
	cached := writeSidecarFixture(t, filepath.Join(t.TempDir(), "poster.jpg"), "poster")

	userID := testutil.TestUserID(t, pool)
	library, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "queued-image-setting-test", MediaType: sqlc.MediaTypeMovie, Paths: []string{root},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID, Settings: []byte(`{"save_images":true}`),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, library.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: library.ID, MediaType: sqlc.MediaTypeMovie, Title: "Queued Movie", SortTitle: "Queued Movie",
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	_, err = q.UpdateLibrarySettings(ctx, sqlc.UpdateLibrarySettingsParams{
		ID:       library.ID,
		Settings: []byte(`{"save_images":false}`),
	})
	require.NoError(t, err)

	generatedWrites := &recordingGeneratedWriteSuppressor{}
	w := &SaveImagesWorker{DB: pool, Progress: NewTaskProgressBroadcaster(nil), GeneratedWrites: generatedWrites}
	job := &river.Job[SaveImagesArgs]{
		JobRow: &rivertype.JobRow{},
		Args: SaveImagesArgs{
			MediaItemID: item.ID,
			FilePath:    filepath.Join(mediaDir, "movie.mkv"),
			CachedPath:  cached,
			AssetType:   "poster",
		},
	}
	require.NoError(t, w.Work(ctx, job))
	require.NoFileExists(t, filepath.Join(mediaDir, "poster.jpg"))
	require.Empty(t, generatedWrites.outputs, "disabled queued job must not publish provenance")
}

func TestSaveImagesWorkerSkipsQueuedWriteAfterFileReassigned(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	root := t.TempDir()
	oldDir := filepath.Join(root, "Old Movie")
	oldPath := filepath.Join(oldDir, "movie.mkv")
	newPath := filepath.Join(root, "New Movie", "movie.mkv")
	cached := writeSidecarFixture(t, filepath.Join(t.TempDir(), "poster.jpg"), "poster")
	userID := testutil.TestUserID(t, pool)
	library, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "queued-image-owner-test", MediaType: sqlc.MediaTypeMovie, Paths: []string{root},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID, Settings: []byte(`{"save_images":true}`),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, library.ID) })
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: library.ID, MediaType: sqlc.MediaTypeMovie, Title: "Moved Movie", SortTitle: "Moved Movie", ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID: library.ID, Path: newPath, ParseResult: []byte("{}"), Status: sqlc.FileStatusMatched,
	})
	require.NoError(t, err)
	require.NoError(t, q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID: file.ID, Status: sqlc.FileStatusMatched, MediaItemID: pgtype.Int8{Int64: item.ID, Valid: true},
	}))

	generatedWrites := &recordingGeneratedWriteSuppressor{}
	w := &SaveImagesWorker{DB: pool, Progress: NewTaskProgressBroadcaster(nil), GeneratedWrites: generatedWrites}
	job := &river.Job[SaveImagesArgs]{JobRow: &rivertype.JobRow{}, Args: SaveImagesArgs{
		MediaItemID: item.ID, FilePath: oldPath, CachedPath: cached, AssetType: "poster",
	}}
	require.NoError(t, w.Work(ctx, job))
	require.NoFileExists(t, filepath.Join(oldDir, "poster.jpg"))
	require.Empty(t, generatedWrites.outputs)
}
