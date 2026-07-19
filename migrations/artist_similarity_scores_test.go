package migrations_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestArtistSimilarityScoreAcceptsProviderNativeFanCounts(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	suffix := uuid.NewString()
	library, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "similarity-score-" + suffix,
		MediaType:    sqlc.MediaTypeMusic,
		Paths:        []string{"/similarity-score-" + suffix},
		ScanInterval: pgtype.Interval{Microseconds: 3_600_000_000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, library.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:   library.ID,
		MediaType:   sqlc.MediaTypeMusic,
		Title:       "Similarity Score " + suffix,
		SortTitle:   "similarity score " + suffix,
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	artist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{
		MediaItemID: item.ID,
		Name:        item.Title,
		SortName:    item.SortTitle,
	})
	require.NoError(t, err)

	var precision, scale int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT numeric_precision, numeric_scale
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'artist_similar_artists'
		  AND column_name = 'match_score'
	`).Scan(&precision, &scale))
	require.Equal(t, 23, precision)
	require.Equal(t, 4, scale)

	var score string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO artist_similar_artists (artist_id, rank, name, match_score, provider)
		VALUES ($1, 0, 'Provider Native Score', 262985, 'deezer')
		RETURNING match_score::text
	`, artist.ID).Scan(&score))
	require.Equal(t, "262985.0000", score)
}
