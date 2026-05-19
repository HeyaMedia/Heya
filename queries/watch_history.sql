-- name: CreateWatchHistory :one
INSERT INTO watch_history (user_id, media_item_id, progress_seconds, total_seconds, completed)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateWatchProgress :one
UPDATE watch_history
SET progress_seconds = $2, total_seconds = $3, completed = $4, watched_at = now()
WHERE id = $1
RETURNING *;

-- name: GetLatestWatchHistory :one
SELECT * FROM watch_history
WHERE user_id = $1 AND media_item_id = $2
ORDER BY watched_at DESC
LIMIT 1;

-- name: ListContinueWatching :many
SELECT DISTINCT ON (wh.media_item_id)
    wh.*
FROM watch_history wh
WHERE wh.user_id = $1 AND wh.completed = false AND wh.progress_seconds > 0
ORDER BY wh.media_item_id, wh.watched_at DESC;

-- name: ListWatchHistoryByUser :many
SELECT * FROM watch_history
WHERE user_id = $1
ORDER BY watched_at DESC
LIMIT $2 OFFSET $3;
