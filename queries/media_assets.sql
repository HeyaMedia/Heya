-- name: CreateMediaAsset :one
INSERT INTO media_assets (media_item_id, asset_type, source, local_path, remote_url, language, label, sort_order, width, height, file_size)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: UpsertPrimaryMediaAsset :one
-- Primary visual slots are singular. A newly discovered local sidecar replaces
-- the remote value, while a later remote refresh must not displace local data.
INSERT INTO media_assets (media_item_id, asset_type, source, local_path, remote_url, language, label, sort_order, width, height, file_size)
VALUES ($1, $2, $3, $4, $5, $6, '', 0, $7, $8, $9)
ON CONFLICT (media_item_id, asset_type)
    WHERE label = '' AND asset_type IN ('poster', 'logo', 'art', 'banner', 'thumb', 'disc', 'clearart')
DO UPDATE SET
    source = EXCLUDED.source,
    local_path = EXCLUDED.local_path,
    remote_url = EXCLUDED.remote_url,
    language = EXCLUDED.language,
    sort_order = 0,
    width = EXCLUDED.width,
    height = EXCLUDED.height,
    file_size = EXCLUDED.file_size
WHERE media_assets.source <> 'local'
   OR EXCLUDED.source = 'local'
   OR NOT EXISTS (
        SELECT 1
        FROM media_items
        JOIN libraries ON libraries.id = media_items.library_id
        WHERE media_items.id = media_assets.media_item_id
          AND COALESCE((libraries.settings->>'use_local_data')::boolean, true)
   )
RETURNING *;

-- name: ListMediaAssets :many
SELECT * FROM media_assets
WHERE media_item_id = $1
ORDER BY asset_type, sort_order, id;

-- name: ListMediaAssetsByType :many
SELECT * FROM media_assets
WHERE media_item_id = $1 AND asset_type = $2
ORDER BY sort_order, id;

-- name: DeleteMediaAssetsByItem :exec
DELETE FROM media_assets WHERE media_item_id = $1;

-- name: CountMediaAssetsByType :many
SELECT asset_type, count(*) as count
FROM media_assets
WHERE media_item_id = $1
GROUP BY asset_type;

-- name: GetMediaAssetByID :one
SELECT * FROM media_assets WHERE id = $1;

-- name: UpdateMediaAssetLocalPath :exec
UPDATE media_assets
SET local_path = $2
WHERE id = $1;

-- name: DeleteMediaAsset :exec
DELETE FROM media_assets WHERE id = $1;

-- name: SetAssetSortOrder :exec
UPDATE media_assets SET sort_order = $2 WHERE id = $1;

-- name: ShiftAssetSortOrders :exec
UPDATE media_assets SET sort_order = sort_order + 1
WHERE media_item_id = $1 AND asset_type = $2::asset_type AND sort_order >= 0;
