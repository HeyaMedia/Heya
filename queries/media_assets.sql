-- name: CreateMediaAsset :one
INSERT INTO media_assets (media_item_id, asset_type, source, local_path, remote_url, language, label, sort_order, width, height, file_size)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: UpsertPrimaryMediaAsset :one
-- Primary visual slots are singular. A newly discovered local sidecar replaces
-- the remote value, while a later remote refresh must not displace local data.
--
-- A selected remote identity may already exist in a labeled legacy/alternate
-- row. Remove that row before moving the identity into the primary slot, and
-- carry over its materialized bytes so the cache file does not become
-- orphaned. The delete is gated by the same local-data precedence rule as the
-- upsert: a rejected remote refresh must leave both the local primary and its
-- alternate candidate untouched.
WITH current_primary AS MATERIALIZED (
    SELECT media_assets.*
    FROM media_assets
    WHERE media_assets.media_item_id = sqlc.arg(media_item_id)
      AND media_assets.asset_type = sqlc.arg(asset_type)::asset_type
      AND media_assets.label = ''
      AND media_assets.asset_type IN ('poster', 'logo', 'art', 'banner', 'thumb', 'disc', 'clearart')
    FOR UPDATE
),
replacement_allowed AS MATERIALIZED (
    SELECT NOT EXISTS (SELECT 1 FROM current_primary)
        OR EXISTS (
            SELECT 1
            FROM current_primary
            WHERE current_primary.source <> 'local'
               OR sqlc.arg(source)::text = 'local'
               OR NOT EXISTS (
                    SELECT 1
                    FROM media_items
                    JOIN libraries ON libraries.id = media_items.library_id
                    WHERE media_items.id = current_primary.media_item_id
                      AND COALESCE((libraries.settings->>'use_local_data')::boolean, true)
               )
        ) AS allowed
),
preserved_remote AS MATERIALIZED (
    SELECT media_assets.*
    FROM media_assets
    CROSS JOIN replacement_allowed
    WHERE replacement_allowed.allowed
      AND sqlc.arg(local_path)::text = ''
      AND sqlc.arg(remote_url)::text <> ''
      AND media_assets.media_item_id = sqlc.arg(media_item_id)
      AND media_assets.asset_type = sqlc.arg(asset_type)::asset_type
      AND media_assets.remote_url = sqlc.arg(remote_url)
      AND CASE
            WHEN media_assets.asset_type = 'still'
              OR media_assets.label ~ '^season-[0-9]+$'
              OR media_assets.label ~ '^s[0-9]+e[0-9]+$'
                THEN media_assets.label
            ELSE ''
          END = ''
    ORDER BY (media_assets.local_path <> '') DESC, media_assets.id
    LIMIT 1
    FOR UPDATE OF media_assets
),
removed_conflicts AS (
    DELETE FROM media_assets
    USING replacement_allowed,
          (SELECT count(*) FROM preserved_remote) AS preservation_barrier
    WHERE replacement_allowed.allowed
      AND media_assets.media_item_id = sqlc.arg(media_item_id)
      AND media_assets.asset_type = sqlc.arg(asset_type)::asset_type
      AND media_assets.id <> COALESCE((SELECT id FROM current_primary), 0)
      AND CASE
            WHEN media_assets.asset_type = 'still'
              OR media_assets.label ~ '^season-[0-9]+$'
              OR media_assets.label ~ '^s[0-9]+e[0-9]+$'
                THEN media_assets.label
            ELSE ''
          END = ''
      AND (
          (sqlc.arg(local_path)::text <> '' AND media_assets.local_path = sqlc.arg(local_path))
          OR (sqlc.arg(remote_url)::text <> '' AND media_assets.remote_url = sqlc.arg(remote_url))
      )
    RETURNING media_assets.id
)
INSERT INTO media_assets (
    media_item_id, asset_type, source, local_path, remote_url, language, label,
    sort_order, width, height, file_size, score, likes, aspect, content_hash, visual_hash
)
SELECT
    sqlc.arg(media_item_id),
    sqlc.arg(asset_type)::asset_type,
    sqlc.arg(source),
    CASE
        WHEN sqlc.arg(local_path)::text <> '' THEN sqlc.arg(local_path)
        ELSE COALESCE((SELECT local_path FROM preserved_remote), '')
    END,
    sqlc.arg(remote_url),
    CASE
        WHEN sqlc.arg(language)::text <> '' THEN sqlc.arg(language)
        ELSE COALESCE((SELECT language FROM preserved_remote), '')
    END,
    '',
    0,
    CASE WHEN sqlc.arg(width)::integer > 0 THEN sqlc.arg(width) ELSE COALESCE((SELECT width FROM preserved_remote), 0) END,
    CASE WHEN sqlc.arg(height)::integer > 0 THEN sqlc.arg(height) ELSE COALESCE((SELECT height FROM preserved_remote), 0) END,
    CASE WHEN sqlc.arg(file_size)::bigint > 0 THEN sqlc.arg(file_size) ELSE COALESCE((SELECT file_size FROM preserved_remote), 0) END,
    COALESCE((SELECT score FROM preserved_remote), 0),
    COALESCE((SELECT likes FROM preserved_remote), 0),
    COALESCE((SELECT aspect FROM preserved_remote), ''),
    COALESCE((SELECT content_hash FROM preserved_remote), ''),
    COALESCE((SELECT visual_hash FROM preserved_remote), '')
FROM replacement_allowed
CROSS JOIN (SELECT count(*) FROM removed_conflicts) AS conflict_removal_barrier
WHERE replacement_allowed.allowed
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
    file_size = EXCLUDED.file_size,
    score = EXCLUDED.score,
    likes = EXCLUDED.likes,
    aspect = EXCLUDED.aspect,
    content_hash = EXCLUDED.content_hash,
    visual_hash = EXCLUDED.visual_hash
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

-- name: ListPendingRemoteMediaAssets :many
-- Visual asset rows whose bytes were never materialized locally — the warm
-- sweep pages these by id and enqueues downloads. Non-visual sidecar types
-- (subtitle/lyrics/nfo) never have remote bytes to warm.
SELECT media_assets.id, media_assets.media_item_id, media_assets.asset_type,
       media_assets.remote_url, media_assets.label, media_assets.sort_order,
       media_items.media_type
FROM media_assets
JOIN media_items ON media_items.id = media_assets.media_item_id
WHERE media_assets.local_path = ''
  AND media_assets.remote_url <> ''
  AND media_assets.asset_type IN ('poster', 'backdrop', 'logo', 'art', 'banner', 'thumb', 'disc', 'clearart', 'still')
  AND media_assets.id > $1
ORDER BY media_assets.id
LIMIT $2;

-- name: ListUnfingerprintedMediaAssets :many
-- Materialized artwork from older releases and scanner-discovered local
-- sidecars may predate image fingerprinting. The daily artwork reconciliation
-- pages these rows by id, computes their exact/perceptual hashes, and collapses
-- duplicates through the same path used by fresh downloads.
SELECT media_assets.*
FROM media_assets
WHERE media_assets.local_path <> ''
  AND media_assets.content_hash = ''
  AND media_assets.asset_type IN ('poster', 'backdrop', 'logo', 'art', 'banner', 'thumb', 'disc', 'clearart', 'still')
  AND media_assets.id > $1
ORDER BY media_assets.id
LIMIT $2;

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
