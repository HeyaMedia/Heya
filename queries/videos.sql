-- name: CreateMediaVideo :exec
INSERT INTO media_videos (media_item_id, provider_key, name, site, video_key, video_type, language, official, published_at, description)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (media_item_id, video_key) DO UPDATE SET
  name = EXCLUDED.name,
  video_type = EXCLUDED.video_type,
  official = EXCLUDED.official,
  description = EXCLUDED.description;

-- name: DeleteMediaVideosByItem :exec
DELETE FROM media_videos WHERE media_item_id = $1;

-- name: ListMediaVideos :many
SELECT * FROM media_videos WHERE media_item_id = $1 ORDER BY video_type, name;
