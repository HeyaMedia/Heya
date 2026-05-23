-- name: GetPlaybackPreference :one
SELECT * FROM user_playback_preferences
WHERE user_id = $1 AND media_item_id = $2;

-- name: UpsertPlaybackPreference :one
INSERT INTO user_playback_preferences (user_id, media_item_id, audio_language, subtitle_language, subtitle_mode, updated_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (user_id, media_item_id)
DO UPDATE SET
  audio_language = EXCLUDED.audio_language,
  subtitle_language = EXCLUDED.subtitle_language,
  subtitle_mode = EXCLUDED.subtitle_mode,
  updated_at = now()
RETURNING *;

-- name: DeletePlaybackPreference :exec
DELETE FROM user_playback_preferences
WHERE user_id = $1 AND media_item_id = $2;

-- name: ListPlaybackPreferences :many
SELECT * FROM user_playback_preferences
WHERE user_id = $1
ORDER BY updated_at DESC;
