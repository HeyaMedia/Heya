-- name: CreateMediaExtra :one
INSERT INTO media_extras (media_item_id, extra_type, title, file_path, duration_ms, file_size)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (media_item_id, extra_type, title) DO NOTHING
RETURNING *;

-- name: ListMediaExtras :many
SELECT * FROM media_extras
WHERE media_item_id = $1
ORDER BY extra_type, title;

-- name: ListMediaExtrasByType :many
SELECT * FROM media_extras
WHERE media_item_id = $1 AND extra_type = $2
ORDER BY title;

-- name: DeleteMediaExtrasByItem :exec
DELETE FROM media_extras WHERE media_item_id = $1;
