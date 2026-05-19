-- name: CreateMediaCertification :exec
INSERT INTO media_certifications (media_item_id, country, certification, release_date, release_type)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (media_item_id, country, release_type) DO UPDATE SET
  certification = EXCLUDED.certification,
  release_date = EXCLUDED.release_date;

-- name: ListMediaCertifications :many
SELECT * FROM media_certifications WHERE media_item_id = $1 ORDER BY country;

-- name: GetMediaCertification :one
SELECT * FROM media_certifications WHERE media_item_id = $1 AND country = $2 LIMIT 1;
