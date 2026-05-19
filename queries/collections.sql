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
