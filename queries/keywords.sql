-- name: FindKeywordByName :one
SELECT * FROM keywords WHERE name = $1;

-- name: CreateKeyword :one
INSERT INTO keywords (external_ids, name) VALUES ($1, $2) RETURNING *;

-- name: LinkMediaKeyword :exec
INSERT INTO media_keywords (media_item_id, keyword_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: DeleteMediaKeywordsByItem :exec
DELETE FROM media_keywords WHERE media_item_id = $1;

-- name: ListMediaKeywords :many
SELECT k.* FROM keywords k
JOIN media_keywords mk ON mk.keyword_id = k.id
WHERE mk.media_item_id = $1
ORDER BY k.name;
