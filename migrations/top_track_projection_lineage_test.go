package migrations_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/karbowiak/heya/migrations"
	"github.com/stretchr/testify/require"
)

func TestTopTrackProjectionLineageRepairIsNarrowAndIdempotent(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	suffix := uuid.NewString()
	library, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "lineage-repair-" + suffix, MediaType: sqlc.MediaTypeMusic,
		Paths:        []string{"/lineage-repair-" + suffix},
		ScanInterval: pgtype.Interval{Microseconds: 3_600_000_000, Valid: true},
		CreatedBy:    userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: library.ID, MediaType: sqlc.MediaTypeMusic,
		Title: "Lineage Repair " + suffix, SortTitle: "lineage repair " + suffix,
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	artist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: item.Title, SortName: item.SortTitle})
	require.NoError(t, err)
	entityID := uuid.New()
	_, err = q.UpsertMetadataEntityBinding(ctx, sqlc.UpsertMetadataEntityBindingParams{
		LocalKind: "artist", LocalID: artist.ID, EntityID: entityID,
		EntityKind: "artist", SchemaVersion: 1, ProjectionVersion: 10,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM metadata_entity_bindings WHERE local_kind='artist' AND local_id=$1`, artist.ID)
		testutil.CleanupLibrary(t, pool, library.ID)
	})

	_, err = pool.Exec(ctx, `INSERT INTO artist_top_tracks(artist_id,rank,provider,provider_rank,title) VALUES($1,1,'lastfm',1,'Preserved')`, artist.ID)
	require.NoError(t, err)
	for _, scope := range []string{"top_tracks", "credits"} {
		_, err = q.UpsertMetadataProjectionState(ctx, sqlc.UpsertMetadataProjectionStateParams{
			LocalKind: "artist", LocalID: artist.ID, Scope: scope, EntityID: entityID,
			EntityKind: "artist", ProjectionVersion: 10,
		})
		require.NoError(t, err)
	}

	body, err := migrations.FS.ReadFile("00062_repair_top_track_projection_lineage.sql")
	require.NoError(t, err)
	up := strings.SplitN(string(body), "-- +goose Down", 2)[0]
	up = strings.Replace(up, "-- +goose Up", "", 1)
	_, err = pool.Exec(ctx, up)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, up)
	require.NoError(t, err, "repair must be safe on a repeated deployment")

	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM metadata_projection_states WHERE local_kind='artist' AND local_id=$1 AND scope='top_tracks'`, artist.ID).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM metadata_projection_states WHERE local_kind='artist' AND local_id=$1 AND scope='credits'`, artist.ID).Scan(&count))
	require.Equal(t, 1, count, "unrelated scope checkpoint was deleted")
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM artist_top_tracks WHERE artist_id=$1 AND title='Preserved'`, artist.ID).Scan(&count))
	require.Equal(t, 1, count, "repair must preserve the last good read model until refetch succeeds")
}
