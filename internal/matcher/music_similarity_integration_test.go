package matcher

import (
	"context"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/stretchr/testify/require"
)

func TestWriteArtistSimilarArtistsAcceptsProviderNativeScores(t *testing.T) {
	pool := mergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := sqlc.New(pool).WithTx(tx)
	m := &Matcher{q: qtx, inTx: true}

	_, libraryID := seedUserAndMusicLib(t, ctx, qtx)
	item, err := qtx.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:   libraryID,
		MediaType:   sqlc.MediaTypeMusic,
		Title:       "Toby Fox",
		SortTitle:   "toby fox",
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	artist, err := qtx.CreateArtist(ctx, sqlc.CreateArtistParams{
		MediaItemID: item.ID,
		Name:        item.Title,
		SortName:    item.SortTitle,
	})
	require.NoError(t, err)

	require.NoError(t, m.writeArtistSimilarArtists(ctx, artist.ID, []metadata.SimilarArtistEntry{
		{Name: "The Living Tombstone", Match: 262985, Provider: "deezer"},
	}))

	var score, provider string
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT match_score::text, provider
		FROM artist_similar_artists
		WHERE artist_id = $1 AND rank = 0
	`, artist.ID).Scan(&score, &provider))
	require.Equal(t, "262985.0000", score)
	require.Equal(t, "deezer", provider)
}

func TestWriteArtistSimilarArtistsRollsBackFailedReplacement(t *testing.T) {
	pool := mergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := sqlc.New(pool).WithTx(tx)

	userID, libraryID := seedUserAndMusicLib(t, ctx, qtx)
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM libraries WHERE id = $1`, libraryID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	}()
	item, err := qtx.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: libraryID, MediaType: sqlc.MediaTypeMusic,
		Title: "Atomic Similarity", SortTitle: "atomic similarity", ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	artist, err := qtx.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: item.Title})
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO artist_similar_artists (artist_id, rank, name, match_score, provider)
		VALUES ($1, 0, 'Preserved', 0.9, 'lastfm')
	`, artist.ID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	m := New(pool, MatchOptions{}, nil, nil)
	err = m.writeArtistSimilarArtists(ctx, artist.ID, []metadata.SimilarArtistEntry{
		{Name: "Replacement", Match: 1, Provider: "deezer"},
		{Name: "Broken", Match: 1, Provider: string([]byte{0})},
	})
	require.Error(t, err)

	var name string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT name FROM artist_similar_artists WHERE artist_id = $1 AND rank = 0
	`, artist.ID).Scan(&name))
	require.Equal(t, "Preserved", name)
}
