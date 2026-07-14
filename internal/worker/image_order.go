package worker

import (
	"context"
	"fmt"

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
	for i := len(assets) - 1; i >= 0; i-- {
		if err := q.SetAssetSortOrder(ctx, sqlc.SetAssetSortOrderParams{
			ID: assets[i].ID, SortOrder: assets[i].SortOrder + 1,
		}); err != nil {
			return fmt.Errorf("shift %s asset %d: %w", assetType, assets[i].ID, err)
		}
	}
	return nil
}
