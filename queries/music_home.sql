-- Smart-home shelves powering the music landing page. The 10 sections each
-- have their own query so the FE can refetch any one in isolation (the
-- rotating shelves rotate themselves every 5 minutes; the others honor the
-- standard cache TTL).
--
-- All artist/album/track row shapes mirror ListMusicAlbums / ListMusicArtists
-- so the FE can keep one row component per kind across the page.

-- name: ListRecentlyPlayedArtists :many
-- DISTINCT ON collapses repeats so a user who looped one artist's whole
-- discography still sees diversity. last_played_at is the max(played_at)
-- over any of that artist's tracks.
WITH artist_plays AS (
    SELECT DISTINCT ON (a.id)
           a.id                 AS artist_id,
           a.name               AS artist_name,
           mi.id                AS media_item_id,
           mi.public_id         AS media_item_public_id,
           mi.slug              AS artist_slug,
           mi.poster_path       AS poster_path,
           pe.played_at         AS last_played_at,
           (SELECT count(*) FROM albums al WHERE al.artist_id = a.id) AS album_count,
           (SELECT count(*) FROM tracks t JOIN albums al ON al.id = t.album_id WHERE al.artist_id = a.id) AS track_count,
           EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = a.media_item_id AND lf.deleted_at IS NULL) AS available
    FROM play_events pe
    JOIN tracks      t  ON t.id  = pe.track_id
    JOIN albums      al ON al.id = t.album_id
    JOIN artists     a  ON a.id  = al.artist_id
    JOIN media_item_cards mi ON mi.id = a.media_item_id
    WHERE pe.user_id = $1
    ORDER BY a.id, pe.played_at DESC
)
SELECT * FROM artist_plays
ORDER BY last_played_at DESC
LIMIT $2;

-- name: ListOnThisDayAlbums :many
-- Anniversary releases — albums whose release_date month+day matches today.
-- Ordered newest-year first so the user sees this-year-vs-decades-back at a
-- glance. Falls back to nothing when albums lack release_date — that's
-- expected for stub items not yet fully enriched.
SELECT al.*,
       a.name                                                       AS artist_name,
       mi.slug                                                      AS artist_slug,
       (SELECT count(*) FROM tracks t WHERE t.album_id = al.id)     AS track_count,
       (EXTRACT(YEAR FROM al.release_date))::int                    AS release_year
FROM albums al
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
  AND al.release_date IS NOT NULL
  AND EXTRACT(MONTH FROM al.release_date) = EXTRACT(MONTH FROM CURRENT_DATE)
  AND EXTRACT(DAY   FROM al.release_date) = EXTRACT(DAY   FROM CURRENT_DATE)
  AND EXISTS (SELECT 1 FROM tracks atrk JOIN track_files atf ON atf.track_id = atrk.id JOIN library_files alf ON alf.id = atf.library_file_id WHERE atrk.album_id = al.id AND alf.deleted_at IS NULL)
ORDER BY al.release_date DESC
LIMIT $1;

-- name: ListRecentUserPlaylists :many
-- Playlists ordered by the most recent play of any of their tracks (derived
-- — there's no last_played_at column on user_playlists). Nulls land last,
-- so freshly-created playlists show up below ones the user actually plays.
WITH last_per_playlist AS (
    SELECT upt.playlist_id, max(pe.played_at) AS last_played_at
    FROM user_playlist_tracks upt
    LEFT JOIN play_events pe ON pe.track_id = upt.track_id AND pe.user_id = $1
    GROUP BY upt.playlist_id
)
SELECT up.id,
       up.name,
       up.description,
       up.cover_path,
       up.created_at,
       up.updated_at,
       coalesce(lpp.last_played_at, up.updated_at)                 AS last_activity_at,
       lpp.last_played_at                                          AS last_played_at,
       (SELECT count(*) FROM user_playlist_tracks WHERE playlist_id = up.id)::bigint AS track_count
FROM user_playlists up
LEFT JOIN last_per_playlist lpp ON lpp.playlist_id = up.id
WHERE up.user_id = $1
ORDER BY last_activity_at DESC, up.id DESC
LIMIT $2;

-- name: PickRandomPlayedArtists :many
-- "More By" seeds — sample N artists from the user's play history with a
-- stable seed so the same shelf survives a re-fetch within the 5-min window.
-- md5() over the seed keeps the order deterministic per (user, time-bucket).
WITH artist_play_counts AS (
    SELECT a.id              AS artist_id,
           a.name            AS artist_name,
           mi.id             AS media_item_id,
           mi.public_id      AS media_item_public_id,
           mi.slug           AS artist_slug,
           count(*)          AS play_count
    FROM play_events pe
    JOIN tracks  t  ON t.id  = pe.track_id
    JOIN albums  al ON al.id = t.album_id
    JOIN artists a  ON a.id  = al.artist_id
    JOIN media_item_cards mi ON mi.id = a.media_item_id
    WHERE pe.user_id = sqlc.arg(user_id)
      AND EXISTS (SELECT 1 FROM library_files alf WHERE alf.media_item_id = a.media_item_id AND alf.deleted_at IS NULL)
    GROUP BY a.id, a.name, mi.id, mi.public_id, mi.slug
)
SELECT artist_id, artist_name, media_item_id, media_item_public_id, artist_slug, play_count
FROM artist_play_counts
ORDER BY md5(artist_id::text || sqlc.arg(seed)::text) ASC
LIMIT sqlc.arg(picks);

-- name: ListAlbumsByArtistIDForShelf :many
-- Helper for "More By Artist" — small per-artist album lists. Carries the
-- shape the album-row component already speaks.
SELECT al.*,
       a.name                                                   AS artist_name,
       mi.slug                                                  AS artist_slug,
       (SELECT count(*) FROM tracks t WHERE t.album_id = al.id) AS track_count
FROM albums al
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE al.artist_id = $1
  AND EXISTS (SELECT 1 FROM tracks atrk JOIN track_files atf ON atf.track_id = atrk.id JOIN library_files alf ON alf.id = atf.library_file_id WHERE atrk.album_id = al.id AND alf.deleted_at IS NULL)
ORDER BY al.year DESC NULLS LAST, lower(al.title) ASC
LIMIT $2;

-- name: PickTopGenresForUser :many
-- Top genres derived from albums in user's play history (album.genres TEXT[]).
-- Counts each play once per genre on the album so "metal" wins out over
-- happens-to-be-tagged genres. UNNEST flattens the array; the seed in the
-- service controls which one is picked for the rotating shelf.
SELECT (unnested_genre)::text AS genre,
       count(*)::bigint        AS play_count
FROM (
    SELECT unnest(al.genres) AS unnested_genre
    FROM play_events pe
    JOIN tracks  t  ON t.id  = pe.track_id
    JOIN albums  al ON al.id = t.album_id
    WHERE pe.user_id = $1
) sub
WHERE unnested_genre IS NOT NULL AND unnested_genre != ''
GROUP BY unnested_genre
ORDER BY play_count DESC
LIMIT $2;

-- name: ListArtistsByGenre :many
-- Artists whose albums include the given genre, library scoped to music.
-- This drives the "More in <genre>" no-images list — the FE renders it as
-- an artist-name list with album/track counts.
-- Dedup + limit happen in the inner subquery so the count subqueries run
-- once per OUTPUT row (the LIMIT) instead of once per (artist × genre-album)
-- join row — 5,150 joins rows for 'Electronic' made the original ~1.3s;
-- this shape measures ~95ms on the same data. a.id tie-break: duplicate
-- artist names exist; keeps the LIMIT boundary deterministic regardless of
-- DISTINCT plan shape.
SELECT sub.artist_id,
       sub.artist_name,
       sub.media_item_id,
       sub.media_item_public_id,
       sub.artist_slug,
       sub.poster_path,
       (SELECT count(*) FROM albums al2 WHERE al2.artist_id = sub.artist_id) AS album_count,
       (SELECT count(*) FROM tracks t JOIN albums al3 ON al3.id = t.album_id
                                       WHERE al3.artist_id = sub.artist_id)  AS track_count
FROM (
    SELECT DISTINCT a.id           AS artist_id,
                    a.name         AS artist_name,
                    mi.id          AS media_item_id,
                    mi.public_id   AS media_item_public_id,
                    mi.slug        AS artist_slug,
                    mi.poster_path AS poster_path
    FROM artists a
    JOIN albums       al ON al.artist_id = a.id
    JOIN media_item_cards mi ON mi.id = a.media_item_id
    JOIN libraries    l  ON l.id  = mi.library_id
    WHERE l.media_type = 'music'
      AND sqlc.arg(genre)::text = ANY(al.genres)
      AND EXISTS (SELECT 1 FROM library_files alf WHERE alf.media_item_id = a.media_item_id AND alf.deleted_at IS NULL)
    ORDER BY a.name ASC, a.id ASC
    LIMIT $1
) sub
ORDER BY sub.artist_name ASC, sub.artist_id ASC;

-- name: MostPlayedAlbumsInRange :many
-- "Most Played in <month>" — albums ordered by # of play_events landing on
-- their tracks in the given window. play_count is the per-album play volume
-- in that window so the FE can render "Played 42×" as a subtitle.
SELECT al.id              AS album_id,
       al.title           AS album_title,
       al.slug            AS album_slug,
       al.cover_path      AS album_cover_path,
       al.year            AS album_year,
       a.id               AS artist_id,
       a.name             AS artist_name,
       mi.slug            AS artist_slug,
       count(*)::bigint   AS play_count
FROM play_events pe
JOIN tracks      t  ON t.id  = pe.track_id
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE pe.user_id = $1
  AND pe.played_at >= sqlc.arg(start_at)
  AND pe.played_at <  sqlc.arg(end_at)
  AND EXISTS (SELECT 1 FROM tracks atrk JOIN track_files atf ON atf.track_id = atrk.id JOIN library_files alf ON alf.id = atf.library_file_id WHERE atrk.album_id = al.id AND alf.deleted_at IS NULL)
GROUP BY al.id, al.title, al.slug, al.cover_path, al.year,
         a.id, a.name, mi.slug
ORDER BY play_count DESC, al.id DESC
LIMIT $2;

-- name: ListLapsedArtists :many
-- Artists the user used to listen to but hasn't in `since_days`. The HAVING
-- max(played_at) < cutoff is the lapsed-flag; we also require a minimum play
-- count so a one-off play 6 months ago doesn't surface as "you used to love
-- them". Stable randomization by md5(seed) for the 5-min rotation.
WITH artist_last_played AS (
    SELECT a.id                                  AS artist_id,
           a.name                                AS artist_name,
           mi.id                                 AS media_item_id,
           mi.public_id                          AS media_item_public_id,
           mi.slug                               AS artist_slug,
           max(pe.played_at)::timestamptz        AS last_played_at,
           count(*)::bigint                      AS play_count
    FROM play_events pe
    JOIN tracks      t  ON t.id  = pe.track_id
    JOIN albums      al ON al.id = t.album_id
    JOIN artists     a  ON a.id  = al.artist_id
    JOIN media_item_cards mi ON mi.id = a.media_item_id
    WHERE pe.user_id = sqlc.arg(user_id)
      AND EXISTS (SELECT 1 FROM library_files alf WHERE alf.media_item_id = a.media_item_id AND alf.deleted_at IS NULL)
    GROUP BY a.id, a.name, mi.id, mi.public_id, mi.slug
    HAVING max(pe.played_at) < sqlc.arg(cutoff_at)
       AND count(*) >= sqlc.arg(min_plays)
)
SELECT artist_id, artist_name, media_item_id, media_item_public_id, artist_slug, last_played_at, play_count
FROM artist_last_played
ORDER BY md5(artist_id::text || sqlc.arg(seed)::text) ASC
LIMIT sqlc.arg(picks);

-- name: PickLabelForUser :many
-- Sample N labels that the user actually listens to. Returns ordered by md5
-- of (label, seed) so the rotation is stable within a window but varies
-- across windows. Filters empty labels — many albums never get a label tag.
WITH user_labels AS (
    SELECT al.label, count(*) AS play_count
    FROM play_events pe
    JOIN tracks t   ON t.id  = pe.track_id
    JOIN albums al  ON al.id = t.album_id
    WHERE pe.user_id = sqlc.arg(user_id)
      AND al.label IS NOT NULL AND al.label != ''
      AND EXISTS (SELECT 1 FROM tracks atrk JOIN track_files atf ON atf.track_id = atrk.id JOIN library_files alf ON alf.id = atf.library_file_id WHERE atrk.album_id = al.id AND alf.deleted_at IS NULL)
    GROUP BY al.label
)
SELECT label, play_count
FROM user_labels
ORDER BY md5(label || sqlc.arg(seed)::text) ASC
LIMIT sqlc.arg(picks);

-- name: ListAlbumsByLabel :many
-- "More from <label>" — every album on the label. Carries artist context so
-- one row can render "<album> — <artist>". Library-scoped to music so we
-- don't accidentally cross-link from a soundtracks label into film items.
SELECT al.id              AS album_id,
       al.title           AS album_title,
       al.slug            AS album_slug,
       al.cover_path      AS album_cover_path,
       al.year            AS album_year,
       al.label           AS album_label,
       a.id               AS artist_id,
       a.name             AS artist_name,
       mi.slug            AS artist_slug
FROM albums al
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
  AND lower(al.label) = lower(sqlc.arg(label)::text)
  AND EXISTS (SELECT 1 FROM tracks atrk JOIN track_files atf ON atf.track_id = atrk.id JOIN library_files alf ON alf.id = atf.library_file_id WHERE atrk.album_id = al.id AND alf.deleted_at IS NULL)
ORDER BY al.year DESC NULLS LAST, lower(al.title) ASC
LIMIT $1;

-- name: ListUserSeedArtistsForMixes :many
-- "Mixes for You" seeds — top-N artists by play volume in the last `since_days`.
-- Each row becomes one mix; the service then runs SimilarArtists against the
-- artist_centroids index to flesh out the mix from sonic-similar artists.
WITH ranked AS (
    SELECT a.id                AS artist_id,
           a.name              AS artist_name,
           mi.id               AS media_item_id,
           mi.public_id        AS media_item_public_id,
           mi.slug             AS artist_slug,
           count(*)            AS play_count,
           max(pe.played_at)   AS last_played_at
    FROM play_events pe
    JOIN tracks      t  ON t.id  = pe.track_id
    JOIN albums      al ON al.id = t.album_id
    JOIN artists     a  ON a.id  = al.artist_id
    JOIN media_item_cards mi ON mi.id = a.media_item_id
    WHERE pe.user_id = sqlc.arg(user_id)
      AND pe.played_at >= sqlc.arg(since_at)
      AND EXISTS (SELECT 1 FROM library_files alf WHERE alf.media_item_id = a.media_item_id AND alf.deleted_at IS NULL)
    GROUP BY a.id, a.name, mi.id, mi.public_id, mi.slug
)
SELECT artist_id, artist_name, media_item_id, media_item_public_id, artist_slug, play_count, last_played_at
FROM ranked
ORDER BY play_count DESC, last_played_at DESC
LIMIT sqlc.arg(picks);

-- name: ListArtistTracksTopPlayedFirst :many
-- Drives the "play this artist" button on home tiles. Returns every track the
-- artist has, ordered by the user's play count desc (top hits first), then
-- album year desc, then disc/track number — so the queue plays "best of" up
-- front but still contains the full catalog for endless play-through.
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
       (SELECT count(*) FROM play_events pe WHERE pe.track_id = t.id AND pe.user_id = sqlc.arg(user_id))::bigint AS user_play_count
FROM tracks t
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE mi.slug = sqlc.arg(slug)
  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
ORDER BY user_play_count DESC, al.year DESC NULLS LAST,
         t.disc_number ASC, t.track_number ASC
LIMIT sqlc.arg(track_limit);

-- name: ListArtistTopTracksForMix :many
-- Pulls the tracks that go into a mix for one seed artist's "neighborhood".
-- Given a slice of artist_ids (the seed + its sonic neighbors), returns up
-- to `tracks_per_artist` tracks per artist ordered by play_count desc.
-- diversifyByArtist in the service shuffles them inter-artist so the mix
-- doesn't run six tracks of the same artist back-to-back.
WITH ranked AS (
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
           (SELECT count(*) FROM play_events pe WHERE pe.track_id = t.id) AS play_count,
           row_number() OVER (PARTITION BY a.id ORDER BY (SELECT count(*) FROM play_events pe WHERE pe.track_id = t.id) DESC, t.id ASC) AS rn
    FROM tracks t
    JOIN albums      al ON al.id = t.album_id
    JOIN artists     a  ON a.id  = al.artist_id
    JOIN media_item_cards mi ON mi.id = a.media_item_id
    WHERE a.id = ANY(sqlc.arg(artist_ids)::bigint[])
      AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
)
SELECT track_id, track_title, duration, disc_number, track_number,
       album_id, album_title, album_slug, album_cover_path, album_year,
       artist_id, artist_name, artist_slug, play_count
FROM ranked
WHERE rn <= sqlc.arg(tracks_per_artist)::int;
