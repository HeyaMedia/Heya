-- Music station queries. Each station type returns the same rich track row
-- shape (matching SimilarTracksByTrackRichRow) so the FE can render every
-- station with one component. ORDER BY random() is fine here — n is small
-- (≤ 100) and we benefit from per-tap variety more than from cache locality.

-- name: ListRandomMusicTracks :many
-- Library Radio: N random tracks pulled from across the music library. Joins
-- the album+artist context so the FE renders self-contained rows.
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
       mi.slug           AS artist_slug
FROM tracks t
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
ORDER BY random()
LIMIT $1;

-- name: ListDeepCutsForUser :many
-- Deep Cuts: tracks the user has never played (or barely played). Sorted by
-- the user's play_count ascending (zero-play first), then randomized within
-- the bucket so back-to-back taps give different results.
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
       mi.slug           AS artist_slug
FROM tracks t
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
LEFT JOIN LATERAL (
    SELECT count(*) AS plays
    FROM play_events pe
    WHERE pe.track_id = t.id AND pe.user_id = sqlc.arg(user_id)
) up ON true
WHERE l.media_type = 'music'
ORDER BY coalesce(up.plays, 0) ASC, random()
LIMIT sqlc.arg(track_limit);

-- name: ListTracksByYearRange :many
-- Time Travel: random tracks whose album year falls within [min,max]
-- inclusive. Tracks with empty year strings are filtered out — they can't
-- be placed on a timeline. Treats year as text since that's how the column
-- is stored; the regex pattern catches 4-digit years.
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
       mi.slug           AS artist_slug
FROM tracks t
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
  AND al.year ~ '^[0-9]{4}'
  AND substring(al.year FROM 1 FOR 4)::int BETWEEN sqlc.arg(min_year)::int AND sqlc.arg(max_year)::int
ORDER BY random()
LIMIT sqlc.arg(track_limit);

-- name: PickRandomAlbumWithTracks :many
-- Random Album: every track of one random album, in album order. Two-CTE
-- approach so postgres picks the album once (random) and then lists its
-- tracks in disc/track order — versus a single random() pick which would
-- shuffle the tracks too.
WITH chosen AS (
    SELECT al.id
    FROM albums al
    JOIN artists     a  ON a.id  = al.artist_id
    JOIN media_items mi ON mi.id = a.media_item_id
    JOIN libraries   l  ON l.id  = mi.library_id
    WHERE l.media_type = 'music'
    ORDER BY random()
    LIMIT 1
)
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
       mi.slug           AS artist_slug
FROM tracks t
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
WHERE al.id = (SELECT id FROM chosen)
ORDER BY t.disc_number ASC, t.track_number ASC;
