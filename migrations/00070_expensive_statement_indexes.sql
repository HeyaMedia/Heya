-- +goose Up
-- Index-coverage pass from the 2026-07-24 prod pg_stat_statements audit
-- (sampled ~40 min of workload right after the PG 18 upgrade):
--
-- 1. ResolveMatchingOpenUnscopedScanFinding was the #1 statement by total
--    time (644s / 4.2k calls / 155ms mean — ~27% of a core during scans).
--    The best matching index, (library_id, code) WHERE resolved_at IS NULL,
--    stops at code, and the music library holds 119k unresolved
--    music_track_issue findings under that one prefix — every resolve
--    walked all of them (~130k buffers) just to filter on rel_path/message.
--    (library_id, rel_path) narrows straight to the handful of findings on
--    one path; the remaining filters are cheap on that.
--
-- 2. The "first added" rail queries (MIN(created_at) GROUP BY media_item_id
--    over an id array, for movies and for album shelves via track_files)
--    chose a 1.5s parallel seq scan over the fat library_files heap because
--    idx_library_files_media_item_id couldn't serve created_at. Widening it
--    to (media_item_id, created_at) makes those index-only; the prefix keeps
--    serving every existing media_item_id lookup, so the old index goes.
--
CREATE INDEX IF NOT EXISTS idx_scan_findings_unresolved_path
    ON scan_findings (library_id, rel_path)
    WHERE resolved_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_library_files_media_item_created
    ON library_files (media_item_id, created_at);
DROP INDEX IF EXISTS idx_library_files_media_item_id;

-- +goose Down
DROP INDEX IF EXISTS idx_scan_findings_unresolved_path;
CREATE INDEX IF NOT EXISTS idx_library_files_media_item_id
    ON library_files (media_item_id);
DROP INDEX IF EXISTS idx_library_files_media_item_created;
