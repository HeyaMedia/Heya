package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestPrimaryMediaAssetLocalPrecedenceAndBackdropOrdering(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "media-asset-local-precedence-test",
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{"/tmp/media-asset-local-precedence"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    userID,
		Settings:     []byte(`{"use_local_data":true}`),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:    lib.ID,
		MediaType:    lib.MediaType,
		Title:        "Local Poster Wins",
		SortTitle:    "local poster wins",
		ProviderKind: "heya",
	})
	require.NoError(t, err)

	upsertPoster := func(source, localPath, remoteURL string) error {
		_, upsertErr := q.UpsertPrimaryMediaAsset(ctx, sqlc.UpsertPrimaryMediaAssetParams{
			MediaItemID: item.ID,
			AssetType:   sqlc.AssetTypePoster,
			Source:      source,
			LocalPath:   localPath,
			RemoteUrl:   remoteURL,
		})
		return upsertErr
	}

	require.NoError(t, upsertPoster("remote", "", "https://metadata.invalid/first.jpg"))
	require.NoError(t, upsertPoster("local", "/library/folder.jpg", ""))
	require.ErrorIs(t, upsertPoster("remote", "", "https://metadata.invalid/second.jpg"), pgx.ErrNoRows,
		"remote refresh must not displace local art while use_local_data is enabled")

	posters, err := q.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
		MediaItemID: item.ID,
		AssetType:   sqlc.AssetTypePoster,
	})
	require.NoError(t, err)
	require.Len(t, posters, 1)
	require.Equal(t, "local", posters[0].Source)
	require.Equal(t, "/library/folder.jpg", posters[0].LocalPath)
	require.Zero(t, posters[0].SortOrder)

	_, err = q.UpdateLibrarySettings(ctx, sqlc.UpdateLibrarySettingsParams{
		ID:       lib.ID,
		Settings: []byte(`{"use_local_data":false}`),
	})
	require.NoError(t, err)
	require.NoError(t, upsertPoster("remote", "", "https://metadata.invalid/second.jpg"),
		"remote art may replace a stale local row after local data is disabled")

	posters, err = q.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
		MediaItemID: item.ID,
		AssetType:   sqlc.AssetTypePoster,
	})
	require.NoError(t, err)
	require.Len(t, posters, 1)
	require.Equal(t, "remote", posters[0].Source)
	require.Equal(t, "https://metadata.invalid/second.jpg", posters[0].RemoteUrl)

	// Backdrops are the collection exception: retain every row and use the
	// explicit sort order (with id as the stable final tie-breaker).
	for _, input := range []struct {
		sort int32
		url  string
	}{{sort: 2, url: "https://metadata.invalid/backdrop-2.jpg"}, {sort: 0, url: "https://metadata.invalid/backdrop-0.jpg"}} {
		_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
			MediaItemID: item.ID,
			AssetType:   sqlc.AssetTypeBackdrop,
			Source:      "remote",
			RemoteUrl:   input.url,
			SortOrder:   input.sort,
		})
		require.NoError(t, err)
	}

	backdrops, err := q.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
		MediaItemID: item.ID,
		AssetType:   sqlc.AssetTypeBackdrop,
	})
	require.NoError(t, err)
	require.Len(t, backdrops, 2)
	require.Equal(t, int32(0), backdrops[0].SortOrder)
	require.Equal(t, int32(2), backdrops[1].SortOrder)

}
