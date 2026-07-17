package service

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/disintegration/imaging"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/stretchr/testify/require"
)

func metadataEditorLibrary(t *testing.T, mediaType sqlc.MediaType) (*App, *sqlc.Queries, sqlc.Library, context.Context) {
	t.Helper()
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "metadata-editor-" + string(mediaType), MediaType: mediaType,
		Paths:        []string{"/tmp/metadata-editor-" + string(mediaType)},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool), Settings: []byte(`{"use_local_data":true}`),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
	return &App{
		db: pool,
		config: &config.Config{
			DataDir: config.Field[string]{Value: t.TempDir()},
		},
	}, q, lib, ctx
}

func TestMetadataEditorUpdatesSeason(t *testing.T) {
	app, q, lib, ctx := metadataEditorLibrary(t, sqlc.MediaTypeTv)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "A Show", SortTitle: "a show", ProviderKind: "heya",
	})
	require.NoError(t, err)
	var zero pgtype.Numeric
	require.NoError(t, zero.Scan("0"))
	series, err := q.CreateTVSeries(ctx, sqlc.CreateTVSeriesParams{
		MediaItemID: item.ID, Genres: []string{}, SpokenLanguages: []string{}, OriginCountry: []string{},
		Rating: zero, Popularity: zero,
	})
	require.NoError(t, err)
	season, err := q.CreateTVSeason(ctx, sqlc.CreateTVSeasonParams{
		SeriesID: series.ID, SeasonNumber: 1, Title: "Season One", Overview: "Before", ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)

	title, overview, airDate := "The First Season", "After", "2024-03-02"
	updated, err := app.UpdateSeason(ctx, season.ID, UpdateSeasonReq{
		Title: &title, Overview: &overview, AirDate: &airDate,
	})
	require.NoError(t, err)
	require.Equal(t, title, updated.Title)
	require.Equal(t, overview, updated.Overview)
	require.True(t, updated.AirDate.Valid)
	require.Equal(t, airDate, updated.AirDate.Time.Format("2006-01-02"))
}

func TestMetadataEditorUpdatesBookCatalogFields(t *testing.T) {
	app, q, lib, ctx := metadataEditorLibrary(t, sqlc.MediaTypeBook)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "Old Title", SortTitle: "old title", ProviderKind: "heya",
	})
	require.NoError(t, err)
	book, err := q.CreateBook(ctx, sqlc.CreateBookParams{
		MediaItemID: item.ID, FilePath: "/library/book.epub", Subjects: []string{},
	})
	require.NoError(t, err)

	title, author, isbn := "New Title", "A New Author", "9780000000001"
	publisher, publishDate, language := "Heya Press", "2025-05-04", "en"
	seriesName, format := "A Series", "epub"
	pageCount, seriesNumber := int32(321), int32(2)
	err = app.UpdateMediaMetadata(ctx, item.ID, UpdateMediaMetadataReq{
		Title: &title, AuthorName: &author, ISBN: &isbn, PageCount: &pageCount,
		Publisher: &publisher, PublishDate: &publishDate, Subjects: []string{"Fiction", "Adventure"},
		Language: &language, SeriesName: &seriesName, SeriesNumber: &seriesNumber, Format: &format,
	})
	require.NoError(t, err)

	gotItem, err := q.GetMediaItemByID(ctx, item.ID)
	require.NoError(t, err)
	require.Equal(t, title, gotItem.Title)
	gotBook, err := q.GetBookByMediaItemID(ctx, item.ID)
	require.NoError(t, err)
	require.Equal(t, book.ID, gotBook.ID)
	require.Equal(t, isbn, gotBook.Isbn)
	require.Equal(t, pageCount, gotBook.PageCount)
	require.Equal(t, []string{"Fiction", "Adventure"}, gotBook.Subjects)
	require.Equal(t, "/library/book.epub", gotBook.FilePath, "editing metadata must preserve the local file")
	require.True(t, gotBook.AuthorID.Valid)
	gotAuthor, err := q.GetAuthorByID(ctx, gotBook.AuthorID.Int64)
	require.NoError(t, err)
	require.Equal(t, author, gotAuthor.Name)
}

func TestMetadataEditorExplicitSingularImageChoiceReplacesPrimary(t *testing.T) {
	app, q, lib, ctx := metadataEditorLibrary(t, sqlc.MediaTypeMovie)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "Poster Test", SortTitle: "poster test", ProviderKind: "heya",
	})
	require.NoError(t, err)
	_, err = q.UpsertPrimaryMediaAsset(ctx, sqlc.UpsertPrimaryMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypePoster, Source: "local", LocalPath: "/local/poster.jpg",
	})
	require.NoError(t, err)
	candidate, err := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypePoster, Source: "remote",
		LocalPath: "/cache/replacement.jpg", RemoteUrl: "https://metadata.invalid/replacement.jpg",
		Label: "legacy-candidate", SortOrder: 10,
	})
	require.NoError(t, err)

	require.NoError(t, app.SetPrimaryAsset(ctx, item.ID, candidate.ID))
	posters, err := q.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{MediaItemID: item.ID, AssetType: sqlc.AssetTypePoster})
	require.NoError(t, err)
	require.Len(t, posters, 1)
	require.Empty(t, posters[0].Label)
	require.Equal(t, "/cache/replacement.jpg", posters[0].LocalPath)
	require.Equal(t, "remote", posters[0].Source)
}

func TestMetadataEditorBackdropChoiceReordersCollection(t *testing.T) {
	app, q, lib, ctx := metadataEditorLibrary(t, sqlc.MediaTypeMovie)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "Backdrop Test", SortTitle: "backdrop test", ProviderKind: "heya",
	})
	require.NoError(t, err)

	var selectedID int64
	for i, remoteURL := range []string{"https://metadata.invalid/first.jpg", "https://metadata.invalid/second.jpg", "https://metadata.invalid/third.jpg"} {
		created, createErr := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
			MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "remote",
			RemoteUrl: remoteURL, Label: "extra", SortOrder: int32(i),
		})
		require.NoError(t, createErr)
		if i == 2 {
			selectedID = created.ID
		}
	}

	require.NoError(t, app.SetPrimaryAsset(ctx, item.ID, selectedID))
	backdrops, err := q.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop,
	})
	require.NoError(t, err)
	require.Len(t, backdrops, 3)
	require.Equal(t, selectedID, backdrops[0].ID)
	for i := range backdrops {
		require.Equal(t, int32(i), backdrops[i].SortOrder)
	}
}

func TestMetadataEditorBackdropShiftHandlesUncachedRemoteRows(t *testing.T) {
	_, q, lib, ctx := metadataEditorLibrary(t, sqlc.MediaTypeMovie)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "Backdrop Shift Test", SortTitle: "backdrop shift test", ProviderKind: "heya",
	})
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
			MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "remote",
			RemoteUrl: fmt.Sprintf("https://metadata.invalid/%d.jpg", i), SortOrder: int32(i),
		})
		require.NoError(t, err)
	}

	require.NoError(t, worker.ShiftMediaAssetSortOrders(ctx, q, item.ID, sqlc.AssetTypeBackdrop))
	backdrops, err := q.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop,
	})
	require.NoError(t, err)
	for i := range backdrops {
		require.Equal(t, int32(i+1), backdrops[i].SortOrder)
	}
}

func TestMetadataEditorCustomBackdropUploadMakesRoomForPrimary(t *testing.T) {
	app, q, lib, ctx := metadataEditorLibrary(t, sqlc.MediaTypeMovie)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "Backdrop Upload Test", SortTitle: "backdrop upload test", ProviderKind: "heya",
	})
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
			MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "remote",
			RemoteUrl: fmt.Sprintf("https://metadata.invalid/%d.jpg", i), SortOrder: int32(i),
		})
		require.NoError(t, err)
	}

	result, err := app.UploadMediaAsset(ctx, item.ID, strings.NewReader("not-a-real-image"), "custom.jpg", "backdrop", "")
	require.NoError(t, err)
	require.NotNil(t, result.Asset)
	require.Equal(t, "custom", result.Asset.Source)
	require.Equal(t, int32(0), result.Asset.SortOrder)
	require.FileExists(t, result.Asset.LocalPath)
	servedPath, ok := app.GetMediaImagePath(ctx, item.ID, "backdrop", 0, "")
	require.True(t, ok)
	require.Equal(t, result.Asset.LocalPath, servedPath)

	backdrops, err := q.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop,
	})
	require.NoError(t, err)
	require.Len(t, backdrops, 3)
	for i := range backdrops {
		require.Equal(t, int32(i), backdrops[i].SortOrder)
	}
}

func TestMaterializedBackdropsDeduplicateToBestResolution(t *testing.T) {
	app, q, lib, ctx := metadataEditorLibrary(t, sqlc.MediaTypeTv)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "Duplicate Backdrops", SortTitle: "duplicate backdrops", ProviderKind: "heya",
	})
	require.NoError(t, err)

	writeBackdrop := func(name string, width, height int) string {
		t.Helper()
		path := filepath.Join(app.config.DataDir.Value, name)
		file, createErr := os.Create(path)
		require.NoError(t, createErr)
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				img.Set(x, y, color.RGBA{
					R: uint8(30 + x*180/(width-1)),
					G: uint8(20 + y*160/(height-1)),
					B: 90, A: 255,
				})
			}
		}
		require.NoError(t, png.Encode(file, img))
		require.NoError(t, file.Close())
		return path
	}

	smallPath := writeBackdrop("small.png", 32, 18)
	largePath := writeBackdrop("large.png", 64, 36)
	small, err := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "remote",
		RemoteUrl: "https://metadata.invalid/small", Label: "en", SortOrder: 0,
	})
	require.NoError(t, err)
	large, err := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "remote",
		RemoteUrl: "https://metadata.invalid/large", Label: "extra", SortOrder: 1,
	})
	require.NoError(t, err)

	_, deduped, err := worker.MaterializeMediaAsset(ctx, app.db, small, smallPath, app.config.DataDir.Value)
	require.NoError(t, err)
	require.False(t, deduped)
	winner, deduped, err := worker.MaterializeMediaAsset(ctx, app.db, large, largePath, app.config.DataDir.Value)
	require.NoError(t, err)
	require.True(t, deduped)
	require.Equal(t, large.ID, winner.ID)
	require.Equal(t, int32(0), winner.SortOrder)

	backdrops, err := q.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop,
	})
	require.NoError(t, err)
	require.Len(t, backdrops, 1)
	require.Equal(t, large.ID, backdrops[0].ID)
	require.Equal(t, int32(64), backdrops[0].Width)
	require.Equal(t, int32(36), backdrops[0].Height)
	require.NoFileExists(t, smallPath)
	require.FileExists(t, largePath)

	updated, err := q.GetMediaItemByID(ctx, item.ID)
	require.NoError(t, err)
	require.Equal(t, largePath, updated.BackdropPath)
}

func TestMaterializedBackdropsDeduplicateConcurrentWorkers(t *testing.T) {
	app, q, lib, ctx := metadataEditorLibrary(t, sqlc.MediaTypeTv)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "Concurrent Backdrops", SortTitle: "concurrent backdrops", ProviderKind: "heya",
	})
	require.NoError(t, err)

	path := filepath.Join(app.config.DataDir.Value, "shared.png")
	file, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, png.Encode(file, image.NewRGBA(image.Rect(0, 0, 64, 36))))
	require.NoError(t, file.Close())

	assets := make([]sqlc.MediaAsset, 2)
	for i := range assets {
		assets[i], err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
			MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "remote",
			RemoteUrl: fmt.Sprintf("https://metadata.invalid/concurrent-%d", i), SortOrder: int32(i),
		})
		require.NoError(t, err)
	}

	start := make(chan struct{})
	errors := make(chan error, len(assets))
	var workers sync.WaitGroup
	for _, asset := range assets {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, _, materializeErr := worker.MaterializeMediaAsset(ctx, app.db, asset, path, app.config.DataDir.Value)
			errors <- materializeErr
		}()
	}
	close(start)
	workers.Wait()
	close(errors)
	for materializeErr := range errors {
		require.NoError(t, materializeErr)
	}

	backdrops, err := q.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop,
	})
	require.NoError(t, err)
	require.Len(t, backdrops, 1)
	require.Equal(t, int32(0), backdrops[0].SortOrder)
}

func TestReconcileMediaItemAssetsDeduplicatesResizeAndRecompression(t *testing.T) {
	app, q, lib, ctx := metadataEditorLibrary(t, sqlc.MediaTypeTv)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "Recompressed Backdrops", SortTitle: "recompressed backdrops", ProviderKind: "heya",
	})
	require.NoError(t, err)

	base := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := 0; y < 180; y++ {
		for x := 0; x < 320; x++ {
			base.Set(x, y, color.RGBA{
				R: uint8((x*7 + y*3) % 256),
				G: uint8((x*2 + y*5) % 256),
				B: uint8(40 + (x+y)%180),
				A: 255,
			})
		}
	}

	cacheRoot := filepath.Join(app.config.DataDir.Value, "images")
	require.NoError(t, os.MkdirAll(cacheRoot, 0o750))
	writeJPEG := func(path string, img image.Image, quality int) {
		t.Helper()
		file, createErr := os.Create(path)
		require.NoError(t, createErr)
		require.NoError(t, jpeg.Encode(file, img, &jpeg.Options{Quality: quality}))
		require.NoError(t, file.Close())
	}

	smallPath := filepath.Join(cacheRoot, "backdrop-small.jpg")
	largePath := filepath.Join(cacheRoot, "backdrop-large.jpg")
	writeJPEG(smallPath, imaging.Resize(base, 160, 90, imaging.Lanczos), 62)
	writeJPEG(largePath, base, 94)

	_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "remote",
		LocalPath: smallPath, RemoteUrl: "https://metadata.invalid/small.jpg", SortOrder: 0,
	})
	require.NoError(t, err)
	large, err := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "remote",
		LocalPath: largePath, RemoteUrl: "https://metadata.invalid/large.jpg", SortOrder: 1,
	})
	require.NoError(t, err)

	stats, err := worker.ReconcileMediaItemAssets(ctx, app.db, item.ID, cacheRoot)
	require.NoError(t, err)
	require.Equal(t, 2, stats.Fingerprinted)
	require.Equal(t, 1, stats.Deduplicated)
	require.Zero(t, stats.Failed)

	backdrops, err := q.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop,
	})
	require.NoError(t, err)
	require.Len(t, backdrops, 1)
	require.Equal(t, large.ID, backdrops[0].ID)
	require.Equal(t, int32(320), backdrops[0].Width)
	require.Equal(t, int32(180), backdrops[0].Height)
	require.NoFileExists(t, smallPath, "redundant managed-cache files should be removed")
	require.FileExists(t, largePath)
}

func TestReconcileMediaItemAssetsNeverDeletesSourceSidecars(t *testing.T) {
	app, q, lib, ctx := metadataEditorLibrary(t, sqlc.MediaTypeMovie)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "Source Backdrops", SortTitle: "source backdrops", ProviderKind: "heya",
	})
	require.NoError(t, err)

	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "fanart.jpg")
	cacheRoot := filepath.Join(app.config.DataDir.Value, "images")
	cachePath := filepath.Join(cacheRoot, "backdrop.jpg")
	require.NoError(t, os.MkdirAll(cacheRoot, 0o750))
	img := image.NewRGBA(image.Rect(0, 0, 64, 36))
	for y := 0; y < 36; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 3), G: uint8(y * 5), B: 90, A: 255})
		}
	}
	for _, path := range []string{sourcePath, cachePath} {
		file, createErr := os.Create(path)
		require.NoError(t, createErr)
		require.NoError(t, jpeg.Encode(file, img, &jpeg.Options{Quality: 90}))
		require.NoError(t, file.Close())
	}

	_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "local", LocalPath: sourcePath, SortOrder: 1,
	})
	require.NoError(t, err)
	_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "local", LocalPath: cachePath, SortOrder: 0,
	})
	require.NoError(t, err)

	stats, err := worker.ReconcileMediaItemAssets(ctx, app.db, item.ID, cacheRoot)
	require.NoError(t, err)
	require.Equal(t, 1, stats.Deduplicated)
	require.FileExists(t, sourcePath, "library sidecars are source data, not disposable cache")
}

func TestMediaAssetIdentityConstraintsIgnoreBackdropOrder(t *testing.T) {
	_, q, lib, ctx := metadataEditorLibrary(t, sqlc.MediaTypeTv)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "Identity Backdrops", SortTitle: "identity backdrops", ProviderKind: "heya",
	})
	require.NoError(t, err)

	_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "local", LocalPath: "/library/fanart.jpg", SortOrder: 0,
	})
	require.NoError(t, err)
	_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "local", LocalPath: "/library/fanart.jpg", SortOrder: 17,
	})
	require.Error(t, err, "the same local file must not be appended at a new carousel position")

	_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "remote", RemoteUrl: "https://metadata.invalid/fanart", SortOrder: 1,
	})
	require.NoError(t, err)
	_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "remote", RemoteUrl: "https://metadata.invalid/fanart", SortOrder: 99,
	})
	require.Error(t, err, "the same upstream image must not be appended at a new carousel position")

	for i, label := range []string{"s01e01", "s01e02"} {
		_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
			MediaItemID: item.ID, AssetType: sqlc.AssetTypeStill, Source: "local",
			LocalPath: "/library/shared-still.jpg", Label: label, SortOrder: int32(i),
		})
		require.NoError(t, err, "structural episode slots intentionally retain separate identities")
	}
}

func TestMetadataEditorDeleteImageOnlyRemovesManagedCacheFiles(t *testing.T) {
	app, q, lib, ctx := metadataEditorLibrary(t, sqlc.MediaTypeMovie)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "Delete Image Test", SortTitle: "delete image test", ProviderKind: "heya",
	})
	require.NoError(t, err)

	cachePath := filepath.Join(app.config.DataDir.Value, "images", "movie", "delete-image-test", "backdrop.jpg")
	require.NoError(t, os.MkdirAll(filepath.Dir(cachePath), 0o750))
	require.NoError(t, os.WriteFile(cachePath, []byte("cache"), 0o600))
	sourcePath := filepath.Join(t.TempDir(), "backdrop2.jpg")
	require.NoError(t, os.WriteFile(sourcePath, []byte("source"), 0o600))

	cached, err := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "custom",
		LocalPath: cachePath, SortOrder: 0,
	})
	require.NoError(t, err)
	source, err := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: item.ID, AssetType: sqlc.AssetTypeBackdrop, Source: "local",
		LocalPath: sourcePath, SortOrder: 1,
	})
	require.NoError(t, err)

	require.NoError(t, app.DeleteMediaAsset(ctx, item.ID, cached.ID))
	require.NoFileExists(t, cachePath)
	require.FileExists(t, sourcePath)
	remaining, err := q.GetMediaAssetByID(ctx, source.ID)
	require.NoError(t, err)
	require.Equal(t, int32(0), remaining.SortOrder)

	require.NoError(t, app.DeleteMediaAsset(ctx, item.ID, source.ID))
	require.FileExists(t, sourcePath, "deleting an editor row must not delete a library sidecar")
}

func TestMetadataEditorAlbumEditsSurviveEnrichment(t *testing.T) {
	app, q, lib, ctx := metadataEditorLibrary(t, sqlc.MediaTypeMusic)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "An Artist", SortTitle: "an artist", ProviderKind: "heya",
	})
	require.NoError(t, err)
	artist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: "An Artist"})
	require.NoError(t, err)
	album, err := q.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID: artist.ID, Title: "Local Album", Slug: "local-album", Year: "2001",
		AlbumType: "album", Genres: []string{}, Tags: []string{},
	})
	require.NoError(t, err)

	title, year := "My Corrected Album", "2002"
	_, err = app.UpdateAlbumMetadata(ctx, album.ID, UpdateAlbumReq{Title: &title, Year: &year})
	require.NoError(t, err)
	require.NoError(t, q.UpdateAlbumEnrichedFields(ctx, sqlc.UpdateAlbumEnrichedFieldsParams{
		ID: album.ID, Column3: "Remote Album", Column4: "1999", Column6: "Remote Label",
	}))
	got, err := q.GetAlbumByID(ctx, album.ID)
	require.NoError(t, err)
	require.Equal(t, title, got.Title)
	require.Equal(t, year, got.Year)
	require.Equal(t, "Remote Label", got.Label, "unlocked fields should still enrich")
}
