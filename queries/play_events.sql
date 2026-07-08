-- name: RecordPlayEvent :one
-- Append a play event. The FE submits one of these per qualifying play
-- (>=30s heard or track-end, akin to Last.fm scrobble rules).
INSERT INTO play_events (user_id, track_id, listened_seconds, completed, source)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListRecentlyPlayedTracks :many
-- One row per distinct track, most-recent play first. The DISTINCT ON
-- collapses repeats so a user who looped one track all evening still sees a
-- diverse "Recently Played" rail. CTE pattern: dedupe by track_id taking
-- the freshest played_at, then re-sort the surviving rows newest-first for
-- the rail. Joins album+artist context so the row is self-contained — same
-- shape as ListMusicTracks plus a played_at field for the timestamp chip.
WITH dedup AS (
    SELECT DISTINCT ON (t.id)
           t.id              AS track_id,
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
           pe.played_at      AS played_at
    FROM play_events pe
    JOIN tracks      t  ON t.id = pe.track_id
    JOIN albums      al ON al.id = t.album_id
    JOIN artists     a  ON a.id  = al.artist_id
    JOIN media_item_cards mi ON mi.id = a.media_item_id
    WHERE pe.user_id = sqlc.arg(user_id)
    ORDER BY t.id, pe.played_at DESC
)
SELECT * FROM dedup
ORDER BY played_at DESC
LIMIT sqlc.arg(track_limit) OFFSET sqlc.arg(track_offset);

-- name: CountUserPlayEvents :one
SELECT count(*)::bigint FROM play_events WHERE user_id = $1;

-- name: TopUserGenres :many
-- Per-user top genres derived by joining play history × track_facets. Sums
-- per-event 1.0 weights (could be score-weighted later) so a track that the
-- user actually replays counts more than one they played once. min_score
-- filters out the long-tail low-confidence labels.
SELECT (elem->>'name')::text  AS genre_name,
       count(*)::bigint       AS play_count
FROM play_events pe
JOIN track_facets tf ON tf.track_id = pe.track_id
CROSS JOIN LATERAL jsonb_array_elements(tf.top_genres) AS elem
WHERE pe.user_id = sqlc.arg(user_id)
  AND (elem->>'score')::real >= sqlc.arg(min_score)::real
GROUP BY (elem->>'name')
ORDER BY play_count DESC, (elem->>'name') ASC
LIMIT sqlc.arg(bucket_limit);

-- name: TopUserMoods :many
-- Per-user mood profile. Returns the average classifier score per mood
-- across the user's play history, so a "Happy → 0.72" reading means
-- "tracks Heya users listen to typically score 0.72 on the Happy head".
SELECT mood_key::text                       AS mood_key,
       avg((tf.mood_tags->>mood_key)::real) AS avg_score,
       count(*)::bigint                     AS sample_count
FROM play_events pe
JOIN track_facets tf ON tf.track_id = pe.track_id
CROSS JOIN LATERAL (
    VALUES ('danceability'), ('voice'), ('mood_happy'), ('mood_sad'),
           ('mood_aggressive'), ('mood_relaxed'), ('mood_party'),
           ('mood_electronic'), ('mood_acoustic')
) AS heads(mood_key)
WHERE pe.user_id = $1
  AND tf.mood_tags ? mood_key
GROUP BY mood_key
ORDER BY avg_score DESC;

-- name: UserTempoHistogram :many
-- Per-user BPM distribution bucketed into the same bands the Browse > Tempo
-- tiles use. Pre-aggregated server-side so the FE can render the histogram
-- with one round-trip.
SELECT CASE
           WHEN tf.bpm <  90  THEN '0-90'
           WHEN tf.bpm < 110  THEN '90-110'
           WHEN tf.bpm < 130  THEN '110-130'
           WHEN tf.bpm < 150  THEN '130-150'
           ELSE                    '150-300'
       END                  AS band,
       count(*)::bigint     AS play_count
FROM play_events pe
JOIN track_facets tf ON tf.track_id = pe.track_id
WHERE pe.user_id = $1
  AND tf.bpm IS NOT NULL
GROUP BY band
ORDER BY band ASC;
