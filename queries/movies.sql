-- name: CreateMovie :one
INSERT INTO movies (media_item_id, runtime_minutes, tagline, genres, rating, release_date,
    original_title, original_language, budget, revenue, popularity, spoken_languages, origin_country)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (media_item_id) DO NOTHING
RETURNING *;

-- name: GetMovieByMediaItemID :one
SELECT * FROM movies WHERE media_item_id = $1;

-- name: UpdateMovie :one
UPDATE movies
SET runtime_minutes = $2, tagline = $3,
    genres = $4, rating = $5, release_date = $6,
    original_title = $7, original_language = $8, budget = $9, revenue = $10,
    popularity = $11, spoken_languages = $12, origin_country = $13
WHERE id = $1
RETURNING *;

-- name: DeleteMovie :exec
DELETE FROM movies WHERE id = $1;
