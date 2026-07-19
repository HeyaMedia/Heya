package worker

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil))
	return buf.Bytes()
}

func TestMaterializePendingAssetLandsBytesOnExistingRow(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	img := tinyJPEG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(img)
	}))
	defer server.Close()

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "warm-images-test", MediaType: sqlc.MediaTypeMovie, Paths: []string{"/movies"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: sqlc.MediaTypeMovie, Title: "Warm Movie", SortTitle: "Warm Movie",
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)

	asset, err := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypePoster, Source: "remote",
		RemoteUrl: server.URL + "/poster.jpg",
	})
	require.NoError(t, err)
	require.Empty(t, asset.LocalPath)

	w := &DownloadImageWorker{
		DB:         pool,
		Downloader: images.NewDownloader(t.TempDir(), images.TrustedSource{BaseURL: server.URL}),
		Progress:   NewTaskProgressBroadcaster(nil),
	}
	job := &river.Job[DownloadImageArgs]{
		JobRow: &rivertype.JobRow{},
		Args: DownloadImageArgs{
			MediaItemID: item.ID, EntityType: "media", AssetID: asset.ID,
			URL: asset.RemoteUrl, AssetType: "poster", MediaType: "movie",
		},
	}
	require.NoError(t, w.Work(ctx, job))

	stored, err := q.GetMediaAssetByID(ctx, asset.ID)
	require.NoError(t, err)
	require.NotEmpty(t, stored.LocalPath, "warm download must materialize the pending row in place")
	_, statErr := os.Stat(stored.LocalPath)
	require.NoError(t, statErr, "materialized path must exist on disk")

	updated, err := q.GetMediaItemByID(ctx, item.ID)
	require.NoError(t, err)
	require.Equal(t, stored.LocalPath, updated.PosterPath, "primary poster mirrors into media_items.poster_path")

	// Idempotence: a second run (e.g. sweep raced the enrich warm) is a no-op.
	require.NoError(t, w.Work(ctx, job))
}

func TestMaterializePendingAssetDropsRowWhenUpstreamSays404(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "warm-images-404-test", MediaType: sqlc.MediaTypeTv, Paths: []string{"/tv"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: sqlc.MediaTypeTv, Title: "Warm Show", SortTitle: "Warm Show",
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)

	asset, err := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeStill, Source: "remote",
		RemoteUrl: server.URL + "/still.jpg", Label: "s01e01", SortOrder: 2101,
	})
	require.NoError(t, err)

	w := &DownloadImageWorker{
		DB:         pool,
		Downloader: images.NewDownloader(t.TempDir(), images.TrustedSource{BaseURL: server.URL}),
		Progress:   NewTaskProgressBroadcaster(nil),
	}
	job := &river.Job[DownloadImageArgs]{
		JobRow: &rivertype.JobRow{},
		Args: DownloadImageArgs{
			MediaItemID: item.ID, EntityType: "media", AssetID: asset.ID,
			URL: asset.RemoteUrl, AssetType: "still", MediaType: "tv", Label: "s01e01", SortOrder: 2101,
		},
	}
	require.NoError(t, w.Work(ctx, job), "permanent 404 is not a retryable error")

	_, err = q.GetMediaAssetByID(ctx, asset.ID)
	require.Error(t, err, "dead pending row must be deleted so sweeps converge")
}

func TestDownloadImageWorkerBackdropConflictDoesNotReplaceCurrentProfile(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	img := tinyJPEG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(img)
	}))
	defer server.Close()

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "backdrop-conflict-test", MediaType: sqlc.MediaTypeMovie, Paths: []string{"/movies"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: sqlc.MediaTypeMovie, Title: "Current Backdrop", SortTitle: "Current Backdrop",
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	currentPath := filepath.Join(t.TempDir(), "current-backdrop.jpg")
	require.NoError(t, os.WriteFile(currentPath, img, 0o644))
	require.NoError(t, q.UpdateMediaItemBackdropPath(ctx, sqlc.UpdateMediaItemBackdropPathParams{ID: item.ID, BackdropPath: currentPath}))

	remoteURL := server.URL + "/backdrop.jpg"
	_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "remote",
		LocalPath: currentPath, RemoteUrl: remoteURL,
	})
	require.NoError(t, err)

	w := &DownloadImageWorker{
		DB:         pool,
		Downloader: images.NewDownloader(t.TempDir(), images.TrustedSource{BaseURL: server.URL}),
		Progress:   NewTaskProgressBroadcaster(nil),
	}
	job := &river.Job[DownloadImageArgs]{
		JobRow: &rivertype.JobRow{},
		Args: DownloadImageArgs{
			MediaItemID: item.ID, EntityType: "media", URL: remoteURL,
			AssetType: "backdrop", MediaType: "movie", SortOrder: 0,
		},
	}
	require.NoError(t, w.Work(ctx, job), "the duplicate remote identity is a stale loser, not a retryable failure")

	updated, err := q.GetMediaItemByID(ctx, item.ID)
	require.NoError(t, err)
	require.Equal(t, currentPath, updated.BackdropPath,
		"a conflicting stale download must not replace the current backdrop profile path")
}

func TestDownloadAlbumCoverWarmsRemoteCoverPath(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	img := tinyJPEG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(img)
	}))
	defer server.Close()

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "warm-cover-test", MediaType: sqlc.MediaTypeMusic, Paths: []string{"/music"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: sqlc.MediaTypeMusic, Title: "Warm Artist", SortTitle: "Warm Artist",
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	artist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: "Warm Artist"})
	require.NoError(t, err)
	album, err := q.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID: artist.ID, Title: "Warm Album", Year: "2026", Genres: []string{}, Tags: []string{},
	})
	require.NoError(t, err)
	require.NoError(t, q.UpdateAlbumCoverPath(ctx, sqlc.UpdateAlbumCoverPathParams{
		ID: album.ID, CoverPath: server.URL + "/cover.jpg",
	}))

	covers, err := q.ListArtistAlbumsWithRemoteCovers(ctx, artist.ID)
	require.NoError(t, err)
	require.Len(t, covers, 1, "remote cover must be visible to the enrich-time warm query")

	w := &DownloadImageWorker{
		DB:         pool,
		Downloader: images.NewDownloader(t.TempDir(), images.TrustedSource{BaseURL: server.URL}),
		Progress:   NewTaskProgressBroadcaster(nil),
	}
	job := &river.Job[DownloadImageArgs]{
		JobRow: &rivertype.JobRow{},
		Args: DownloadImageArgs{
			MediaItemID: item.ID, EntityType: "album", AlbumID: album.ID,
			URL: server.URL + "/cover.jpg", AssetType: "cover", MediaType: "music",
		},
	}
	require.NoError(t, w.Work(ctx, job))

	stored, err := q.GetAlbumByID(ctx, album.ID)
	require.NoError(t, err)
	require.NotEmpty(t, stored.CoverPath)
	require.NotContains(t, stored.CoverPath, "http", "cover_path must be rewritten to the local file")
	_, statErr := os.Stat(stored.CoverPath)
	require.NoError(t, statErr)

	after, err := q.ListArtistAlbumsWithRemoteCovers(ctx, artist.ID)
	require.NoError(t, err)
	require.Empty(t, after, "warmed album drops out of the remote-covers query")
}
