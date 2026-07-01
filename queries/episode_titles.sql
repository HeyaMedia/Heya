-- name: CreateEpisodeTitle :exec
INSERT INTO episode_titles (episode_id, title, language, source)
VALUES ($1, $2, $3, $4)
ON CONFLICT (episode_id, language) DO UPDATE SET title = EXCLUDED.title, source = EXCLUDED.source;

-- name: ListEpisodeTitles :many
SELECT * FROM episode_titles WHERE episode_id = $1 ORDER BY language;

-- name: GetEpisodeTitleByLanguage :one
SELECT * FROM episode_titles WHERE episode_id = $1 AND language = $2;

-- name: DeleteEpisodeTitles :exec
DELETE FROM episode_titles WHERE episode_id = $1;

-- name: ListEpisodeTitlesForSeries :many
-- Batched detail-page fetch: every episode title for a series in the wanted
-- languages, one query instead of 1-2 GetEpisodeTitleByLanguage per episode.
SELECT et.* FROM episode_titles et
JOIN tv_episodes e ON e.id = et.episode_id
JOIN tv_seasons s ON s.id = e.season_id
WHERE s.series_id = $1 AND et.language = ANY(@languages::text[]);
