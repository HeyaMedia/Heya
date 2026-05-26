-- Per-user track ratings. UI displays 5 stars in half-step increments;
-- the column stores 1..10 so half-stars round-trip cleanly.

-- name: SetUserTrackRating :exec
-- Upsert: writes a rating or replaces the existing one. Touch updated_at
-- on conflict so "recently rated" ordering reflects last edit.
INSERT INTO user_track_ratings (user_id, track_id, rating)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, track_id) DO UPDATE
    SET rating = EXCLUDED.rating,
        updated_at = now();

-- name: DeleteUserTrackRating :exec
DELETE FROM user_track_ratings
WHERE user_id = $1 AND track_id = $2;

-- name: GetUserTrackRating :one
SELECT rating FROM user_track_ratings
WHERE user_id = $1 AND track_id = $2;

-- name: GetUserTrackRatingsForTrackIDs :many
-- Bulk lookup for a track-list view — returns (track_id, rating) for the
-- subset of input ids the user has rated. Driver passes the rest as nulls
-- in its merge.
SELECT track_id, rating
FROM user_track_ratings
WHERE user_id = $1 AND track_id = ANY($2::bigint[]);

-- name: ListUserRatedTracks :many
-- Paginated list of every track the user has rated, sorted by rating
-- desc then most-recently-rated. Carries album+artist context so the FE
-- renders self-contained rows. Filters by min_rating for the Favorites
-- view (use 1 to get everything rated).
SELECT t.id              AS track_id,
       t.title           AS track_title,
       t.duration        AS duration,
       t.disc_number     AS disc_number,
       t.track_number    AS track_number,
       al.id             AS album_id,
       al.title          AS album_title,
       al.slug           AS album_slug,
       al.cover_path     AS album_cover_path,
       al.year           AS album_year,
       a.id              AS artist_id,
       a.name            AS artist_name,
       mi.slug           AS artist_slug,
       utr.rating        AS rating,
       utr.updated_at    AS rated_at
FROM user_track_ratings utr
JOIN tracks      t  ON t.id  = utr.track_id
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
WHERE utr.user_id = sqlc.arg(user_id)
  AND utr.rating  >= sqlc.arg(min_rating)
ORDER BY utr.rating DESC, utr.updated_at DESC
LIMIT sqlc.arg(track_limit) OFFSET sqlc.arg(offset_);

-- name: CountUserRatedTracks :one
SELECT count(*) FROM user_track_ratings
WHERE user_id = $1 AND rating >= $2;

-- name: GetUserFavoritesThreshold :one
SELECT favorites_threshold FROM users WHERE id = $1;

-- name: UpdateUserFavoritesThreshold :exec
UPDATE users SET favorites_threshold = $2 WHERE id = $1;
