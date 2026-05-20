-- name: CreateCollection :one
INSERT INTO collections (tmdb_id, name, overview, poster_path, backdrop_path)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (tmdb_id) DO UPDATE SET
  name = EXCLUDED.name,
  overview = EXCLUDED.overview,
  poster_path = EXCLUDED.poster_path,
  backdrop_path = EXCLUDED.backdrop_path
RETURNING *;

-- name: GetCollectionByTmdbID :one
SELECT * FROM collections WHERE tmdb_id = $1;

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
