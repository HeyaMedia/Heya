-- name: CreateCollection :one
INSERT INTO collections (external_ids, name, overview, poster_path, backdrop_path)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: FindCollectionByName :one
SELECT * FROM collections WHERE name = $1 LIMIT 1;

-- name: UpdateCollection :exec
UPDATE collections
SET external_ids = $2, overview = $3, poster_path = $4, backdrop_path = $5
WHERE id = $1;

-- name: SetMovieCollection :exec
UPDATE movies SET collection_id = $2 WHERE media_item_id = $1;

-- name: GetCollectionByID :one
SELECT * FROM collections WHERE id = $1;

-- name: ListCollectionMovies :many
SELECT mi.*
FROM media_items mi
JOIN movies m ON m.media_item_id = mi.id
WHERE m.collection_id = $1
ORDER BY m.release_date NULLS LAST;

-- name: ListAllCollections :many
SELECT c.*,
       (SELECT count(*) FROM movies m WHERE m.collection_id = c.id)::int AS movie_count
FROM collections c
ORDER BY c.name
LIMIT $1 OFFSET $2;

-- name: CountAllCollections :one
SELECT count(*) FROM collections;

-- name: ListCollectionsWithLocalMedia :many
SELECT c.id, c.name, c.poster_path,
       count(m.id)::int AS movie_count
FROM collections c
JOIN movies m ON m.collection_id = c.id
GROUP BY c.id, c.name, c.poster_path
HAVING count(m.id) > 0
ORDER BY c.name;
