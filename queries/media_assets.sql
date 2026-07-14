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

-- name: ReplacePrimaryMediaAsset :one
-- An explicit metadata-editor choice is authoritative. Unlike the scanner's
-- local-first upsert above, this always replaces the singular visual slot.
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

-- name: UpdateMediaAssetMaterialization :one
UPDATE media_assets
SET local_path = sqlc.arg(local_path),
    content_hash = sqlc.arg(content_hash),
    visual_hash = sqlc.arg(visual_hash),
    width = CASE WHEN sqlc.arg(width)::integer > 0 THEN sqlc.arg(width) ELSE width END,
    height = CASE WHEN sqlc.arg(height)::integer > 0 THEN sqlc.arg(height) ELSE height END,
    file_size = CASE WHEN sqlc.arg(file_size)::bigint > 0 THEN sqlc.arg(file_size) ELSE file_size END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteMediaAsset :exec
DELETE FROM media_assets WHERE id = $1;

-- name: DeleteMediaAssetsByTypeLabel :exec
DELETE FROM media_assets
WHERE media_item_id = $1 AND asset_type = $2 AND label = $3;

-- name: SetAssetSortOrder :exec
UPDATE media_assets SET sort_order = $2 WHERE id = $1;

-- name: StageOrderedMediaAssets :exec
-- Move the collection into a collision-free negative range before assigning
-- its final 0..N order. This is necessary because older remote rows may all
-- have an empty local_path, which makes in-place swaps trip the legacy unique
-- index on (media_item_id, asset_type, sort_order, local_path).
WITH ranked AS (
    SELECT media_assets.id,
           row_number() OVER (ORDER BY media_assets.sort_order, media_assets.id) - 1 AS old_order
    FROM media_assets
    WHERE media_assets.media_item_id = $1
      AND media_assets.asset_type = $2::asset_type
)
UPDATE media_assets AS asset
SET sort_order = (-1000000000 + ranked.old_order)::integer
FROM ranked
WHERE asset.id = ranked.id;

-- name: PromoteOrderedMediaAsset :exec
WITH ranked AS (
    SELECT media_assets.id,
           row_number() OVER (
               ORDER BY CASE WHEN media_assets.id = $3 THEN 0 ELSE 1 END,
                        media_assets.sort_order, media_assets.id
           ) - 1 AS wanted_order
    FROM media_assets
    WHERE media_assets.media_item_id = $1
      AND media_assets.asset_type = $2::asset_type
)
UPDATE media_assets AS asset
SET sort_order = ranked.wanted_order
FROM ranked
WHERE asset.id = ranked.id;

-- name: StageMediaAssetsAfterDedup :exec
-- Preserve the best duplicate at the earliest position occupied by its group,
-- then close all ordering gaps. The doubled keys place the winner before an
-- unrelated row that happened to share that historical sort_order.
WITH ranked AS (
    SELECT media_assets.id,
           row_number() OVER (
               ORDER BY CASE
                            WHEN media_assets.id = sqlc.arg(winner_id) THEN sqlc.arg(desired_order)::bigint * 2
                            ELSE media_assets.sort_order::bigint * 2 + 1
                        END,
                        media_assets.id
           ) - 1 AS wanted_order
    FROM media_assets
    WHERE media_assets.media_item_id = sqlc.arg(media_item_id)
      AND media_assets.asset_type = sqlc.arg(asset_type)::asset_type
)
UPDATE media_assets AS asset
SET sort_order = (-1000000000 + ranked.wanted_order)::integer
FROM ranked
WHERE asset.id = ranked.id;

-- name: FinalizeStagedMediaAssetOrder :exec
UPDATE media_assets
SET sort_order = sort_order + 1000000000
WHERE media_item_id = sqlc.arg(media_item_id)
  AND asset_type = sqlc.arg(asset_type)::asset_type
  AND sort_order < 0;
