-- name: ToggleFavorite :one
INSERT INTO user_favorites (user_id, entity_type, entity_id)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, entity_type, entity_id) DO NOTHING
RETURNING *;

-- name: RemoveFavorite :exec
DELETE FROM user_favorites
WHERE user_id = $1 AND entity_type = $2 AND entity_id = $3;

-- name: IsFavorited :one
SELECT EXISTS(
  SELECT 1 FROM user_favorites
  WHERE user_id = $1 AND entity_type = $2 AND entity_id = $3
) AS favorited;

-- name: ListUserFavoriteMediaItems :many
SELECT mi.*
FROM media_items mi
JOIN user_favorites uf ON uf.entity_id = mi.id AND uf.entity_type = 'media_item'
WHERE uf.user_id = $1
ORDER BY uf.created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListFavoritesByEntity :many
SELECT * FROM user_favorites
WHERE user_id = $1 AND entity_type = $2
ORDER BY created_at DESC;

-- name: ListUserLovedTracks :many
-- Flat list of the user's loved tracks across every music library, joined
-- with the album + artist for one-shot rendering on the Loved tab.
SELECT t.id           AS track_id,
       t.title        AS track_title,
       t.duration     AS duration,
       t.disc_number  AS disc_number,
       t.track_number AS track_number,
       al.id          AS album_id,
       al.title       AS album_title,
       al.cover_path  AS album_cover_path,
       al.year        AS album_year,
       al.slug        AS album_slug,
       a.id           AS artist_id,
       a.name         AS artist_name,
       mi.slug        AS artist_slug,
       uf.created_at  AS loved_at
FROM user_favorites uf
JOIN tracks      t  ON t.id  = uf.entity_id
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
WHERE uf.user_id = $1 AND uf.entity_type = 'track'
ORDER BY uf.created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountUserLovedTracks :one
SELECT count(*) FROM user_favorites WHERE user_id = $1 AND entity_type = 'track';

-- name: ListUserLovedTrackIDs :many
-- Compact set of just the IDs so the UI can mark hearts filled on whatever
-- track rows it's currently rendering. Caller filters client-side.
SELECT entity_id AS track_id
FROM user_favorites
WHERE user_id = $1 AND entity_type = 'track';

-- name: ListUserLovedArtistIDs :many
SELECT entity_id AS artist_id
FROM user_favorites
WHERE user_id = $1 AND entity_type = 'artist';

-- name: ListUserLovedAlbumIDs :many
SELECT entity_id AS album_id
FROM user_favorites
WHERE user_id = $1 AND entity_type = 'album';

-- name: ListUserLovedArtists :many
-- The user's favorited artists, with poster + counts, ordered most-recently
-- loved first. Mirrors the shape of ListMusicArtists so the UI can reuse the
-- same tile component.
SELECT a.*,
       mi.slug         AS slug,
       mi.poster_path  AS poster_path,
       (SELECT count(*) FROM albums al WHERE al.artist_id = a.id) AS album_count,
       (SELECT count(*) FROM tracks t JOIN albums al ON al.id = t.album_id WHERE al.artist_id = a.id) AS track_count,
       uf.created_at AS loved_at
FROM user_favorites uf
JOIN artists     a  ON a.id  = uf.entity_id
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE uf.user_id = $1 AND uf.entity_type = 'artist' AND l.media_type = 'music'
ORDER BY uf.created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountUserLovedArtists :one
SELECT count(*) FROM user_favorites
WHERE user_id = $1 AND entity_type = 'artist';

-- name: ListUserLovedAlbums :many
-- The user's favorited albums with artist join, ordered most-recently loved.
SELECT al.*,
       a.name           AS artist_name,
       mi.slug          AS artist_slug,
       (SELECT count(*) FROM tracks t WHERE t.album_id = al.id) AS track_count,
       uf.created_at    AS loved_at
FROM user_favorites uf
JOIN albums      al ON al.id = uf.entity_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE uf.user_id = $1 AND uf.entity_type = 'album' AND l.media_type = 'music'
ORDER BY uf.created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountUserLovedAlbums :one
SELECT count(*) FROM user_favorites
WHERE user_id = $1 AND entity_type = 'album';
