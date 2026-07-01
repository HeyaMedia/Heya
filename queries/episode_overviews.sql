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

-- name: ListEpisodeOverviewsForSeries :many
-- Batched detail-page fetch — see ListEpisodeTitlesForSeries.
SELECT eo.* FROM episode_overviews eo
JOIN tv_episodes e ON e.id = eo.episode_id
JOIN tv_seasons s ON s.id = e.season_id
WHERE s.series_id = $1 AND eo.language = ANY(@languages::text[]);
