package worker

import (
	"context"
	"fmt"
	"slices"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// ShiftMediaAssetSortOrders makes room at position zero without violating the
// legacy uniqueness index used by uncached remote images. Updating the whole
// collection with `sort_order = sort_order + 1` can collide while PostgreSQL
// checks each row; moving the highest position first is safe and deterministic.
func ShiftMediaAssetSortOrders(ctx context.Context, q *sqlc.Queries, mediaItemID int64, assetType sqlc.AssetType) error {
	assets, err := q.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
		MediaItemID: mediaItemID,
		AssetType:   assetType,
	})
	if err != nil {
		return fmt.Errorf("list %s assets: %w", assetType, err)
	}
	for _, asset := range slices.Backward(assets) {
		if err := q.SetAssetSortOrder(ctx, sqlc.SetAssetSortOrderParams{
			ID: asset.ID, SortOrder: asset.SortOrder + 1,
		}); err != nil {
			return fmt.Errorf("shift %s asset %d: %w", assetType, asset.ID, err)
		}
	}
	return nil
}
