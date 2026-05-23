-- name: CreateEpisodeOverview :exec
INSERT INTO episode_overviews (episode_id, language, overview)
VALUES ($1, $2, $3)
ON CONFLICT (episode_id, language) DO UPDATE SET overview = EXCLUDED.overview;

-- name: ListEpisodeOverviews :many
SELECT * FROM episode_overviews WHERE episode_id = $1 ORDER BY language;

-- name: GetEpisodeOverviewByLanguage :one
SELECT * FROM episode_overviews WHERE episode_id = $1 AND language = $2;

-- name: DeleteEpisodeOverviews :exec
DELETE FROM episode_overviews WHERE episode_id = $1;
