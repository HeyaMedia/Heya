-- name: CreateMediaTitle :exec
INSERT INTO media_titles (media_item_id, title, language, country, title_type, source)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (media_item_id, title, language) DO UPDATE SET
  country = EXCLUDED.country, title_type = EXCLUDED.title_type, source = EXCLUDED.source;

-- name: ListMediaTitles :many
SELECT * FROM media_titles WHERE media_item_id = $1 ORDER BY language, title_type;

-- name: GetMediaTitleByLanguage :one
SELECT * FROM media_titles
WHERE media_item_id = $1 AND language = $2 AND title_type = 'official'
LIMIT 1;

-- name: DeleteMediaTitlesByItem :exec
DELETE FROM media_titles WHERE media_item_id = $1;
