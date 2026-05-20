-- name: ListAllGenres :many
SELECT genre, count(*) AS count FROM (
  SELECT unnest(genres) AS genre FROM movies
  UNION ALL
  SELECT unnest(genres) AS genre FROM tv_series
) sub
GROUP BY genre
ORDER BY genre;

-- name: ListMediaByGenre :many
SELECT mi.*
FROM media_items mi
LEFT JOIN movies m ON m.media_item_id = mi.id
LEFT JOIN tv_series ts ON ts.media_item_id = mi.id
WHERE ($1::text = ANY(m.genres) OR $1::text = ANY(ts.genres))
ORDER BY mi.sort_title ASC, mi.title ASC
LIMIT $2 OFFSET $3;

-- name: CountMediaByGenre :one
SELECT count(*)
FROM media_items mi
LEFT JOIN movies m ON m.media_item_id = mi.id
LEFT JOIN tv_series ts ON ts.media_item_id = mi.id
WHERE ($1::text = ANY(m.genres) OR $1::text = ANY(ts.genres));

-- name: ListMediaByKeyword :many
SELECT mi.*
FROM media_items mi
JOIN media_keywords mk ON mk.media_item_id = mi.id
JOIN keywords k ON k.id = mk.keyword_id
WHERE lower(k.name) = lower($1::text)
ORDER BY mi.sort_title ASC, mi.title ASC
LIMIT $2 OFFSET $3;

-- name: CountMediaByKeyword :one
SELECT count(*)
FROM media_items mi
JOIN media_keywords mk ON mk.media_item_id = mi.id
JOIN keywords k ON k.id = mk.keyword_id
WHERE lower(k.name) = lower($1::text);
