-- +goose Up
-- Query-performance pass (2026-07): denormalized video_height + missing
-- indexes. Findings verified with EXPLAIN (ANALYZE, BUFFERS) against the
-- production dataset (673k library_files / 240k tracks / 51k albums).

-- Movies/TV browse resolution badges dug the video stream height out of the
-- full ffprobe media_info jsonb per row at request time — a seq scan plus
-- ~71k TOAST detoasts costing ~1.5s per TV browse. Store the height once at
-- probe time instead (UpdateLibraryFileMediaInfo derives it; the UpsertLibraryFile
-- conflict branch resets it alongside media_info). ADD COLUMN with a constant
-- default is metadata-only (no table rewrite); the backfill touches only the
-- ~72k probed rows.
ALTER TABLE library_files ADD COLUMN IF NOT EXISTS video_height int NOT NULL DEFAULT 0;

UPDATE library_files
SET video_height = COALESCE(
      (SELECT (s->>'height')::int
       FROM jsonb_array_elements(media_info->'streams') AS s
       WHERE s->>'codec_type' = 'video'
       LIMIT 1), 0)
WHERE media_info <> '{}'::jsonb AND media_info <> 'null'::jsonb;

-- Mandatory pairing for ListMediaResolutions: without it the planner still
-- seq-scans (a browse page's ANY(ids) array is ~40% of all media_item_ids and
-- gets overestimated). With it: index-only scan, ~12ms.
CREATE INDEX IF NOT EXISTS idx_library_files_media_item_height
  ON library_files (media_item_id, video_height)
  WHERE deleted_at IS NULL;

-- Music-home "more from label" shelf filtered lower(label) across all 51k
-- albums (~90-130ms). No partial predicate: predtest can't prove label <> ''
-- from lower(label) = const, so a partial index is never chosen.
CREATE INDEX IF NOT EXISTS idx_albums_lower_label ON albums (lower(label));

-- "On this day" shelf: month/day anniversary matches were a full filter over
-- all albums. release_date as the third key column serves the ORDER BY
-- release_date DESC directly (sort-free backward scan under the equality).
CREATE INDEX IF NOT EXISTS idx_albums_release_month_day
  ON albums (EXTRACT(MONTH FROM release_date), EXTRACT(DAY FROM release_date), release_date)
  WHERE release_date IS NOT NULL;

-- Time-travel station filters albums by substring(year FROM 1 FOR 4)::int
-- (year is text). The expression index both serves the range predicate and
-- gives ANALYZE expression stats that fix a 1-vs-2000 row misestimate.
CREATE INDEX IF NOT EXISTS idx_albums_year_prefix
  ON albums ((substring(year FROM 1 FOR 4)::int))
  WHERE year ~ '^[0-9]{4}';

-- People autocomplete: trigram GIN costs a fixed ~17ms probe per keystroke
-- for pure-prefix LIKE. A text_pattern_ops btree on the searched expression
-- serves lower(name) LIKE 'x%' as a range scan (~1ms). idx_people_name_trgm
-- stays — quick-search similarity (%) still needs it.
CREATE INDEX IF NOT EXISTS idx_people_lower_name ON people (lower(name) text_pattern_ops);

-- +goose Down
DROP INDEX IF EXISTS idx_people_lower_name;
DROP INDEX IF EXISTS idx_albums_year_prefix;
DROP INDEX IF EXISTS idx_albums_release_month_day;
DROP INDEX IF EXISTS idx_albums_lower_label;
DROP INDEX IF EXISTS idx_library_files_media_item_height;
ALTER TABLE library_files DROP COLUMN IF EXISTS video_height;
