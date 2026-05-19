-- name: CreateKeyword :one
INSERT INTO keywords (tmdb_id, name)
VALUES ($1, $2)
ON CONFLICT (tmdb_id) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: LinkMediaKeyword :exec
INSERT INTO media_keywords (media_item_id, keyword_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ListMediaKeywords :many
SELECT k.* FROM keywords k
JOIN media_keywords mk ON mk.keyword_id = k.id
WHERE mk.media_item_id = $1
ORDER BY k.name;
