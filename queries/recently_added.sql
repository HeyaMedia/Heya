-- Home-page "recently added" rails. TV and music rails group raw file
-- additions into Plex-style entries (new series / new season / new episodes)
-- in the service layer; these queries only surface the inputs.
--
-- All "first added" queries deliberately include soft-deleted files: a
-- quality upgrade replaces the library_files row, and the old (deleted) row
-- is what proves the episode/album isn't actually new.

-- name: ListRecentlyAddedTVFiles :many
-- Newest N live TV files with the season/episode numbers the parser
-- extracted. The derived table pins the plan to a backward
-- idx_library_files_created_at scan with a per-row media_items probe that
-- stops at LIMIT, instead of sorting every live file.
SELECT r.id, r.media_item_id, r.created_at,
       r.library_id, r.title, r.slug,
       (COALESCE((r.parse_result->'parsed'->'release'->'seasons'->>0)::int, -1))::int AS season_number,
       (COALESCE(r.parse_result->'parsed'->'release'->'episodes', '[]'::jsonb))::jsonb AS episode_numbers
FROM (
  SELECT lf.id, lf.media_item_id, lf.created_at, lf.parse_result,
         mi.library_id, mi.title, mi.slug
  FROM library_files lf
  JOIN media_item_cards mi ON mi.id = lf.media_item_id
  WHERE mi.media_type = 'tv' AND lf.deleted_at IS NULL
  ORDER BY lf.created_at DESC
  LIMIT $1
) r
ORDER BY r.created_at DESC;

-- name: ListTVEpisodeFirstAdded :many
-- Per-(show, season, episode) earliest file arrival for the given shows.
-- Multi-episode files fan out via the lateral so each covered episode gets
-- its own first-added stamp.
SELECT lf.media_item_id,
       (COALESCE((lf.parse_result->'parsed'->'release'->'seasons'->>0)::int, -1))::int AS season_number,
       (ep.value)::int    AS episode_number,
       (MIN(lf.created_at))::timestamptz AS first_added
FROM library_files lf
CROSS JOIN LATERAL jsonb_array_elements_text(
  COALESCE(lf.parse_result->'parsed'->'release'->'episodes', '[]'::jsonb)
) AS ep(value)
WHERE lf.media_item_id = ANY(@media_item_ids::bigint[])
GROUP BY lf.media_item_id, 2, 3;

-- name: GetTVEpisodeBrief :one
-- Point lookup for a single-episode rail entry's display title.
SELECT e.id, e.title
FROM tv_series s
JOIN tv_seasons  se ON se.series_id = s.id AND se.season_number = $2
JOIN tv_episodes e  ON e.season_id = se.id AND e.episode_number = $3
WHERE s.media_item_id = $1;

-- name: ListRecentlyAddedMusicFiles :many
-- Newest N live music files mapped through their track to the album and
-- artist. Files not yet matched to a track drop out (inner joins) — they
-- can't be attributed to an artist event yet.
SELECT r.created_at, r.media_item_id,
       t.album_id, al.artist_id,
       al.title AS album_title, al.slug AS album_slug,
       al.album_type
FROM (
  SELECT lf.id, lf.created_at, lf.media_item_id
  FROM library_files lf
  JOIN media_item_cards mi ON mi.id = lf.media_item_id
  WHERE mi.media_type = 'music' AND lf.deleted_at IS NULL
  ORDER BY lf.created_at DESC
  LIMIT $1
) r
JOIN track_files tf ON tf.library_file_id = r.id
JOIN tracks      t  ON t.id = tf.track_id
JOIN albums      al ON al.id = t.album_id;

-- name: ListArtistFirstAdded :many
-- Earliest file arrival per artist media item (music files hang off the
-- artist's media_item_id).
SELECT lf.media_item_id, (MIN(lf.created_at))::timestamptz AS first_added
FROM library_files lf
WHERE lf.media_item_id = ANY(@media_item_ids::bigint[])
GROUP BY lf.media_item_id;

-- name: ListAlbumFirstAdded :many
-- Earliest file arrival per album, via the track_files linkage.
SELECT t.album_id, (MIN(lf.created_at))::timestamptz AS first_added
FROM tracks t
JOIN track_files   tf ON tf.track_id = t.id
JOIN library_files lf ON lf.id = tf.library_file_id
WHERE t.album_id = ANY(@album_ids::bigint[])
GROUP BY t.album_id;

-- name: ListArtistsBriefByIDs :many
-- Display info for the artists the rail surfaced.
SELECT a.id, a.name, a.media_item_id, mi.slug,
       (SELECT count(*) FROM albums al WHERE al.artist_id = a.id) AS album_count,
       (SELECT count(*) FROM tracks t JOIN albums al ON al.id = t.album_id WHERE al.artist_id = a.id) AS track_count
FROM artists a
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE a.id = ANY(@artist_ids::bigint[]);
