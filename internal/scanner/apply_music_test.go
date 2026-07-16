package scanner

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestApplyMusicArtistAdoptsExistingNameDisambiguation(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scanner-music-artist-adopt-test",
		MediaType:    sqlc.MediaTypeMusic,
		Paths:        []string{"/tmp/music-artist-adopt"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	name := fmt.Sprintf("Scanner Duplicate Artist %d", time.Now().UnixNano())
	disambig := "scanner concurrency regression"
	canonicalItem, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:    lib.ID,
		MediaType:    sqlc.MediaTypeMusic,
		Title:        name,
		SortTitle:    name,
		ProviderKind: "heya",
	})
	require.NoError(t, err)
	canonicalArtist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{
		MediaItemID:    canonicalItem.ID,
		MusicbrainzID:  "scanner-canonical-mbid",
		Name:           name,
		SortName:       name,
		Disambiguation: disambig,
	})
	require.NoError(t, err)

	duplicateItem, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:    lib.ID,
		MediaType:    sqlc.MediaTypeMusic,
		Title:        name,
		SortTitle:    name,
		ProviderKind: "heya",
	})
	require.NoError(t, err)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	qtx := sqlc.New(tx)
	artist, artistAction, err := applyMusicArtist(ctx, qtx, duplicateItem.ID, MusicMaterializePreview{Artist: name}, &metadata.MediaDetail{
		ArtistName:           name,
		ArtistSortName:       name,
		ArtistDisambiguation: disambig,
		ExternalIDs:          map[string]string{"mbid": "scanner-canonical-mbid"},
		ProviderKind:         "heya",
	})
	require.NoError(t, err)
	require.Equal(t, canonicalArtist.ID, artist.ID)
	require.Equal(t, canonicalItem.ID, artist.MediaItemID)
	require.Equal(t, "adopt_artist_row", artistAction)

	item, mediaAction, err := applyMusicCanonicalArtistMediaItem(ctx, qtx, duplicateItem, "create_media_item", artist, &metadata.MediaDetail{
		ArtistName:           name,
		ArtistSortName:       name,
		ArtistDisambiguation: disambig,
		ExternalIDs:          map[string]string{"mbid": "scanner-canonical-mbid"},
		ProviderKind:         "heya",
	})
	require.NoError(t, err)
	require.Equal(t, canonicalItem.ID, item.ID)
	require.Equal(t, "adopt_media_item", mediaAction)
	require.NoError(t, tx.Commit(ctx))

	_, err = q.GetMediaItemByID(ctx, duplicateItem.ID)
	require.True(t, errors.Is(err, pgx.ErrNoRows), "duplicate media item should be removed after adopting canonical artist")
}

func TestApplyMusicAlbumPrefersExistingTupleOverSiblingMBID(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         fmt.Sprintf("scanner-music-album-identity-test-%d", time.Now().UnixNano()),
		MediaType:    sqlc.MediaTypeMusic,
		Paths:        []string{"/tmp/music-album-identity"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: sqlc.MediaTypeMusic,
		Title: "Scanner Wilkinson", SortTitle: "Scanner Wilkinson",
	})
	require.NoError(t, err)
	artist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: "Scanner Wilkinson"})
	require.NoError(t, err)

	parent, err := q.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID: artist.ID, Title: "Afterglow", Year: "2013", MusicbrainzID: "scanner-parent-release-group",
		Genres: []string{}, Tags: []string{},
	})
	require.NoError(t, err)
	edition, err := q.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID: artist.ID, Title: "Afterglow (remixes)", Year: "2013", MusicbrainzID: "scanner-edition-release-group",
		Genres: []string{}, Tags: []string{},
	})
	require.NoError(t, err)

	// Reproduce production evidence: the remixes folder carries the parent
	// release-group MBID even though its exact local tuple already has a row.
	mapping := MusicAlbumFetchMatch{
		LocalAlbum: "Afterglow (Remixes)", LocalYear: "2013",
		LocalExternalIDs: map[string]string{"musicbrainz_release_group": parent.MusicbrainzID},
	}
	got, action, err := applyMusicAlbum(ctx, q, artist.ID, mapping, musicAlbumEntryForApply(nil, mapping))
	require.NoError(t, err)
	require.Equal(t, "update", action)
	require.Equal(t, edition.ID, got.ID, "exact title/year owner must win over a sibling MBID")
	require.Equal(t, edition.MusicbrainzID, got.MusicbrainzID, "conflicting sibling MBID must not move onto the edition")

	unchangedParent, err := q.GetAlbumByID(ctx, parent.ID)
	require.NoError(t, err)
	require.Equal(t, "Afterglow", unchangedParent.Title)
	require.Equal(t, parent.MusicbrainzID, unchangedParent.MusicbrainzID)
}
