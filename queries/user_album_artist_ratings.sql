-- Album + artist rating CRUD. Same shape as user_track_ratings — separate
-- tables (rather than polymorphic) so each FK constraint can cascade
-- correctly when the parent row is deleted.

-- ============== Albums ==============

-- name: SetUserAlbumRating :exec
INSERT INTO user_album_ratings (user_id, album_id, rating)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, album_id) DO UPDATE
    SET rating = EXCLUDED.rating,
        updated_at = now();

-- name: DeleteUserAlbumRating :exec
DELETE FROM user_album_ratings
WHERE user_id = $1 AND album_id = $2;

-- name: GetUserAlbumRating :one
SELECT rating FROM user_album_ratings
WHERE user_id = $1 AND album_id = $2;

-- name: GetUserAlbumRatingsForIDs :many
SELECT album_id, rating
FROM user_album_ratings
WHERE user_id = $1 AND album_id = ANY($2::bigint[]);

-- name: ListUserRatedAlbums :many
-- Paginated rated albums. Carries artist context so the FE renders rows
-- without follow-up lookups.
SELECT al.*,
       a.name        AS artist_name,
       mi.slug       AS artist_slug,
       uar.rating    AS rating,
       uar.updated_at AS rated_at
FROM user_album_ratings uar
JOIN albums      al ON al.id = uar.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE uar.user_id = sqlc.arg(user_id)
  AND uar.rating  >= sqlc.arg(min_rating)
ORDER BY uar.rating DESC, uar.updated_at DESC
LIMIT sqlc.arg(album_limit) OFFSET sqlc.arg(offset_);

-- name: CountUserRatedAlbums :one
SELECT count(*) FROM user_album_ratings
WHERE user_id = $1 AND rating >= $2;

-- ============== Artists ==============

-- name: SetUserArtistRating :exec
INSERT INTO user_artist_ratings (user_id, artist_id, rating)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, artist_id) DO UPDATE
    SET rating = EXCLUDED.rating,
        updated_at = now();

-- name: DeleteUserArtistRating :exec
DELETE FROM user_artist_ratings
WHERE user_id = $1 AND artist_id = $2;

-- name: GetUserArtistRating :one
SELECT rating FROM user_artist_ratings
WHERE user_id = $1 AND artist_id = $2;

-- name: GetUserArtistRatingsForIDs :many
SELECT artist_id, rating
FROM user_artist_ratings
WHERE user_id = $1 AND artist_id = ANY($2::bigint[]);

-- name: ListUserRatedArtists :many
-- Paginated rated artists with album/track counts so the tile matches the
-- shape of /api/me/loved/artists (drop-in replacement for that endpoint).
SELECT a.*,
       mi.slug         AS slug,
       mi.public_id    AS media_item_public_id,
       mi.poster_path  AS poster_path,
       (SELECT count(*) FROM albums al WHERE al.artist_id = a.id) AS album_count,
       (SELECT count(*) FROM tracks t JOIN albums al ON al.id = t.album_id WHERE al.artist_id = a.id) AS track_count,
       uar.rating      AS rating,
       uar.updated_at  AS rated_at
FROM user_artist_ratings uar
JOIN artists     a  ON a.id  = uar.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE uar.user_id = sqlc.arg(user_id)
  AND uar.rating  >= sqlc.arg(min_rating)
ORDER BY uar.rating DESC, uar.updated_at DESC
LIMIT sqlc.arg(artist_limit) OFFSET sqlc.arg(offset_);

-- name: CountUserRatedArtists :one
SELECT count(*) FROM user_artist_ratings
WHERE user_id = $1 AND rating >= $2;
