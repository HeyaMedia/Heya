package scanner

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

func TestBindCanonicalMetadataPromotesScannerIdentity(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	q := sqlc.New(tx)

	userID := testutil.TestUserID(t, pool)
	library, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "canonical-binding-test", MediaType: sqlc.MediaTypeMovie,
		Paths: []string{"/tmp/canonical-binding-test"}, CreatedBy: userID,
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: library.ID, MediaType: sqlc.MediaTypeMovie, Title: "The Matrix",
		SortTitle: "the matrix", ProviderKind: "heya",
	})
	require.NoError(t, err)
	_, err = q.UpsertLocalMediaIdentity(ctx, sqlc.UpsertLocalMediaIdentityParams{
		LibraryID: library.ID, MediaType: sqlc.MediaTypeMovie, IdentityKey: "tmdb:603",
		Title: "The Matrix", Year: "1999", Confidence: 1, Source: "scanner",
		ReviewStatus: "matched", MetadataProviderID: "heyametadata:v2:candidate:movie:aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		MediaItemID: pgInt8(item.ID), RawIdentity: []byte("{}"),
	})
	require.NoError(t, err)

	const entityID = "050aa960-5191-4fd7-9461-39c2180fe6cb"
	err = bindCanonicalMetadata(ctx, q, "media_item", item.ID, &metadata.MediaDetail{
		CanonicalID: entityID, CanonicalKind: "movie", SchemaVersion: 2, ProjectionVersion: 17,
	})
	require.NoError(t, err)
	binding, err := q.GetMediaItemMetadataBinding(ctx, item.ID)
	require.NoError(t, err)
	require.Equal(t, entityID, binding.EntityID.String())
	require.Equal(t, int64(17), binding.ProjectionVersion)

	var providerID string
	require.NoError(t, tx.QueryRow(ctx, `SELECT metadata_provider_id FROM local_media_identities WHERE library_id = $1 AND identity_key = $2`, library.ID, "tmdb:603").Scan(&providerID))
	require.Equal(t, "heyametadata:v2:entity:"+entityID, providerID)
}
