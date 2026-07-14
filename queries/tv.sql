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
