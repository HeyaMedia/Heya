-- Per-show watched: total episodes vs watched episodes, grouped by media_item_id
-- name: ListShowWatchCounts :many
SELECT ts.media_item_id,
       count(e.id)::int AS total_episodes,
       count(wp.entity_id)::int AS watched_episodes
FROM tv_series ts
JOIN tv_seasons s ON s.series_id = ts.id
JOIN tv_episodes e ON e.season_id = s.id
LEFT JOIN user_watch_progress wp ON wp.entity_id = e.id AND wp.entity_type = 'episode' AND wp.completed = true AND wp.user_id = $1
GROUP BY ts.media_item_id;

-- Per-season watched: total vs watched, grouped by season
-- name: ListSeasonWatchCounts :many
SELECT s.id AS season_id,
       s.series_id,
       count(e.id)::int AS total_episodes,
       count(wp.entity_id)::int AS watched_episodes
FROM tv_seasons s
JOIN tv_episodes e ON e.season_id = s.id
LEFT JOIN user_watch_progress wp ON wp.entity_id = e.id AND wp.entity_type = 'episode' AND wp.completed = true AND wp.user_id = $1
WHERE s.series_id = $2
GROUP BY s.id, s.series_id;

-- Per-episode watched IDs for a single series
-- name: ListWatchedEpisodeIDsForSeries :many
SELECT wp.entity_id AS episode_id
FROM user_watch_progress wp
JOIN tv_episodes e ON e.id = wp.entity_id
JOIN tv_seasons s ON s.id = e.season_id
WHERE wp.user_id = $1 AND wp.entity_type = 'episode' AND wp.completed = true AND s.series_id = $2;

-- All favorited entity IDs by type
-- name: ListFavoritedIDs :many
SELECT entity_id FROM user_favorites
WHERE user_id = $1 AND entity_type = $2;

-- Watched movie IDs
-- name: ListWatchedMovieIDs :many
SELECT entity_id AS media_item_id FROM user_watch_progress
WHERE user_id = $1 AND entity_type = 'movie' AND completed = true;
