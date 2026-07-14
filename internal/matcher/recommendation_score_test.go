package matcher

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestRecommendationProviderScoreDoesNotOverflowVoteAverage(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(tx)

	library, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "recommendation-provider-score-test", MediaType: sqlc.MediaTypeTv,
		Paths: []string{"/tmp/recommendation-provider-score-test"}, CreatedBy: testutil.TestUserID(t, pool),
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: library.ID, MediaType: sqlc.MediaTypeTv, Title: "Breaking Bad",
		SortTitle: "breaking bad", ProviderKind: "heya",
	})
	require.NoError(t, err)

	m := &Matcher{q: q, inTx: true}
	err = m.storeRichMetadata(ctx, item.ID, &metadata.MediaDetail{Recommendations: []metadata.RecommendationDetail{{
		Title: "Better Call Saul", MediaType: "tv", ProviderScore: 164.9115,
	}}})
	require.NoError(t, err)

	var voteAverage, providerScore float64
	require.NoError(t, tx.QueryRow(ctx, `SELECT vote_average::double precision, provider_score FROM media_recommendations WHERE media_item_id = $1`, item.ID).Scan(&voteAverage, &providerScore))
	require.Zero(t, voteAverage)
	require.InDelta(t, 164.9115, providerScore, 0.000001)
}
