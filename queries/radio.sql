-- name: ListRadioFavorites :many
-- A user's favorited stations, newest first.
SELECT * FROM user_radio_favorites
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: AddRadioFavorite :one
-- Upsert: if the station is already a favorite, refresh the cached
-- metadata (the upstream values change as stations re-tag themselves).
INSERT INTO user_radio_favorites (
    user_id, stationuuid, name, url, favicon, homepage,
    country, countrycode, language, tags, codec, bitrate
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (user_id, stationuuid) DO UPDATE SET
    name        = EXCLUDED.name,
    url         = EXCLUDED.url,
    favicon     = EXCLUDED.favicon,
    homepage    = EXCLUDED.homepage,
    country     = EXCLUDED.country,
    countrycode = EXCLUDED.countrycode,
    language    = EXCLUDED.language,
    tags        = EXCLUDED.tags,
    codec       = EXCLUDED.codec,
    bitrate     = EXCLUDED.bitrate
RETURNING *;

-- name: RemoveRadioFavorite :exec
DELETE FROM user_radio_favorites WHERE user_id = $1 AND stationuuid = $2;

-- name: IsRadioFavorited :one
SELECT EXISTS(
    SELECT 1 FROM user_radio_favorites WHERE user_id = $1 AND stationuuid = $2
)::bool AS favorited;

-- name: ListRadioRecents :many
-- Deduped recent-plays. Same DISTINCT ON dance as recently-played tracks
-- so a station looped all morning shows up once with its freshest timestamp.
WITH dedup AS (
    SELECT DISTINCT ON (stationuuid)
           id, user_id, stationuuid, name, url, favicon, country,
           tags, codec, bitrate, played_at
    FROM user_radio_recents
    WHERE user_id = $1
    ORDER BY stationuuid, played_at DESC
)
SELECT * FROM dedup
ORDER BY played_at DESC
LIMIT sqlc.arg(track_limit);

-- name: RecordRadioPlay :one
-- Append-only history log. Pruning is handled by a periodic vacuum query
-- below; cheap inserts during playback are more important than cap policy.
INSERT INTO user_radio_recents (
    user_id, stationuuid, name, url, favicon, country, tags, codec, bitrate
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: PruneRadioRecents :exec
-- Caps recents per user to keep the table bounded. Runs from the same
-- vacuum scheduler that prunes play_events later.
DELETE FROM user_radio_recents
WHERE id IN (
    SELECT r.id FROM user_radio_recents r
    WHERE r.user_id = sqlc.arg(user_id)
    ORDER BY r.played_at DESC
    OFFSET sqlc.arg(keep_count)
);
