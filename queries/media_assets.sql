-- name: CreateMediaAsset :one
INSERT INTO media_assets (media_item_id, asset_type, source, local_path, remote_url, language, label, sort_order, width, height, file_size)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: ListMediaAssets :many
SELECT * FROM media_assets
WHERE media_item_id = $1
ORDER BY asset_type, sort_order;

-- name: ListMediaAssetsByType :many
SELECT * FROM media_assets
WHERE media_item_id = $1 AND asset_type = $2
ORDER BY sort_order;

-- name: DeleteMediaAssetsByItem :exec
DELETE FROM media_assets WHERE media_item_id = $1;

-- name: CountMediaAssetsByType :many
SELECT asset_type, count(*) as count
FROM media_assets
WHERE media_item_id = $1
GROUP BY asset_type;
