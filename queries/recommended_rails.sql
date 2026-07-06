-- Personalized "Recommended" landing rails for the Movies / TV sections.
-- Every query here is read-only against existing tables (no schema changes).
-- Rows share a common projection — id, title, slug, year, media_type, rating —
-- so the service maps them all through one RecItem builder. Availability is
-- gated inside the query (a live, non-deleted library_file) so a "watch this"
-- rail never surfaces an owned-but-missing title.

-- ── Movies ──────────────────────────────────────────────────────────────

-- Recently released films we own — ordered by the film's own release date, not
-- when the file landed (that's "Recently Added"). Only dated, already-released.
-- name: ListRecentlyReleasedMovies :many
SELECT mi.id, mi.library_id, mi.title, mi.slug, mi.year, mi.media_type::text AS media_type, m.rating
FROM media_items mi
JOIN movies m ON m.media_item_id = mi.id
WHERE mi.media_type = 'movie'
  AND m.release_date IS NOT NULL
  AND m.release_date <= now()::date
  AND EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL)
ORDER BY m.release_date DESC, m.popularity DESC
LIMIT $1;

-- Highly-rated films the user hasn't finished yet.
-- name: ListTopUnwatchedMovies :many
SELECT mi.id, mi.library_id, mi.title, mi.slug, mi.year, mi.media_type::text AS media_type, m.rating
FROM media_items mi
JOIN movies m ON m.media_item_id = mi.id
WHERE mi.media_type = 'movie'
  AND m.rating > 0
  AND EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL)
  AND NOT EXISTS (
    SELECT 1 FROM user_watch_progress wp
    WHERE wp.user_id = $1 AND wp.entity_type = 'movie' AND wp.entity_id = mi.id AND wp.completed = true
  )
ORDER BY m.rating DESC, m.popularity DESC
LIMIT $2;

-- The genres the user actually finishes, most-watched first — the seed for the
-- personalized "More <Genre>" movie rail.
-- name: ListTopWatchedMovieGenres :many
SELECT g::text AS genre, count(*)::int AS cnt
FROM user_watch_progress wp
JOIN movies m ON m.media_item_id = wp.entity_id
CROSS JOIN LATERAL unnest(m.genres) AS g
WHERE wp.user_id = $1 AND wp.entity_type = 'movie' AND wp.completed = true
GROUP BY g
ORDER BY count(*) DESC, g
LIMIT 5;

-- Top-rated unseen films in a genre (the payload for "More <Genre>").
-- name: ListTopMoviesInGenreUnseen :many
SELECT mi.id, mi.library_id, mi.title, mi.slug, mi.year, mi.media_type::text AS media_type, m.rating
FROM media_items mi
JOIN movies m ON m.media_item_id = mi.id
WHERE mi.media_type = 'movie'
  AND sqlc.arg(genre)::text = ANY(m.genres)
  AND EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL)
  AND NOT EXISTS (
    SELECT 1 FROM user_watch_progress wp
    WHERE wp.user_id = sqlc.arg(user_id) AND wp.entity_type = 'movie' AND wp.entity_id = mi.id AND wp.completed = true
  )
ORDER BY m.rating DESC, m.popularity DESC
LIMIT sqlc.arg(lim);

-- The actors that appear most across the user's finished films (top billing
-- only, to skip bit-part noise) — the seed for the "Starring <Actor>" rail.
-- name: ListTopWatchedMovieActors :many
SELECT p.id AS person_id, p.name, count(*)::int AS cnt
FROM user_watch_progress wp
JOIN media_cast mc ON mc.media_item_id = wp.entity_id AND mc.display_order < 8
JOIN people p ON p.id = mc.person_id
WHERE wp.user_id = $1 AND wp.entity_type = 'movie' AND wp.completed = true
GROUP BY p.id, p.name
ORDER BY count(*) DESC, p.id
LIMIT $2;

-- A given actor's owned, unseen films (payload for "Starring <Actor>").
-- name: ListPersonUnseenMovies :many
SELECT mi.id, mi.library_id, mi.title, mi.slug, mi.year, mi.media_type::text AS media_type, m.rating
FROM media_cast mc
JOIN media_items mi ON mi.id = mc.media_item_id
JOIN movies m ON m.media_item_id = mi.id
WHERE mc.person_id = sqlc.arg(person_id)
  AND mi.media_type = 'movie'
  AND EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL)
  AND NOT EXISTS (
    SELECT 1 FROM user_watch_progress wp
    WHERE wp.user_id = sqlc.arg(user_id) AND wp.entity_type = 'movie' AND wp.entity_id = mi.id AND wp.completed = true
  )
ORDER BY m.rating DESC, m.popularity DESC
LIMIT sqlc.arg(lim);

-- ── TV ──────────────────────────────────────────────────────────────────

-- Highest-rated shows we own.
-- name: ListTopRatedTV :many
SELECT mi.id, mi.library_id, mi.title, mi.slug, mi.year, mi.media_type::text AS media_type, ts.rating
FROM media_items mi
JOIN tv_series ts ON ts.media_item_id = mi.id
WHERE mi.media_type = 'tv'
  AND ts.rating > 0
  AND EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL)
ORDER BY ts.rating DESC, ts.popularity DESC
LIMIT $1;

-- Genres the user watches most across finished episodes — seed for "More
-- <Genre>" on TV.
-- name: ListTopWatchedTVGenres :many
SELECT g::text AS genre, count(*)::int AS cnt
FROM user_watch_progress wp
JOIN tv_episodes e ON e.id = wp.entity_id
JOIN tv_seasons se ON se.id = e.season_id
JOIN tv_series ts ON ts.id = se.series_id
CROSS JOIN LATERAL unnest(ts.genres) AS g
WHERE wp.user_id = $1 AND wp.entity_type = 'episode' AND wp.completed = true
GROUP BY g
ORDER BY count(*) DESC, g
LIMIT 5;

-- Top-rated shows in a genre (payload for TV "More <Genre>").
-- name: ListTopTVInGenre :many
SELECT mi.id, mi.library_id, mi.title, mi.slug, mi.year, mi.media_type::text AS media_type, ts.rating
FROM media_items mi
JOIN tv_series ts ON ts.media_item_id = mi.id
WHERE mi.media_type = 'tv'
  AND sqlc.arg(genre)::text = ANY(ts.genres)
  AND EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL)
ORDER BY ts.rating DESC, ts.popularity DESC
LIMIT sqlc.arg(lim);

-- "Rediscover": shows the user watched a while ago that have since aired
-- episodes newer than their last watch — the signature re-engagement rail.
-- name: ListRediscoverTV :many
WITH watched AS (
  SELECT ts.id AS series_id, ts.media_item_id, max(wp.updated_at) AS last_watched
  FROM user_watch_progress wp
  JOIN tv_episodes e ON e.id = wp.entity_id
  JOIN tv_seasons se ON se.id = e.season_id
  JOIN tv_series ts ON ts.id = se.series_id
  WHERE wp.user_id = $1 AND wp.entity_type = 'episode' AND wp.completed = true
  GROUP BY ts.id, ts.media_item_id
)
SELECT mi.id, mi.library_id, mi.title, mi.slug, mi.year, mi.media_type::text AS media_type
FROM watched w
JOIN media_items mi ON mi.id = w.media_item_id
WHERE w.last_watched < now() - interval '30 days'
  -- The newer episode must be one we actually hold a playable file for — not
  -- just a catalog row. tv_episodes lists every aired episode (owned or not),
  -- so gating on air_date alone would recommend "new episodes" the user can't
  -- play. Match a matched, non-deleted library_file to the episode the same
  -- way ListContinueWatching does (parse_result season+episode on the series'
  -- media item), which also subsumes the "series has any file" check.
  AND EXISTS (
    SELECT 1 FROM tv_episodes e2
    JOIN tv_seasons se2 ON se2.id = e2.season_id
    JOIN library_files lf ON lf.media_item_id = w.media_item_id
      AND lf.deleted_at IS NULL AND lf.status = 'matched'
      AND lf.parse_result->'parsed'->'release'->'seasons'  @> to_jsonb(se2.season_number)
      AND lf.parse_result->'parsed'->'release'->'episodes' @> to_jsonb(e2.episode_number)
    WHERE se2.series_id = w.series_id AND e2.air_date > w.last_watched::date
  )
ORDER BY w.last_watched DESC
LIMIT $2;

-- ── Shared: local TMDB recommendations ──────────────────────────────────

-- Library-wide TMDB "recommended" entries that resolve to an owned item of the
-- given media_type, most-recommended first. Movies the user has finished are
-- dropped; TV keeps its series (episode-keyed progress can't mark a series
-- done here, which is fine for a discovery rail).
-- name: ListLocalRecommendations :many
WITH agg AS (
  SELECT mr.external_ids, count(*)::int AS source_count, max(mr.vote_average) AS vote
  FROM media_recommendations mr
  WHERE mr.media_type = sqlc.arg(rec_type)::text AND mr.external_ids <> '{}'
  GROUP BY mr.external_ids
)
SELECT mi.id, mi.library_id, mi.title, mi.slug, mi.year, mi.media_type::text AS media_type, agg.source_count
FROM agg
JOIN media_items mi ON mi.external_ids @> agg.external_ids
WHERE mi.media_type = sqlc.arg(item_type)::media_type
  AND EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL)
  AND NOT EXISTS (
    SELECT 1 FROM user_watch_progress wp
    WHERE wp.user_id = sqlc.arg(user_id) AND wp.entity_type = 'movie' AND wp.entity_id = mi.id AND wp.completed = true
  )
ORDER BY agg.source_count DESC, agg.vote DESC NULLS LAST
LIMIT sqlc.arg(lim);
