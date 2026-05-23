-- name: CreateMediaOverview :exec
INSERT INTO media_overviews (media_item_id, language, overview)
VALUES ($1, $2, $3)
ON CONFLICT (media_item_id, language) DO UPDATE SET overview = EXCLUDED.overview;

-- name: ListMediaOverviews :many
SELECT * FROM media_overviews WHERE media_item_id = $1 ORDER BY language;

-- name: GetMediaOverview :one
SELECT * FROM media_overviews WHERE media_item_id = $1 AND language = $2;

-- name: DeleteMediaOverviewsByItem :exec
DELETE FROM media_overviews WHERE media_item_id = $1;
