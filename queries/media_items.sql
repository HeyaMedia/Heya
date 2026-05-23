-- name: CreateMediaItem :one
INSERT INTO media_items (library_id, media_type, title, sort_title, year, description, poster_path, backdrop_path, external_ids, tagline, original_title, original_language, status, provider_kind, heya_slug)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
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
    poster_path = $6, backdrop_path = $7, external_ids = $8,
    tagline = $9, original_title = $10, original_language = $11,
    status = $12, provider_kind = $13, heya_slug = $14, updated_at = now()
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

-- name: SearchMediaItemsByLibrary :many
SELECT * FROM media_items
WHERE library_id = $1
  AND ($4::text = '' OR title ILIKE '%' || $4 || '%')
ORDER BY sort_title ASC, title ASC
LIMIT $2 OFFSET $3;

-- name: UpdateMediaItemPosterPath :exec
UPDATE media_items SET poster_path = $2, updated_at = now() WHERE id = $1;

-- name: UpdateMediaItemBackdropPath :exec
UPDATE media_items SET backdrop_path = $2, updated_at = now() WHERE id = $1;

-- name: ListEnrichedMovies :many
SELECT mi.id, mi.library_id, mi.media_type, mi.title, mi.sort_title, mi.slug,
       mi.year, mi.description, mi.poster_path, mi.backdrop_path,
       mi.external_ids, mi.created_at, mi.updated_at,
       m.genres, m.rating, m.runtime_minutes, m.original_language,
       m.release_date, m.collection_id
FROM media_items mi
JOIN movies m ON m.media_item_id = mi.id
ORDER BY mi.sort_title ASC, mi.title ASC
LIMIT $1 OFFSET $2;

-- name: ListEnrichedTVSeries :many
SELECT mi.id, mi.library_id, mi.media_type, mi.title, mi.sort_title, mi.slug,
       mi.year, mi.description, mi.poster_path, mi.backdrop_path,
       mi.external_ids, mi.created_at, mi.updated_at,
       ts.genres, ts.rating, ts.first_air_date, ts.last_air_date,
       ts.status, ts.original_language, ts.number_of_seasons, ts.number_of_episodes
FROM media_items mi
JOIN tv_series ts ON ts.media_item_id = mi.id
ORDER BY mi.sort_title ASC, mi.title ASC
LIMIT $1 OFFSET $2;
