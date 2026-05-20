-- name: CreateMediaItem :one
INSERT INTO media_items (library_id, media_type, title, sort_title, year, description, poster_path, backdrop_path, external_ids)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetMediaItemByID :one
SELECT * FROM media_items WHERE id = $1;

-- name: GetMediaItemBySlug :one
SELECT * FROM media_items WHERE slug = $1;

-- name: UpdateMediaItemSlug :exec
UPDATE media_items SET slug = $2 WHERE id = $1;

-- name: MediaItemSlugExists :one
SELECT EXISTS(SELECT 1 FROM media_items WHERE slug = $1 AND id != $2) as exists;

-- name: ListMediaItemsByLibrary :many
SELECT * FROM media_items
WHERE library_id = $1
ORDER BY sort_title ASC, title ASC
LIMIT $2 OFFSET $3;

-- name: ListMediaItemsByType :many
SELECT * FROM media_items
WHERE media_type = $1
ORDER BY sort_title ASC, title ASC
LIMIT $2 OFFSET $3;

-- name: UpdateMediaItem :one
UPDATE media_items
SET title = $2, sort_title = $3, year = $4, description = $5,
    poster_path = $6, backdrop_path = $7, external_ids = $8, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteMediaItem :exec
DELETE FROM media_items WHERE id = $1;

-- name: CountMediaItemsByLibrary :one
SELECT count(*) FROM media_items WHERE library_id = $1;

-- name: CountMediaItemsByType :one
SELECT count(*) FROM media_items WHERE media_type = $1;

-- name: MarkMetadataRefreshed :exec
UPDATE media_items SET metadata_refreshed_at = now() WHERE id = $1;

-- name: ListUnavailableMediaItemIDs :many
SELECT DISTINCT mi.id
FROM media_items mi
WHERE mi.media_type = $1
  AND NOT EXISTS (
    SELECT 1 FROM library_files lf
    WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL
  );
