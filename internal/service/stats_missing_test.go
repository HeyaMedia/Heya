package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMissingCountDeduplicatesTrackWithMultipleDeletedQualities(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	q := sqlc.New(pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "missing-track-quality-dedupe", MediaType: sqlc.MediaTypeMusic,
		Paths:        []string{"/media/missing-track-quality-dedupe"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool), Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "Artist", SortTitle: "artist", ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	artist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: item.Title})
	require.NoError(t, err)
	album, err := q.CreateAlbum(ctx, sqlc.CreateAlbumParams{ArtistID: artist.ID, Title: "Album", Genres: []string{}, Tags: []string{}})
	require.NoError(t, err)
	missingTrack, err := q.CreateTrack(ctx, sqlc.CreateTrackParams{AlbumID: album.ID, DiscNumber: 1, TrackNumber: 1, Title: "Missing"})
	require.NoError(t, err)
	liveTrack, err := q.CreateTrack(ctx, sqlc.CreateTrackParams{AlbumID: album.ID, DiscNumber: 1, TrackNumber: 2, Title: "Live"})
	require.NoError(t, err)

	addFile := func(mediaItemID, trackID int64, suffix string, deleted bool) {
		file, fileErr := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
			LibraryID: lib.ID, Path: fmt.Sprintf("/media/missing-track-quality-dedupe/%s", suffix),
			Size: 1, Mtime: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			ParseResult: []byte("{}"), Status: sqlc.FileStatusMatched,
		})
		require.NoError(t, fileErr)
		require.NoError(t, q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
			ID: file.ID, Status: sqlc.FileStatusMatched,
			MediaItemID: pgtype.Int8{Int64: mediaItemID, Valid: true},
		}))
		_, fileErr = q.UpsertTrackFile(ctx, sqlc.UpsertTrackFileParams{TrackID: trackID, LibraryFileID: file.ID})
		require.NoError(t, fileErr)
		if deleted {
			require.NoError(t, q.SoftDeleteLibraryFile(ctx, file.ID))
		}
	}
	addFile(item.ID, missingTrack.ID, "01-missing.flac", true)
	addFile(item.ID, missingTrack.ID, "01-missing.mp3", true)
	addFile(item.ID, liveTrack.ID, "02-live.flac", false)

	countForLibrary := func() int {
		tx, beginErr := pool.Begin(ctx)
		require.NoError(t, beginErr)
		defer func() { _ = tx.Rollback(ctx) }()
		count, countErr := queryMissingCount(ctx, tx, lib.ID)
		require.NoError(t, countErr)
		return count
	}
	require.Equal(t, 1, countForLibrary(), "one logical missing track counts once regardless of deleted quality/file links")

	goneItem, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "Gone Artist", SortTitle: "gone artist", ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	goneArtist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: goneItem.ID, Name: goneItem.Title})
	require.NoError(t, err)
	for index := 1; index <= 2; index++ {
		goneAlbum, createErr := q.CreateAlbum(ctx, sqlc.CreateAlbumParams{
			ArtistID: goneArtist.ID, Title: fmt.Sprintf("Gone Album %d", index), Genres: []string{}, Tags: []string{},
		})
		require.NoError(t, createErr)
		goneTrack, createErr := q.CreateTrack(ctx, sqlc.CreateTrackParams{
			AlbumID: goneAlbum.ID, DiscNumber: 1, TrackNumber: 1, Title: "Gone",
		})
		require.NoError(t, createErr)
		addFile(goneItem.ID, goneTrack.ID, fmt.Sprintf("gone-%d.flac", index), true)
	}

	require.Equal(t, 2, countForLibrary(), "a fully-gone artist is one media_item unit, not the artist plus every child album")
}
