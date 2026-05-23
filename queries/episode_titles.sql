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
