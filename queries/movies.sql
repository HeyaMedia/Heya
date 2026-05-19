-- name: CreateMovie :one
INSERT INTO movies (media_item_id, tmdb_id, imdb_id, runtime_minutes, tagline, genres, rating, release_date,
    original_title, original_language, budget, revenue, popularity, vote_count, production_companies, cast_data, crew_data)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
RETURNING *;

-- name: GetMovieByMediaItemID :one
SELECT * FROM movies WHERE media_item_id = $1;

-- name: GetMovieByTmdbID :one
SELECT * FROM movies WHERE tmdb_id = $1;

-- name: UpdateMovie :one
UPDATE movies
SET tmdb_id = $2, imdb_id = $3, runtime_minutes = $4, tagline = $5,
    genres = $6, rating = $7, release_date = $8,
    original_title = $9, original_language = $10, budget = $11, revenue = $12,
    popularity = $13, vote_count = $14, production_companies = $15, cast_data = $16, crew_data = $17
WHERE id = $1
RETURNING *;

-- name: DeleteMovie :exec
DELETE FROM movies WHERE id = $1;
