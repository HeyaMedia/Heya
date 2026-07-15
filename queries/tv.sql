-- name: CreateTVSeries :one
INSERT INTO tv_series (media_item_id, status, genres, rating, first_air_date, last_air_date,
    original_name, original_language, number_of_seasons, number_of_episodes,
    popularity, spoken_languages, origin_country)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (media_item_id) DO NOTHING
RETURNING *;

-- name: GetTVSeriesByMediaItemID :one
SELECT * FROM tv_series WHERE media_item_id = $1;

-- name: UpdateTVSeries :one
UPDATE tv_series
SET status = $2, genres = $3,
    rating = $4, first_air_date = $5, last_air_date = $6,
    original_name = $7, original_language = $8,
    number_of_seasons = $9, number_of_episodes = $10,
    popularity = $11, spoken_languages = $12, origin_country = $13
WHERE id = $1
RETURNING *;

-- name: CreateTVSeason :one
-- DO NOTHING preserves an existing season (incl. user edits) on re-enrich; the
-- caller recovers its id via GetTVSeason on the resulting ErrNoRows so new
-- episodes can still be attached. New seasons insert normally.
INSERT INTO tv_seasons (series_id, season_number, title, overview, poster_path, air_date, end_date, status, aired_episodes, external_ids)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (series_id, season_number) DO NOTHING
RETURNING *;

-- name: ListTVSeasonsBySeries :many
SELECT * FROM tv_seasons WHERE series_id = $1 ORDER BY season_number ASC;

-- name: GetTVSeason :one
SELECT * FROM tv_seasons WHERE series_id = $1 AND season_number = $2;

-- name: GetTVSeasonByID :one
SELECT * FROM tv_seasons WHERE id = $1;

-- name: UpdateTVSeason :one
UPDATE tv_seasons
SET title = $2, overview = $3, poster_path = $4, air_date = $5,
    end_date = $6, status = $7, aired_episodes = $8, external_ids = $9
WHERE id = $1
RETURNING *;

-- name: GetTVSeriesByID :one
SELECT * FROM tv_series WHERE id = $1;

-- name: CreateTVEpisode :one
-- DO NOTHING: episodes are insert-or-preserve. On re-enrich an existing episode
-- (possibly user-edited) returns ErrNoRows and the caller skips it; new episodes
-- insert. Episode-field refresh is deferred (episodes have no provenance column).
INSERT INTO tv_episodes (season_id, episode_number, title, overview, still_path, runtime_minutes, air_date, rating, absolute_number, is_special, episode_type, external_ids, source)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (season_id, episode_number) DO NOTHING
RETURNING *;

-- name: ListTVEpisodesBySeason :many
SELECT * FROM tv_episodes WHERE season_id = $1 ORDER BY episode_number ASC;

-- name: GetTVEpisode :one
SELECT * FROM tv_episodes WHERE season_id = $1 AND episode_number = $2;

-- name: GetTVEpisodeByID :one
SELECT * FROM tv_episodes WHERE id = $1;

-- name: UpdateTVEpisode :one
UPDATE tv_episodes
SET title = $2, overview = $3, still_path = $4, runtime_minutes = $5, air_date = $6, rating = $7,
    absolute_number = $8, is_special = $9, episode_type = $10, external_ids = $11, source = $12
WHERE id = $1
RETURNING *;

-- name: ListTVEpisodesBySeries :many
-- Whole-series episode fetch for the detail page — one query instead of one
-- ListTVEpisodesBySeason per season. Ordered so the caller can group by season.
SELECT e.* FROM tv_episodes e
JOIN tv_seasons s ON s.id = e.season_id
WHERE s.series_id = $1
ORDER BY s.season_number ASC, e.episode_number ASC;

-- name: ListCanonicalTVEpisodeRowsBySeries :many
-- Canonically bound rows are compared with the current HeyaMetadata projection
-- after files have been relinked. Rows absent from (or moved within) the current
-- projection are old provider layouts and can then be removed safely.
SELECT e.id, s.season_number, e.episode_number, b.entity_id
FROM tv_episodes e
JOIN tv_seasons s ON s.id = e.season_id
JOIN metadata_entity_bindings b
  ON b.local_kind = 'tv_episode' AND b.local_id = e.id
WHERE s.series_id = $1
ORDER BY s.season_number, e.episode_number;

-- name: DeleteCanonicalTVEpisodesByIDs :execrows
WITH deleted_bindings AS (
    DELETE FROM metadata_entity_bindings
    WHERE local_kind = 'tv_episode'
      AND local_id = ANY(sqlc.arg(episode_ids)::bigint[])
    RETURNING local_id
)
DELETE FROM tv_episodes
WHERE id = ANY(sqlc.arg(episode_ids)::bigint[]);

-- name: ListCanonicalTVSeasonRowsBySeries :many
SELECT s.id, s.season_number, b.entity_id
FROM tv_seasons s
JOIN metadata_entity_bindings b
  ON b.local_kind = 'tv_season' AND b.local_id = s.id
WHERE s.series_id = $1
ORDER BY s.season_number;

-- name: DeleteEmptyCanonicalTVSeasonsByIDs :execrows
WITH empty_seasons AS MATERIALIZED (
    SELECT s.id
    FROM tv_seasons s
    WHERE s.id = ANY(sqlc.arg(season_ids)::bigint[])
      AND NOT EXISTS (SELECT 1 FROM tv_episodes e WHERE e.season_id = s.id)
), deleted_bindings AS (
    DELETE FROM metadata_entity_bindings b
    USING empty_seasons stale
    WHERE b.local_kind = 'tv_season' AND b.local_id = stale.id
    RETURNING b.local_id
)
DELETE FROM tv_seasons s
USING empty_seasons stale
WHERE s.id = stale.id;

-- name: ListEpisodeAbsoluteMap :many
-- Absolute-number -> (season, episode) resolution for one series. Powers the
-- read-time remap of absolute-numbered anime files: their parse_result carries
-- an absolute episode with no season ("Series - 24 - Title"), and this maps that
-- 24 back onto its real season/episode via the enriched catalog.
--
-- Specials are excluded (season 0 / is_special): the absolute run of a series
-- covers only its main seasons, and providers sometimes stamp a non-zero
-- absolute_number on a special — without this guard an absolute file could
-- remap onto that special.
SELECT s.season_number, e.episode_number, e.absolute_number
FROM tv_episodes e
JOIN tv_seasons s ON s.id = e.season_id
JOIN tv_series ts ON ts.id = s.series_id
WHERE ts.media_item_id = $1
  AND e.absolute_number > 0
  AND s.season_number > 0
  AND NOT e.is_special;

-- name: ListEpisodeNumbersForMediaItems :many
-- Season/episode numbers for many series at once — the catalog side of
-- presentEpisodeTotals (the file side is ListEpisodeFileParses).
SELECT ts.media_item_id, s.season_number, e.episode_number
FROM tv_episodes e
JOIN tv_seasons s ON s.id = e.season_id
JOIN tv_series ts ON ts.id = s.series_id
WHERE ts.media_item_id = ANY(sqlc.arg(media_item_ids)::bigint[]);

-- name: PersistTVStructure :one
-- Persist an entire canonical season/episode projection in one PostgreSQL
-- statement. Existing season and episode rows are intentionally preserved so
-- local/user edits keep winning; localized titles and overviews remain
-- replaceable projections. The RETURNING unions make newly inserted rows
-- available to later CTEs without thousands of client/server round trips.
WITH season_input AS MATERIALIZED (
    SELECT DISTINCT ON ((value->>'season_number')::integer)
           (value->>'season_number')::integer AS season_number,
           COALESCE(value->>'title', '') AS title,
           COALESCE(value->>'overview', '') AS overview,
           COALESCE(value->>'poster_path', '') AS poster_path,
           COALESCE(value->>'air_date', '') AS air_date,
           COALESCE(value->>'end_date', '') AS end_date,
           COALESCE(value->>'status', '') AS status,
           COALESCE((value->>'aired_episodes')::integer, 0) AS aired_episodes,
           COALESCE(value->'external_ids', '{}'::jsonb) AS external_ids,
           COALESCE(value->>'canonical_id', '') AS canonical_id
    FROM jsonb_array_elements(sqlc.arg(seasons)::jsonb) AS value
), inserted_seasons AS (
    INSERT INTO tv_seasons (
        series_id, season_number, title, overview, poster_path, air_date,
        end_date, status, aired_episodes, external_ids
    )
    SELECT sqlc.arg(series_id), season_number, title, overview, poster_path,
           NULLIF(air_date, '')::date, NULLIF(end_date, '')::date, status,
           aired_episodes, COALESCE(external_ids, '{}'::jsonb)
    FROM season_input
    ON CONFLICT (series_id, season_number) DO NOTHING
    RETURNING id, season_number
), all_seasons AS MATERIALIZED (
    SELECT id, season_number FROM inserted_seasons
    UNION ALL
    SELECT season.id, season.season_number
    FROM tv_seasons season
    JOIN season_input input USING (season_number)
    WHERE season.series_id = sqlc.arg(series_id)
      AND NOT EXISTS (
          SELECT 1 FROM inserted_seasons inserted WHERE inserted.season_number = season.season_number
      )
), season_bindings AS (
    INSERT INTO metadata_entity_bindings (
        local_kind, local_id, entity_id, entity_kind, schema_version, projection_version
    )
    SELECT 'tv_season', season.id, input.canonical_id::uuid, 'season',
           sqlc.arg(schema_version), sqlc.arg(projection_version)
    FROM all_seasons season
    JOIN season_input input USING (season_number)
    WHERE NULLIF(input.canonical_id, '') IS NOT NULL
    ON CONFLICT (local_kind, local_id) DO UPDATE SET
        entity_id = EXCLUDED.entity_id,
        entity_kind = EXCLUDED.entity_kind,
        schema_version = EXCLUDED.schema_version,
        projection_version = CASE
            WHEN metadata_entity_bindings.entity_id = EXCLUDED.entity_id
              THEN GREATEST(metadata_entity_bindings.projection_version, EXCLUDED.projection_version)
            ELSE EXCLUDED.projection_version
        END,
        updated_at = now()
    RETURNING 1
), episode_input AS MATERIALIZED (
    SELECT DISTINCT ON (
               (value->>'season_number')::integer,
               (value->>'episode_number')::integer
           )
           (value->>'season_number')::integer AS season_number,
           (value->>'episode_number')::integer AS episode_number,
           COALESCE(value->>'title', '') AS title,
           COALESCE(value->>'overview', '') AS overview,
           COALESCE(value->>'still_path', '') AS still_path,
           COALESCE((value->>'runtime_minutes')::integer, 0) AS runtime_minutes,
           COALESCE(value->>'air_date', '') AS air_date,
           COALESCE((value->>'rating')::double precision, 0) AS rating,
           COALESCE((value->>'absolute_number')::integer, 0) AS absolute_number,
           COALESCE((value->>'is_special')::boolean, false) AS is_special,
           COALESCE((value->>'episode_type')::integer, 0) AS episode_type,
           COALESCE(value->'external_ids', '{}'::jsonb) AS external_ids,
           COALESCE(value->>'source', '') AS source,
           COALESCE(value->>'canonical_id', '') AS canonical_id
    FROM jsonb_array_elements(sqlc.arg(episodes)::jsonb) AS value
), inserted_episodes AS (
    INSERT INTO tv_episodes (
        season_id, episode_number, title, overview, still_path,
        runtime_minutes, air_date, rating, absolute_number, is_special,
        episode_type, external_ids, source
    )
    SELECT season.id, input.episode_number, input.title, input.overview,
           input.still_path, input.runtime_minutes, NULLIF(input.air_date, '')::date,
           input.rating, input.absolute_number, input.is_special,
           input.episode_type, COALESCE(input.external_ids, '{}'::jsonb), input.source
    FROM episode_input input
    JOIN all_seasons season USING (season_number)
    ON CONFLICT (season_id, episode_number) DO NOTHING
    RETURNING id, season_id, episode_number
), all_episodes AS MATERIALIZED (
    SELECT inserted.id, season.season_number, inserted.episode_number
    FROM inserted_episodes inserted
    JOIN all_seasons season ON season.id = inserted.season_id
    UNION ALL
    SELECT episode.id, season.season_number, episode.episode_number
    FROM tv_episodes episode
    JOIN all_seasons season ON season.id = episode.season_id
    JOIN episode_input input
      ON input.season_number = season.season_number
     AND input.episode_number = episode.episode_number
    WHERE NOT EXISTS (
        SELECT 1 FROM inserted_episodes inserted WHERE inserted.id = episode.id
    )
), episode_bindings AS (
    INSERT INTO metadata_entity_bindings (
        local_kind, local_id, entity_id, entity_kind, schema_version, projection_version
    )
    SELECT 'tv_episode', episode.id, input.canonical_id::uuid, 'episode',
           sqlc.arg(schema_version), sqlc.arg(projection_version)
    FROM all_episodes episode
    JOIN episode_input input USING (season_number, episode_number)
    WHERE NULLIF(input.canonical_id, '') IS NOT NULL
    ON CONFLICT (local_kind, local_id) DO UPDATE SET
        entity_id = EXCLUDED.entity_id,
        entity_kind = EXCLUDED.entity_kind,
        schema_version = EXCLUDED.schema_version,
        projection_version = CASE
            WHEN metadata_entity_bindings.entity_id = EXCLUDED.entity_id
              THEN GREATEST(metadata_entity_bindings.projection_version, EXCLUDED.projection_version)
            ELSE EXCLUDED.projection_version
        END,
        updated_at = now()
    RETURNING 1
), title_input AS MATERIALIZED (
    SELECT DISTINCT ON (
               (value->>'season_number')::integer,
               (value->>'episode_number')::integer,
               COALESCE(value->>'language', '')
           )
           (value->>'season_number')::integer AS season_number,
           (value->>'episode_number')::integer AS episode_number,
           COALESCE(value->>'title', '') AS title,
           COALESCE(value->>'language', '') AS language,
           COALESCE(value->>'source', '') AS source
    FROM jsonb_array_elements(sqlc.arg(titles)::jsonb) AS value
), written_titles AS (
    INSERT INTO episode_titles (episode_id, title, language, source)
    SELECT episode.id, input.title, input.language, input.source
    FROM title_input input
    JOIN all_episodes episode USING (season_number, episode_number)
    ON CONFLICT (episode_id, language) DO UPDATE SET
        title = EXCLUDED.title,
        source = EXCLUDED.source
    RETURNING 1
), overview_input AS MATERIALIZED (
    SELECT DISTINCT ON (
               (value->>'season_number')::integer,
               (value->>'episode_number')::integer,
               COALESCE(value->>'language', '')
           )
           (value->>'season_number')::integer AS season_number,
           (value->>'episode_number')::integer AS episode_number,
           COALESCE(value->>'language', '') AS language,
           COALESCE(value->>'overview', '') AS overview
    FROM jsonb_array_elements(sqlc.arg(overviews)::jsonb) AS value
), written_overviews AS (
    INSERT INTO episode_overviews (episode_id, language, overview)
    SELECT episode.id, input.language, input.overview
    FROM overview_input input
    JOIN all_episodes episode USING (season_number, episode_number)
    ON CONFLICT (episode_id, language) DO UPDATE SET overview = EXCLUDED.overview
    RETURNING 1
)
SELECT
    (SELECT count(*) FROM all_seasons)::bigint AS seasons,
    (SELECT count(*) FROM all_episodes)::bigint AS episodes,
    (SELECT count(*) FROM season_bindings)::bigint AS season_bindings,
    (SELECT count(*) FROM episode_bindings)::bigint AS episode_bindings,
    (SELECT count(*) FROM written_titles)::bigint AS titles,
    (SELECT count(*) FROM written_overviews)::bigint AS overviews;
