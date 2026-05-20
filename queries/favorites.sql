-- name: ToggleFavorite :one
INSERT INTO user_favorites (user_id, entity_type, entity_id)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, entity_type, entity_id) DO NOTHING
RETURNING *;

-- name: RemoveFavorite :exec
DELETE FROM user_favorites
WHERE user_id = $1 AND entity_type = $2 AND entity_id = $3;

-- name: IsFavorited :one
SELECT EXISTS(
  SELECT 1 FROM user_favorites
  WHERE user_id = $1 AND entity_type = $2 AND entity_id = $3
) AS favorited;

-- name: ListUserFavoriteMediaItems :many
SELECT mi.*
FROM media_items mi
JOIN user_favorites uf ON uf.entity_id = mi.id AND uf.entity_type = 'media_item'
WHERE uf.user_id = $1
ORDER BY uf.created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListFavoritesByEntity :many
SELECT * FROM user_favorites
WHERE user_id = $1 AND entity_type = $2
ORDER BY created_at DESC;
