-- Music station queries. Each station type returns the same rich track row
-- shape (matching SimilarTracksByTrackRichRow) so the FE can render every
-- station with one component. ORDER BY random() is fine here — n is small
-- (≤ 100) and we benefit from per-tap variety more than from cache locality.

-- name: ListRandomMusicTracks :many
-- Library Radio: N random tracks pulled from across the music library. Joins
-- the album+artist context so the FE renders self-contained rows.
-- Sample-first: TABLESAMPLE SYSTEM (2) yields ~4.8k candidate tracks on a
-- 240k-track library, so the join+shuffle touches thousands of rows instead
-- of materializing the whole library (measured 463ms -> 6ms). Callers MUST
-- fall back to ListRandomMusicTracksFull when fewer than limit rows come
-- back — on a tiny library the 2% page sample can be empty.
WITH cand AS (
    SELECT t.id
    FROM tracks t TABLESAMPLE SYSTEM (2)
    ORDER BY random()
    LIMIT $1
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
FROM cand
JOIN tracks t ON t.id = cand.id
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
ORDER BY random();

-- name: ListRandomMusicTracksFull :many
-- Fallback for ListRandomMusicTracks on small libraries: the original
-- materialize-everything shape — cheap exactly when the sample under-fills.
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
JOIN media_item_cards mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
ORDER BY random()
LIMIT $1;

-- name: ListDeepCutsForUser :many
-- Deep Cuts: random never-played tracks, sample-first. TABLESAMPLE SYSTEM (2)
-- yields ~4.8k candidate tracks on a 240k-track library; the NOT EXISTS
-- anti-join against the user's play_events filters played ones. The original
-- per-track lateral count(*) ran 240k probes per tap (~1s); this measures
-- ~11ms. Callers MUST fall back to ListDeepCutsForUserFull when fewer than
-- limit rows come back (small library: sample can be empty; fully-played
-- library: the anti-join empties it).
WITH cand AS (
    SELECT t.id
    FROM tracks t TABLESAMPLE SYSTEM (2)
    WHERE NOT EXISTS (
        SELECT 1 FROM play_events pe
        WHERE pe.track_id = t.id AND pe.user_id = sqlc.arg(user_id) AND pe.completed
    )
    ORDER BY random()
    LIMIT sqlc.arg(track_limit)
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
FROM cand
JOIN tracks t ON t.id = cand.id
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
ORDER BY random();

-- name: ListDeepCutsForUserFull :many
-- Fallback for ListDeepCutsForUser: the original fewest-plays-first shape,
-- used when the sampled variant under-fills.
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
JOIN media_item_cards mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
LEFT JOIN LATERAL (
    SELECT count(*) AS plays
    FROM play_events pe
    WHERE pe.track_id = t.id AND pe.user_id = sqlc.arg(user_id) AND pe.completed
) up ON true
WHERE l.media_type = 'music'
ORDER BY coalesce(up.plays, 0) ASC, random()
LIMIT sqlc.arg(track_limit);

-- name: ListTracksByYearRange :many
-- Time Travel: random tracks whose album year falls within [min,max]
-- inclusive. Treats year as text since that's how the column is stored; the
-- regex pattern catches 4-digit years (and matches the partial predicate of
-- idx_albums_year_prefix, which also fixes the row estimate on the cast).
-- Two-phase pick: bound the album pool to 100 random matches first so the
-- randomized track sort works on ~1k rows instead of the whole decade's
-- tracks (~97k joined rows for the 2010s). For sparse ranges (<100 matching
-- albums) the pool is exhaustive, so semantics are unchanged.
WITH cand_albums AS (
    SELECT al.id
    FROM albums al
    WHERE al.year ~ '^[0-9]{4}'
      AND substring(al.year FROM 1 FOR 4)::int BETWEEN sqlc.arg(min_year)::int AND sqlc.arg(max_year)::int
    ORDER BY random()
    LIMIT 100
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
JOIN cand_albums ca ON ca.id = t.album_id
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
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
    JOIN media_item_cards mi ON mi.id = a.media_item_id
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
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE al.id = (SELECT id FROM chosen)
ORDER BY t.disc_number ASC, t.track_number ASC;
