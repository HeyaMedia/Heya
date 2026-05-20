-- name: UpsertExternalRating :one
INSERT INTO external_ratings (media_item_id, source, value, score)
VALUES ($1, $2, $3, $4)
ON CONFLICT (media_item_id, source) DO UPDATE SET value = $3, score = $4
RETURNING *;

-- name: ListExternalRatings :many
SELECT * FROM external_ratings WHERE media_item_id = $1;

-- name: DeleteExternalRatings :exec
DELETE FROM external_ratings WHERE media_item_id = $1;
