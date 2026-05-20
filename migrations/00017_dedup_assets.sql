-- +goose Up
DELETE FROM media_assets a USING media_assets b
WHERE a.id > b.id
  AND a.media_item_id = b.media_item_id
  AND a.asset_type = b.asset_type
  AND a.sort_order = b.sort_order
  AND a.local_path = b.local_path;

CREATE UNIQUE INDEX IF NOT EXISTS idx_media_assets_unique
ON media_assets (media_item_id, asset_type, sort_order, local_path);

-- +goose Down
DROP INDEX IF EXISTS idx_media_assets_unique;
