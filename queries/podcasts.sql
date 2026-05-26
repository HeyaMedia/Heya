-- name: ListPodcastSubscriptions :many
-- A user's subscribed feeds, newest-first.
SELECT * FROM user_podcast_subscriptions
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: AddPodcastSubscription :one
-- Upsert: re-subscribing refreshes the cached title/author/artwork rather
-- than failing on the unique constraint.
INSERT INTO user_podcast_subscriptions (
    user_id, feed_url, title, author, artwork_url
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, feed_url) DO UPDATE SET
    title       = EXCLUDED.title,
    author      = EXCLUDED.author,
    artwork_url = EXCLUDED.artwork_url
RETURNING *;

-- name: RemovePodcastSubscription :exec
DELETE FROM user_podcast_subscriptions
WHERE user_id = $1 AND feed_url = $2;

-- name: IsPodcastSubscribed :one
SELECT EXISTS(
    SELECT 1 FROM user_podcast_subscriptions
    WHERE user_id = $1 AND feed_url = $2
)::bool AS subscribed;

-- name: UpsertPodcastProgress :one
-- One row per (user, feed, episode) — same shape as user_watch_progress
-- but for podcast episodes. Caller computes `completed` so the FE can mark
-- an episode "done" whenever it wants (e.g. user hits the next button at
-- the outro). progress_seconds=0 + completed=true means "I've heard this"
-- without a position to resume from.
INSERT INTO user_podcast_progress (
    user_id, feed_url, episode_guid, title, artwork_url, audio_url,
    progress_seconds, total_seconds, completed, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
ON CONFLICT (user_id, feed_url, episode_guid) DO UPDATE SET
    title            = EXCLUDED.title,
    artwork_url      = EXCLUDED.artwork_url,
    audio_url        = EXCLUDED.audio_url,
    progress_seconds = EXCLUDED.progress_seconds,
    total_seconds    = EXCLUDED.total_seconds,
    completed        = EXCLUDED.completed,
    updated_at       = now()
RETURNING *;

-- name: ListPodcastContinue :many
-- "Continue listening" — episodes the user started but didn't finish,
-- newest activity first. Same indexed predicate as user_watch_progress
-- so the partial index handles the filter cheaply.
SELECT *
FROM user_podcast_progress
WHERE user_id = $1 AND completed = false AND progress_seconds > 0
ORDER BY updated_at DESC
LIMIT sqlc.arg(track_limit);

-- name: GetPodcastEpisodeProgress :one
SELECT * FROM user_podcast_progress
WHERE user_id = $1 AND feed_url = $2 AND episode_guid = $3;
