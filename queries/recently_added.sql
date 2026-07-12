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
-- stops at LIMIT, instead of sorting every live file. A window of 0 lifts
-- the cap entirely (LIMIT NULL) — the deep-history pages of the infinite
-- rail group the show's full arrival timeline. Show descriptions are NOT
-- carried per file row (they'd multiply a ~1KB blob by every file in the
-- window); the service overlays them per surfaced entry via
-- ListMediaDescriptionsByIDs below.
SELECT r.id, r.media_item_id, r.created_at,
       r.public_id, r.library_id, r.title, r.slug,
       (COALESCE((r.parse_result->'parsed'->'release'->'seasons'->>0)::int, -1))::int AS season_number,
       (COALESCE(r.parse_result->'parsed'->'release'->'episodes', '[]'::jsonb))::jsonb AS episode_numbers
FROM (
  SELECT lf.id, lf.media_item_id, lf.created_at, lf.parse_result,
         mi.public_id, mi.library_id, mi.title, mi.slug
  FROM library_files lf
  JOIN media_item_cards mi ON mi.id = lf.media_item_id
  WHERE mi.media_type IN ('tv', 'anime') AND lf.deleted_at IS NULL
  ORDER BY lf.created_at DESC
  LIMIT NULLIF(sqlc.arg(file_window)::bigint, 0)
) r
ORDER BY r.created_at DESC;

-- name: ListMediaDescriptionsByIDs :many
-- Batched description lookup for the entries that survived the grouping cut.
SELECT id, description FROM media_item_cards WHERE id = ANY(@ids::bigint[]);

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
-- Point lookup for a single-episode rail entry's display title + overview
-- (English preferred, else any non-empty language). Correlated subquery on
-- purpose — sqlc mistypes LEFT JOIN LATERAL columns as non-nullable.
SELECT e.id, e.title,
       COALESCE((
         SELECT eo.overview FROM episode_overviews eo
         WHERE eo.episode_id = e.id AND eo.overview <> ''
         ORDER BY (eo.language = 'en') DESC
         LIMIT 1
       ), NULLIF(e.overview, ''), '')::text AS overview
FROM tv_series s
JOIN tv_seasons  se ON se.series_id = s.id AND se.season_number = $2
JOIN tv_episodes e  ON e.season_id = se.id AND e.episode_number = $3
WHERE s.media_item_id = $1;

-- name: GetTVSeasonOverview :one
-- Season blurb for a "new season" rail entry; empty when the provider
-- shipped none (caller falls back to the show description).
SELECT COALESCE(se.overview, '')::text AS overview
FROM tv_series s
JOIN tv_seasons se ON se.series_id = s.id AND se.season_number = $2
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
SELECT a.id, a.name, a.media_item_id, mi.public_id AS media_item_public_id, mi.slug,
       (SELECT count(*) FROM albums al WHERE al.artist_id = a.id) AS album_count,
       (SELECT count(*) FROM tracks t JOIN albums al ON al.id = t.album_id WHERE al.artist_id = a.id) AS track_count
FROM artists a
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE a.id = ANY(@artist_ids::bigint[]);
