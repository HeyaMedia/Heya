package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
