-- Queries backing the Jellyfin-compatible API (internal/jellyfin), wrapped by
-- internal/service/jellyfin_query.go. Purpose-built for Jellyfin's /Items
-- param grid: one generic lister per entity level (media_items / seasons /
-- episodes / albums / tracks), each with the same optional-filter idiom —
-- a zero/empty arg disables its clause. Sort dispatch happens in ORDER BY
-- CASE lines so one prepared statement serves every sort the clients send.
-- LIMIT NULLIF(x, 0) means "0 = unlimited", matching Jellyfin, whose /Items
-- returns everything when no Limit is given (Finamp fetches whole track
-- catalogs that way).

-- name: JFListLibraryItems :many
SELECT mi.id, mi.library_id, mi.media_type,
       COALESCE((SELECT mt.title FROM media_titles mt
                 WHERE mt.media_item_id = mi.id AND mt.language = 'en' AND mt.title <> ''
                 LIMIT 1), mi.title) AS title,
       mi.sort_title, mi.year,
       COALESCE((SELECT mo.overview FROM media_overviews mo
                 WHERE mo.media_item_id = mi.id AND mo.language = 'en' AND mo.overview <> ''
                 LIMIT 1), mi.description) AS description,
       mi.slug, mi.external_ids, mi.status, mi.tagline,
       mi.created_at, mi.updated_at,
       m.runtime_minutes AS movie_runtime_minutes,
       m.genres AS movie_genres,
       m.rating AS movie_rating,
       m.release_date AS movie_release_date,
       ts.id AS series_id,
       ts.genres AS series_genres,
       ts.rating AS series_rating,
       ts.first_air_date AS series_first_air_date,
       ts.last_air_date AS series_last_air_date,
       ts.status AS series_status,
       ts.number_of_episodes AS series_episode_count,
       ts.number_of_seasons AS series_season_count,
       ar.id AS artist_id,
       ar.name AS artist_name,
       CASE WHEN mi.media_type = 'movie' THEN
         COALESCE((SELECT lf.path FROM library_files lf
                   WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL AND lf.status = 'matched'
                   ORDER BY lf.id LIMIT 1), '')
       ELSE '' END::text AS primary_path
FROM media_items mi
LEFT JOIN movies m ON m.media_item_id = mi.id
LEFT JOIN tv_series ts ON ts.media_item_id = mi.id
LEFT JOIN artists ar ON ar.media_item_id = mi.id
WHERE mi.media_type = sqlc.arg(media_type)
  AND (sqlc.arg(library_id)::bigint = 0 OR mi.library_id = sqlc.arg(library_id))
  AND (cardinality(sqlc.arg(only_ids)::bigint[]) = 0 OR mi.id = ANY(sqlc.arg(only_ids)::bigint[]))
  AND (sqlc.arg(search)::text = '' OR mi.title ILIKE '%' || sqlc.arg(search) || '%')
  AND (NOT sqlc.arg(filter_played)::bool OR mi.id = ANY(sqlc.arg(played_ids)::bigint[]))
  AND (NOT sqlc.arg(filter_unplayed)::bool OR NOT (mi.id = ANY(sqlc.arg(played_ids)::bigint[])))
  AND (NOT sqlc.arg(filter_favorite)::bool OR mi.id = ANY(sqlc.arg(favorite_ids)::bigint[]))
  -- Genre filter (case-insensitive name overlap). Empty arg = no filter. Movies
  -- carry m.genres, series ts.genres; COALESCE picks whichever this item has.
  AND (cardinality(sqlc.arg(genres)::text[]) = 0 OR EXISTS (
        SELECT 1 FROM unnest(COALESCE(m.genres, ts.genres)) AS g
        WHERE lower(g) = ANY(sqlc.arg(genres)::text[])))
  -- Hide episode-less TV series: enrichment that produced no seasons leaves a
  -- phantom series (no /Seasons, no /Episodes) that strict clients (Infuse)
  -- error on. Real Jellyfin never has one — a series exists only from real
  -- episode files. Series WITH seasons but empty tv_episodes still pass.
  AND (mi.media_type <> 'tv' OR EXISTS (SELECT 1 FROM tv_seasons s2 WHERE s2.series_id = ts.id))
ORDER BY
  CASE WHEN sqlc.arg(sort_by)::text = 'random' THEN md5(mi.id::text || sqlc.arg(rand_seed)::text) END ASC,
  CASE WHEN sqlc.arg(sort_by) = 'added'    AND sqlc.arg(sort_desc)::bool     THEN mi.created_at END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort_by) = 'added'    AND NOT sqlc.arg(sort_desc)::bool THEN mi.created_at END ASC NULLS LAST,
  CASE WHEN sqlc.arg(sort_by) = 'premiere' AND sqlc.arg(sort_desc)::bool     THEN COALESCE(m.release_date, ts.first_air_date) END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort_by) = 'premiere' AND NOT sqlc.arg(sort_desc)::bool THEN COALESCE(m.release_date, ts.first_air_date) END ASC NULLS LAST,
  CASE WHEN sqlc.arg(sort_by) = 'year'     AND sqlc.arg(sort_desc)::bool     THEN NULLIF(mi.year, '') END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort_by) = 'year'     AND NOT sqlc.arg(sort_desc)::bool THEN NULLIF(mi.year, '') END ASC NULLS LAST,
  CASE WHEN sqlc.arg(sort_by) = 'rating'   AND sqlc.arg(sort_desc)::bool     THEN COALESCE(m.rating, ts.rating) END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort_by) = 'rating'   AND NOT sqlc.arg(sort_desc)::bool THEN COALESCE(m.rating, ts.rating) END ASC NULLS LAST,
  CASE WHEN sqlc.arg(sort_desc)::bool THEN lower(COALESCE(NULLIF(mi.sort_title, ''), mi.title)) END DESC,
  CASE WHEN NOT sqlc.arg(sort_desc)::bool THEN lower(COALESCE(NULLIF(mi.sort_title, ''), mi.title)) END ASC,
  mi.id ASC
LIMIT NULLIF(sqlc.arg(lim)::int, 0) OFFSET sqlc.arg(off)::int;

-- name: JFCountLibraryItems :one
SELECT count(*)
FROM media_items mi
LEFT JOIN movies m ON m.media_item_id = mi.id
LEFT JOIN tv_series ts ON ts.media_item_id = mi.id
WHERE mi.media_type = sqlc.arg(media_type)
  AND (sqlc.arg(library_id)::bigint = 0 OR mi.library_id = sqlc.arg(library_id))
  AND (cardinality(sqlc.arg(only_ids)::bigint[]) = 0 OR mi.id = ANY(sqlc.arg(only_ids)::bigint[]))
  AND (sqlc.arg(search)::text = '' OR mi.title ILIKE '%' || sqlc.arg(search) || '%')
  AND (NOT sqlc.arg(filter_played)::bool OR mi.id = ANY(sqlc.arg(played_ids)::bigint[]))
  AND (NOT sqlc.arg(filter_unplayed)::bool OR NOT (mi.id = ANY(sqlc.arg(played_ids)::bigint[])))
  AND (NOT sqlc.arg(filter_favorite)::bool OR mi.id = ANY(sqlc.arg(favorite_ids)::bigint[]))
  -- Genre filter (case-insensitive name overlap). Empty arg = no filter.
  AND (cardinality(sqlc.arg(genres)::text[]) = 0 OR EXISTS (
        SELECT 1 FROM unnest(COALESCE(m.genres, ts.genres)) AS g
        WHERE lower(g) = ANY(sqlc.arg(genres)::text[])))
  -- Mirror JFListLibraryItems: exclude episode-less TV series.
  AND (mi.media_type <> 'tv' OR EXISTS (
        SELECT 1 FROM tv_series ts2 JOIN tv_seasons s2 ON s2.series_id = ts2.id
        WHERE ts2.media_item_id = mi.id));

-- name: JFListSeasons :many
SELECT s.id, s.series_id, s.season_number, s.title, s.overview, s.air_date,
       s.aired_episodes,
       ser.media_item_id AS series_media_item_id,
       smi.title AS series_title,
       smi.library_id,
       (SELECT count(*) FROM tv_episodes e WHERE e.season_id = s.id)::int AS episode_count
FROM tv_seasons s
JOIN tv_series ser ON ser.id = s.series_id
JOIN media_items smi ON smi.id = ser.media_item_id
WHERE (sqlc.arg(series_media_item_id)::bigint = 0 OR ser.media_item_id = sqlc.arg(series_media_item_id))
  AND (cardinality(sqlc.arg(only_ids)::bigint[]) = 0 OR s.id = ANY(sqlc.arg(only_ids)::bigint[]))
ORDER BY (CASE WHEN s.season_number = 0 THEN 1 ELSE 0 END), s.season_number ASC;

-- name: JFListEpisodes :many
SELECT e.id, e.season_id, e.episode_number,
       COALESCE((SELECT et.title FROM episode_titles et
                 WHERE et.episode_id = e.id AND et.language = 'en' AND et.title <> ''
                 LIMIT 1), e.title) AS title,
       COALESCE((SELECT eo.overview FROM episode_overviews eo
                 WHERE eo.episode_id = e.id AND eo.language = 'en' AND eo.overview <> ''
                 LIMIT 1), e.overview) AS overview,
       e.still_path,
       e.runtime_minutes, e.air_date, e.rating, e.is_special,
       s.season_number,
       s.title AS season_title,
       ser.media_item_id AS series_media_item_id,
       smi.title AS series_title,
       smi.library_id
FROM tv_episodes e
JOIN tv_seasons s ON s.id = e.season_id
JOIN tv_series ser ON ser.id = s.series_id
JOIN media_items smi ON smi.id = ser.media_item_id
WHERE (sqlc.arg(season_id)::bigint = 0 OR e.season_id = sqlc.arg(season_id))
  AND (sqlc.arg(series_media_item_id)::bigint = 0 OR ser.media_item_id = sqlc.arg(series_media_item_id))
  AND (sqlc.arg(library_id)::bigint = 0 OR smi.library_id = sqlc.arg(library_id))
  AND (cardinality(sqlc.arg(only_ids)::bigint[]) = 0 OR e.id = ANY(sqlc.arg(only_ids)::bigint[]))
  AND (sqlc.arg(search)::text = '' OR e.title ILIKE '%' || sqlc.arg(search) || '%')
ORDER BY
  CASE WHEN sqlc.arg(sort_by)::text = 'added' THEN e.id END DESC,
  (CASE WHEN s.season_number = 0 THEN 1 ELSE 0 END), s.season_number ASC, e.episode_number ASC, e.id ASC
LIMIT NULLIF(sqlc.arg(lim)::int, 0) OFFSET sqlc.arg(off)::int;

-- name: JFCountEpisodes :one
SELECT count(*)
FROM tv_episodes e
JOIN tv_seasons s ON s.id = e.season_id
JOIN tv_series ser ON ser.id = s.series_id
JOIN media_items smi ON smi.id = ser.media_item_id
WHERE (sqlc.arg(season_id)::bigint = 0 OR e.season_id = sqlc.arg(season_id))
  AND (sqlc.arg(series_media_item_id)::bigint = 0 OR ser.media_item_id = sqlc.arg(series_media_item_id))
  AND (sqlc.arg(library_id)::bigint = 0 OR smi.library_id = sqlc.arg(library_id))
  AND (cardinality(sqlc.arg(only_ids)::bigint[]) = 0 OR e.id = ANY(sqlc.arg(only_ids)::bigint[]))
  AND (sqlc.arg(search)::text = '' OR e.title ILIKE '%' || sqlc.arg(search) || '%');

-- name: JFListAlbums :many
SELECT al.id, al.artist_id, al.title, al.slug, al.year, al.album_type,
       al.genres, al.cover_path, al.release_date, al.total_tracks,
       al.duration_seconds, al.rating,
       ar.name AS artist_name,
       ar.media_item_id AS artist_media_item_id,
       mi.slug AS artist_slug,
       mi.library_id
FROM albums al
JOIN artists ar ON ar.id = al.artist_id
JOIN media_items mi ON mi.id = ar.media_item_id
WHERE (sqlc.arg(artist_media_item_id)::bigint = 0 OR ar.media_item_id = sqlc.arg(artist_media_item_id))
  AND (sqlc.arg(library_id)::bigint = 0 OR mi.library_id = sqlc.arg(library_id))
  AND (cardinality(sqlc.arg(only_ids)::bigint[]) = 0 OR al.id = ANY(sqlc.arg(only_ids)::bigint[]))
  AND (sqlc.arg(search)::text = '' OR al.title ILIKE '%' || sqlc.arg(search) || '%')
ORDER BY
  CASE WHEN sqlc.arg(sort_by)::text = 'random' THEN md5(al.id::text || sqlc.arg(rand_seed)::text) END ASC,
  CASE WHEN sqlc.arg(sort_by) = 'premiere' AND sqlc.arg(sort_desc)::bool     THEN al.release_date END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort_by) = 'premiere' AND NOT sqlc.arg(sort_desc)::bool THEN al.release_date END ASC NULLS LAST,
  CASE WHEN sqlc.arg(sort_by) = 'year'     AND sqlc.arg(sort_desc)::bool     THEN NULLIF(al.year, '') END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort_by) = 'year'     AND NOT sqlc.arg(sort_desc)::bool THEN NULLIF(al.year, '') END ASC NULLS LAST,
  CASE WHEN sqlc.arg(sort_desc)::bool THEN lower(al.title) END DESC,
  CASE WHEN NOT sqlc.arg(sort_desc)::bool THEN lower(al.title) END ASC,
  al.id ASC
LIMIT NULLIF(sqlc.arg(lim)::int, 0) OFFSET sqlc.arg(off)::int;

-- name: JFCountAlbums :one
SELECT count(*)
FROM albums al
JOIN artists ar ON ar.id = al.artist_id
JOIN media_items mi ON mi.id = ar.media_item_id
WHERE (sqlc.arg(artist_media_item_id)::bigint = 0 OR ar.media_item_id = sqlc.arg(artist_media_item_id))
  AND (sqlc.arg(library_id)::bigint = 0 OR mi.library_id = sqlc.arg(library_id))
  AND (cardinality(sqlc.arg(only_ids)::bigint[]) = 0 OR al.id = ANY(sqlc.arg(only_ids)::bigint[]))
  AND (sqlc.arg(search)::text = '' OR al.title ILIKE '%' || sqlc.arg(search) || '%');

-- name: JFListTracks :many
SELECT t.id, t.album_id, t.disc_number, t.track_number, t.title, t.duration,
       al.title AS album_title,
       al.slug AS album_slug,
       al.cover_path AS album_cover_path,
       al.genres AS album_genres,
       ar.id AS artist_id,
       ar.name AS artist_name,
       ar.media_item_id AS artist_media_item_id,
       mi.slug AS artist_slug,
       mi.library_id,
       COALESCE((SELECT tf.id FROM track_files tf WHERE tf.track_id = t.id
        ORDER BY tf.quality_score DESC, tf.id ASC LIMIT 1), 0)::bigint AS best_file_id
FROM tracks t
JOIN albums al ON al.id = t.album_id
JOIN artists ar ON ar.id = al.artist_id
JOIN media_items mi ON mi.id = ar.media_item_id
WHERE (sqlc.arg(album_id)::bigint = 0 OR t.album_id = sqlc.arg(album_id))
  AND (sqlc.arg(artist_media_item_id)::bigint = 0 OR ar.media_item_id = sqlc.arg(artist_media_item_id))
  AND (sqlc.arg(library_id)::bigint = 0 OR mi.library_id = sqlc.arg(library_id))
  AND (cardinality(sqlc.arg(only_ids)::bigint[]) = 0 OR t.id = ANY(sqlc.arg(only_ids)::bigint[]))
  AND (sqlc.arg(search)::text = '' OR t.title ILIKE '%' || sqlc.arg(search) || '%')
ORDER BY
  CASE WHEN sqlc.arg(sort_by)::text = 'random' THEN md5(t.id::text || sqlc.arg(rand_seed)::text) END ASC,
  CASE WHEN sqlc.arg(sort_by) = 'name' AND sqlc.arg(sort_desc)::bool     THEN lower(t.title) END DESC,
  CASE WHEN sqlc.arg(sort_by) = 'name' AND NOT sqlc.arg(sort_desc)::bool THEN lower(t.title) END ASC,
  lower(al.title) ASC, t.disc_number ASC, t.track_number ASC, t.id ASC
LIMIT NULLIF(sqlc.arg(lim)::int, 0) OFFSET sqlc.arg(off)::int;

-- name: JFCountTracks :one
SELECT count(*)
FROM tracks t
JOIN albums al ON al.id = t.album_id
JOIN artists ar ON ar.id = al.artist_id
JOIN media_items mi ON mi.id = ar.media_item_id
WHERE (sqlc.arg(album_id)::bigint = 0 OR t.album_id = sqlc.arg(album_id))
  AND (sqlc.arg(artist_media_item_id)::bigint = 0 OR ar.media_item_id = sqlc.arg(artist_media_item_id))
  AND (sqlc.arg(library_id)::bigint = 0 OR mi.library_id = sqlc.arg(library_id))
  AND (cardinality(sqlc.arg(only_ids)::bigint[]) = 0 OR t.id = ANY(sqlc.arg(only_ids)::bigint[]))
  AND (sqlc.arg(search)::text = '' OR t.title ILIKE '%' || sqlc.arg(search) || '%');

-- name: JFListWatchProgressByIDs :many
-- Batch progress decoration for one page of dtos: resume position + played
-- flag per entity. entity_type is 'movie' (media_item ids) or 'episode'
-- (tv_episodes ids), mirroring user_watch_progress semantics.
SELECT entity_id, progress_seconds, total_seconds, completed
FROM user_watch_progress
WHERE user_id = sqlc.arg(user_id)
  AND entity_type = sqlc.arg(entity_type)
  AND entity_id = ANY(sqlc.arg(entity_ids)::bigint[]);

-- name: JFLibraryFilesByIDs :many
-- Batch hydration of library files for list-level MediaSources decoration
-- (fields=MediaSources on /Shows/{id}/Episodes and friends).
SELECT * FROM library_files
WHERE id = ANY(sqlc.arg(ids)::bigint[]) AND deleted_at IS NULL;

-- name: JFBestVideoFilesForItems :many
-- Best playable file per movie media item, batched: matched files win, then
-- path order — the same pick JFMovieFileID makes one item at a time.
SELECT DISTINCT ON (media_item_id) *
FROM library_files
WHERE media_item_id = ANY(sqlc.arg(media_item_ids)::bigint[]) AND deleted_at IS NULL
ORDER BY media_item_id, (status = 'matched') DESC, path ASC;

-- name: JFFileHasSegments :one
-- Backs MediaSourceInfo.HasSegments — jellyfin-web's MediaSegmentManager
-- gates its entire /MediaSegments fetch on this flag at playback start (a
-- falsy HasSegments means the skip-intro/outro UI never even asks). Real
-- Jellyfin computes the identical per-item EXISTS in
-- MediaSegmentManager.HasSegments (Jellyfin.Server.Implementations); the
-- table here (queries/media_segments.sql) is owned by the segments worker.
SELECT EXISTS(
    SELECT 1 FROM media_segments WHERE library_file_id = $1
) AS exists;

-- name: JFTrackFilesByIDs :many
-- track_file id -> owning library file, batched for list-level MediaSources
-- decoration of Audio items (fields=MediaSources).
SELECT tf.id, tf.library_file_id
FROM track_files tf
WHERE tf.id = ANY(sqlc.arg(ids)::bigint[]);
