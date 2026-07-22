-- name: UpsertWatchProgress :one
INSERT INTO user_watch_progress (user_id, entity_type, entity_id, progress_seconds, total_seconds, completed, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, now())
ON CONFLICT (user_id, entity_type, entity_id) DO UPDATE SET
    progress_seconds = EXCLUDED.progress_seconds,
    total_seconds = EXCLUDED.total_seconds,
    completed = EXCLUDED.completed,
    updated_at = now()
RETURNING *;

-- name: GetWatchProgress :one
SELECT * FROM user_watch_progress
WHERE user_id = $1 AND entity_type = $2 AND entity_id = $3;

-- name: MarkEpisodeWatched :exec
INSERT INTO user_watch_progress (user_id, entity_type, entity_id, completed, updated_at)
VALUES ($1, 'episode', $2, true, now())
ON CONFLICT (user_id, entity_type, entity_id) DO UPDATE SET completed = true, updated_at = now();

-- name: UnmarkEpisodeWatched :exec
DELETE FROM user_watch_progress WHERE user_id = $1 AND entity_type = 'episode' AND entity_id = $2;

-- name: MarkEpisodesWatched :exec
-- Bulk mark a specific set of episodes watched. Season/show bulk-mark resolves
-- to only the episodes we actually hold a file for (see presentEpisodeIDs), so
-- unaired catalog episodes are never pre-marked — otherwise a later-arriving
-- episode would show as already watched.
INSERT INTO user_watch_progress (user_id, entity_type, entity_id, completed, updated_at)
SELECT $1, 'episode', eid, true, now()
FROM unnest($2::bigint[]) AS eid
ON CONFLICT (user_id, entity_type, entity_id) DO UPDATE SET completed = true, updated_at = now();

-- name: IsEpisodeWatched :one
SELECT EXISTS(
  SELECT 1 FROM user_watch_progress WHERE user_id = $1 AND entity_type = 'episode' AND entity_id = $2 AND completed = true
) AS watched;

-- name: UnmarkSeasonWatched :exec
DELETE FROM user_watch_progress
WHERE user_id = $1 AND entity_type = 'episode'
AND entity_id IN (SELECT id FROM tv_episodes WHERE season_id = $2);

-- name: UnmarkShowWatched :exec
DELETE FROM user_watch_progress
WHERE user_id = $1 AND entity_type = 'episode' AND entity_id IN (
  SELECT e.id FROM tv_episodes e
  JOIN tv_seasons s ON s.id = e.season_id
  JOIN tv_series ts ON ts.id = s.series_id
  WHERE ts.media_item_id = $2
);

-- name: MarkMovieWatched :exec
INSERT INTO user_watch_progress (user_id, entity_type, entity_id, completed, updated_at)
VALUES ($1, 'movie', $2, true, now())
ON CONFLICT (user_id, entity_type, entity_id) DO UPDATE SET completed = true, updated_at = now();

-- name: UnmarkMovieWatched :exec
DELETE FROM user_watch_progress WHERE user_id = $1 AND entity_type = 'movie' AND entity_id = $2;

-- name: ListFavoritedMediaItemIDs :many
SELECT entity_id FROM user_favorites
WHERE user_id = $1 AND entity_type = 'media_item';

-- Continue watching: incomplete progress across movies and episodes
-- name: ListContinueWatching :many
SELECT wp.id, wp.entity_type, wp.entity_id, wp.progress_seconds, wp.total_seconds, wp.updated_at,
       COALESCE(mi.id, ep_mi.id) AS media_item_id,
       COALESCE(mi.public_id, ep_mi.public_id) AS media_item_public_id,
       COALESCE(mi.library_id, ep_mi.library_id) AS library_id,
       COALESCE(mi.title, ep_mi.title) AS title,
       COALESCE(mi.poster_path, ep_mi.poster_path) AS poster_path,
       COALESCE(mi.slug, ep_mi.slug) AS slug,
       COALESCE(mi.media_type, ep_mi.media_type)::text AS media_type,
       ep.episode_number,
       ep.title AS episode_title,
       s.season_number
FROM user_watch_progress wp
LEFT JOIN media_item_cards mi ON wp.entity_type = 'movie' AND mi.id = wp.entity_id
LEFT JOIN tv_episodes ep ON wp.entity_type = 'episode' AND ep.id = wp.entity_id
LEFT JOIN tv_seasons s ON ep.season_id = s.id
LEFT JOIN tv_series ts ON s.series_id = ts.id
LEFT JOIN media_item_cards ep_mi ON ts.media_item_id = ep_mi.id
WHERE wp.user_id = sqlc.arg(user_id) AND wp.completed = false AND wp.progress_seconds > 30
  -- Skip items whose file is missing on disk. For a movie that's any live
  -- file on the media item; for an episode it must be the specific file
  -- matching this season+episode (a sibling episode surviving doesn't make
  -- THIS one playable), mirroring BuildEpisodeFileMap's parse_result match.
  AND (
    (wp.entity_type = 'movie' AND EXISTS (
      SELECT 1 FROM library_files lf
      WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL
    ))
    OR (wp.entity_type = 'episode' AND EXISTS (
      SELECT 1 FROM library_files lf
      WHERE lf.media_item_id = ep_mi.id AND lf.deleted_at IS NULL AND lf.status = 'matched'
        AND lf.parse_result->'parsed'->'release'->'seasons'  @> to_jsonb(s.season_number)
        AND lf.parse_result->'parsed'->'release'->'episodes' @> to_jsonb(ep.episode_number)
    ))
  )
ORDER BY wp.updated_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- Recently watched (completed items). DISTINCT ON dedupes to one row per
-- media item (the newest watch); the outer ORDER BY restores recency order —
-- DISTINCT ON forces the inner sort to lead with the distinct key, which is
-- id order, not watch order. Recency order also makes OFFSET paging walk
-- backwards through watch history, which is what the infinite rail wants.
-- name: ListRecentlyWatched :many
SELECT * FROM (
  SELECT DISTINCT ON (COALESCE(mi.id, ep_mi.id))
         wp.id, wp.entity_type, wp.entity_id, wp.updated_at,
         COALESCE(mi.id, ep_mi.id) AS media_item_id,
         COALESCE(mi.public_id, ep_mi.public_id) AS media_item_public_id,
         COALESCE(mi.library_id, ep_mi.library_id) AS library_id,
         COALESCE(mi.title, ep_mi.title) AS title,
         COALESCE(mi.poster_path, ep_mi.poster_path) AS poster_path,
         COALESCE(mi.slug, ep_mi.slug) AS slug,
         COALESCE(mi.media_type, ep_mi.media_type)::text AS media_type
  FROM user_watch_progress wp
  LEFT JOIN media_item_cards mi ON wp.entity_type = 'movie' AND mi.id = wp.entity_id
  LEFT JOIN tv_episodes ep ON wp.entity_type = 'episode' AND ep.id = wp.entity_id
  LEFT JOIN tv_seasons s ON ep.season_id = s.id
  LEFT JOIN tv_series ts ON s.series_id = ts.id
  LEFT JOIN media_item_cards ep_mi ON ts.media_item_id = ep_mi.id
  WHERE wp.user_id = sqlc.arg(user_id) AND wp.completed = true
  ORDER BY COALESCE(mi.id, ep_mi.id), wp.updated_at DESC
) deduped
ORDER BY deduped.updated_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- Recently watched EPISODES (not deduped to the show) — the TV "Recently
-- Watched" rail shows one tile per episode, each painted with the show's poster
-- (media_item_id) and an "S02E03 · Title" subtitle. Distinct from
-- ListRecentlyWatched, which collapses to one row per media item.
-- name: ListRecentlyWatchedEpisodes :many
SELECT wp.entity_id AS episode_id, wp.updated_at,
       ts.media_item_id,
       ep_mi.public_id AS media_item_public_id,
       ep_mi.library_id,
       ep_mi.title AS series_title,
       ep_mi.slug AS series_slug,
       s.season_number,
       ep.episode_number,
       ep.title AS episode_title
FROM user_watch_progress wp
JOIN tv_episodes ep ON ep.id = wp.entity_id
JOIN tv_seasons s ON s.id = ep.season_id
JOIN tv_series ts ON ts.id = s.series_id
JOIN media_item_cards ep_mi ON ep_mi.id = ts.media_item_id
WHERE wp.user_id = sqlc.arg(user_id) AND wp.entity_type = 'episode' AND wp.completed = true
ORDER BY wp.updated_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- Episode progress for a specific series (for showing progress bars on episode cards)
-- name: ListEpisodeProgressForSeries :many
SELECT wp.entity_id AS episode_id, wp.progress_seconds, wp.total_seconds, wp.completed
FROM user_watch_progress wp
JOIN tv_episodes e ON e.id = wp.entity_id
JOIN tv_seasons s ON s.id = e.season_id
WHERE wp.user_id = $1 AND wp.entity_type = 'episode' AND s.series_id = $2;

-- name: ListSeriesEpisodeRefs :many
-- Every catalog episode for a series' media item with its season number —
-- the shuffle pool source (filtered to held files in Go).
SELECT e.id AS episode_id, e.episode_number, e.title, e.runtime_minutes,
       s.id AS season_id, s.season_number,
       ts.media_item_id
FROM tv_episodes e
JOIN tv_seasons s ON s.id = e.season_id
JOIN tv_series ts ON ts.id = s.series_id
WHERE ts.media_item_id = $1
ORDER BY (CASE WHEN s.season_number = 0 THEN 1 ELSE 0 END), s.season_number ASC, e.episode_number ASC;

-- Next unwatched episode for a series (ordered by season then episode number)
-- name: GetNextUnwatchedEpisode :one
SELECT e.id AS episode_id, e.episode_number, e.title, e.runtime_minutes,
       s.id AS season_id, s.season_number,
       ts.media_item_id
FROM tv_episodes e
JOIN tv_seasons s ON s.id = e.season_id
JOIN tv_series ts ON ts.id = s.series_id
LEFT JOIN user_watch_progress wp ON wp.entity_id = e.id AND wp.entity_type = 'episode' AND wp.completed = true AND wp.user_id = $1
WHERE ts.media_item_id = $2 AND wp.entity_id IS NULL
ORDER BY (CASE WHEN s.season_number = 0 THEN 1 ELSE 0 END), s.season_number ASC, e.episode_number ASC
LIMIT 1;

-- name: ListWatchedEpisodeNumbersForMediaItems :many
-- Season/episode numbers of a user's completed episodes across many series —
-- the watched numerator of presentShowWatchCounts. Numbers (not ids) so the
-- caller can intersect with the parsed file keys the same way the totals do.
SELECT ts.media_item_id, s.season_number, e.episode_number
FROM user_watch_progress wp
JOIN tv_episodes e ON e.id = wp.entity_id
JOIN tv_seasons s ON s.id = e.season_id
JOIN tv_series ts ON ts.id = s.series_id
WHERE wp.user_id = $1 AND wp.entity_type = 'episode' AND wp.completed = true
  AND ts.media_item_id = ANY(sqlc.arg(media_item_ids)::bigint[]);

-- The server-owned Up Next rail: one row per recently-watched series that
-- still has an unwatched episode WITH a matched file — the next episode is
-- the lowest (season, episode) unwatched episode that has a file, skipping
-- catalog episodes the library doesn't hold. Replaces the old FE derivation
-- (first 20 recent titles × one /up-next call each), which went blind when a
-- bulk mark-watched pass filled the recency window with finished shows.
--
-- candidates prefilters to series whose regular-season catalog count exceeds
-- the user's completed count: fully-watched shows never pay the parse_result
-- expansion (the expensive part — multi-KB jsonb detoast per file), and the
-- LIMIT 100 bounds the worst case. Specials (season 0) are never nominated.
-- name: ListUpNextRail :many
WITH watched AS (
  SELECT ts.media_item_id AS mid, ts.id AS series_id,
         count(*) FILTER (WHERE s.season_number > 0) AS watched_regular,
         max(wp.updated_at) AS last_watch
  FROM user_watch_progress wp
  JOIN tv_episodes e ON e.id = wp.entity_id
  JOIN tv_seasons s ON s.id = e.season_id
  JOIN tv_series ts ON ts.id = s.series_id
  WHERE wp.user_id = sqlc.arg(user_id) AND wp.entity_type = 'episode' AND wp.completed = true
  GROUP BY ts.media_item_id, ts.id
),
totals AS (
  SELECT s.series_id, count(*) AS total_regular
  FROM tv_seasons s JOIN tv_episodes e ON e.season_id = s.id
  WHERE s.season_number > 0
  GROUP BY s.series_id
),
candidates AS (
  SELECT w.mid, w.series_id, w.last_watch
  FROM watched w
  JOIN totals t ON t.series_id = w.series_id
  WHERE t.total_regular > w.watched_regular
  ORDER BY w.last_watch DESC
  LIMIT 100
),
file_keys AS (
  SELECT lf.media_item_id AS mid, (sv.val)::int AS season, (ev.val)::int AS ep,
         lf.id AS file_id, lf.public_id AS file_public_id
  FROM library_files lf
  JOIN candidates c ON c.mid = lf.media_item_id
  CROSS JOIN jsonb_array_elements_text(lf.parse_result->'parsed'->'release'->'seasons') sv(val)
  CROSS JOIN jsonb_array_elements_text(lf.parse_result->'parsed'->'release'->'episodes') ev(val)
  WHERE lf.deleted_at IS NULL AND lf.status = 'matched'
),
next_ep AS (
  SELECT DISTINCT ON (c.mid)
      c.mid, e.id AS episode_id, e.episode_number, e.title AS episode_title,
      e.runtime_minutes, s.id AS season_id, s.season_number, fk.file_id, fk.file_public_id
  FROM candidates c
  JOIN tv_seasons s ON s.series_id = c.series_id AND s.season_number > 0
  JOIN tv_episodes e ON e.season_id = s.id
  JOIN file_keys fk ON fk.mid = c.mid AND fk.season = s.season_number AND fk.ep = e.episode_number
  WHERE NOT EXISTS (
    SELECT 1 FROM user_watch_progress wp3
    WHERE wp3.entity_id = e.id AND wp3.entity_type = 'episode' AND wp3.completed = true AND wp3.user_id = sqlc.arg(user_id)
  )
  ORDER BY c.mid, s.season_number ASC, e.episode_number ASC, fk.file_id ASC
)
SELECT n.mid AS media_item_id,
       mic.public_id AS media_item_public_id,
       mic.library_id,
       mic.title,
       mic.slug,
       mic.media_type::text AS media_type,
       n.episode_id, n.episode_number, n.episode_title, n.runtime_minutes,
       n.season_id, n.season_number,
       n.file_id, n.file_public_id,
       c.last_watch::timestamptz AS last_watched_at
FROM next_ep n
JOIN candidates c ON c.mid = n.mid
JOIN media_item_cards mic ON mic.id = n.mid
ORDER BY c.last_watch DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);
