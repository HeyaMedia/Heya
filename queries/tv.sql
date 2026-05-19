-- name: CreateTVSeries :one
INSERT INTO tv_series (media_item_id, tmdb_id, imdb_id, status, genres, rating, first_air_date, last_air_date,
    original_name, original_language, networks, created_by, number_of_seasons, number_of_episodes,
    popularity, vote_count, cast_data)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
RETURNING *;

-- name: GetTVSeriesByMediaItemID :one
SELECT * FROM tv_series WHERE media_item_id = $1;

-- name: GetTVSeriesByTmdbID :one
SELECT * FROM tv_series WHERE tmdb_id = $1;

-- name: UpdateTVSeries :one
UPDATE tv_series
SET tmdb_id = $2, imdb_id = $3, status = $4, genres = $5,
    rating = $6, first_air_date = $7, last_air_date = $8,
    original_name = $9, original_language = $10, networks = $11, created_by = $12,
    number_of_seasons = $13, number_of_episodes = $14, popularity = $15, vote_count = $16, cast_data = $17
WHERE id = $1
RETURNING *;

-- name: CreateTVSeason :one
INSERT INTO tv_seasons (series_id, season_number, title, overview, poster_path, air_date)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListTVSeasonsBySeries :many
SELECT * FROM tv_seasons WHERE series_id = $1 ORDER BY season_number ASC;

-- name: GetTVSeason :one
SELECT * FROM tv_seasons WHERE series_id = $1 AND season_number = $2;

-- name: CreateTVEpisode :one
INSERT INTO tv_episodes (season_id, episode_number, title, overview, still_path, runtime_minutes, air_date)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListTVEpisodesBySeason :many
SELECT * FROM tv_episodes WHERE season_id = $1 ORDER BY episode_number ASC;

-- name: GetTVEpisode :one
SELECT * FROM tv_episodes WHERE season_id = $1 AND episode_number = $2;

-- name: UpdateTVEpisode :one
UPDATE tv_episodes
SET title = $2, overview = $3, still_path = $4, runtime_minutes = $5, air_date = $6
WHERE id = $1
RETURNING *;
