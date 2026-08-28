-- +goose Up
-- Provider runtimes are reference data. Operational durations describe the
-- bytes actually present in the library and come exclusively from ffprobe.
ALTER TABLE movies ADD COLUMN metadata_runtime_minutes integer NOT NULL DEFAULT 0;
ALTER TABLE tv_episodes ADD COLUMN metadata_runtime_minutes integer NOT NULL DEFAULT 0;
ALTER TABLE tracks ADD COLUMN metadata_duration integer NOT NULL DEFAULT 0;
ALTER TABLE albums ADD COLUMN metadata_duration_seconds integer NOT NULL DEFAULT 0;

UPDATE movies SET metadata_runtime_minutes = runtime_minutes, runtime_minutes = 0;
UPDATE tv_episodes SET metadata_runtime_minutes = runtime_minutes, runtime_minutes = 0;
UPDATE tracks SET metadata_duration = duration, duration = 0;
UPDATE albums SET metadata_duration_seconds = duration_seconds, duration_seconds = 0;

-- Existing libraries already have probe results; backfill without a re-scan.
WITH durations AS (
    SELECT l.movie_id,
           ROUND(COALESCE(
             SUM((lf.media_info->>'duration')::numeric) FILTER (WHERE l.relation_type = 'part'),
             MAX((lf.media_info->>'duration')::numeric) FILTER (WHERE l.relation_type = 'primary')
           ) / 60.0)::integer AS minutes
    FROM library_file_links l
    JOIN library_files lf ON lf.id = l.library_file_id
    WHERE l.movie_id IS NOT NULL AND lf.deleted_at IS NULL
      AND l.relation_type IN ('primary', 'part')
      AND lf.media_info->>'duration' ~ '^[0-9]+(?:\.[0-9]+)?$'
    GROUP BY l.movie_id
)
UPDATE movies m SET runtime_minutes = GREATEST(d.minutes, 1)
FROM durations d WHERE d.movie_id = m.id AND d.minutes > 0;

WITH durations AS (
    SELECT l.tv_episode_id,
           ROUND(COALESCE(
             SUM((lf.media_info->>'duration')::numeric) FILTER (WHERE l.relation_type = 'part'),
             MAX((lf.media_info->>'duration')::numeric) FILTER (WHERE l.relation_type = 'primary')
           ) / 60.0)::integer AS minutes
    FROM library_file_links l
    JOIN library_files lf ON lf.id = l.library_file_id
    WHERE l.tv_episode_id IS NOT NULL AND lf.deleted_at IS NULL
      AND l.relation_type IN ('primary', 'part')
      AND lf.media_info->>'duration' ~ '^[0-9]+(?:\.[0-9]+)?$'
    GROUP BY l.tv_episode_id
)
UPDATE tv_episodes e SET runtime_minutes = GREATEST(d.minutes, 1)
FROM durations d WHERE d.tv_episode_id = e.id AND d.minutes > 0;

UPDATE tracks t SET duration = best.duration
FROM (
    SELECT DISTINCT ON (tf.track_id) tf.track_id, tf.duration
    FROM track_files tf
    JOIN library_files lf ON lf.id = tf.library_file_id
    WHERE lf.deleted_at IS NULL AND tf.duration > 0
    ORDER BY tf.track_id, tf.quality_score DESC, tf.id ASC
) best
WHERE best.track_id = t.id;

UPDATE albums a SET duration_seconds = totals.duration
FROM (SELECT album_id, SUM(duration)::integer AS duration FROM tracks GROUP BY album_id) totals
WHERE totals.album_id = a.id;

-- +goose Down
ALTER TABLE albums DROP COLUMN metadata_duration_seconds;
ALTER TABLE tracks DROP COLUMN metadata_duration;
ALTER TABLE tv_episodes DROP COLUMN metadata_runtime_minutes;
ALTER TABLE movies DROP COLUMN metadata_runtime_minutes;
