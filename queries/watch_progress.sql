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
WHERE wp.user_id = $1 AND wp.completed = false AND wp.progress_seconds > 30
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
LIMIT 20;

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
