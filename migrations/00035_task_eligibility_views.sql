-- +goose Up

-- Task-eligibility views: the single source of truth for "which files need
-- trickplay" / "which extras need thumbnails". Before these, the predicate was
-- hand-copied across service/tasks.go (stats + item listings) and
-- worker/kickoff_workers.go (enqueue) — four raw-SQL copies per task that could
-- (and did) threaten to drift, making the Settings count disagree with what the
-- kickoff actually enqueues. Every consumer now reads the view via sqlc.

CREATE OR REPLACE VIEW trickplay_eligible_files AS
SELECT lf.id, lf.path, lf.has_trickplay
FROM library_files lf
JOIN libraries l ON l.id = lf.library_id
WHERE lf.deleted_at IS NULL
  AND lf.status = 'matched'
  AND lf.media_info IS NOT NULL
  AND lf.media_info->'streams' @> '[{"codec_type":"video"}]'
  AND l.settings->>'enable_trickplay' = 'true';

CREATE OR REPLACE VIEW thumbnail_eligible_extras AS
SELECT me.id, me.title, me.file_path, me.thumbnail_path, me.extra_type,
       mi.title AS media_title
FROM media_extras me
JOIN media_items mi ON mi.id = me.media_item_id
JOIN libraries l ON l.id = mi.library_id
WHERE me.file_path != ''
  AND l.settings->>'generate_thumbnails' = 'true';

-- +goose Down
DROP VIEW IF EXISTS thumbnail_eligible_extras;
DROP VIEW IF EXISTS trickplay_eligible_files;
