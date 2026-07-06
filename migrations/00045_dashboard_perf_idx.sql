-- +goose Up

-- Home dashboard "Recently Added" rails for movies and books start from a
-- per-type top-N ordered by created_at (ListMediaItemsByTypeRecent). The only
-- media_type index was single-column, so the planner filtered on media_type
-- and then sorted every matching row of that type just to take 20. This
-- composite serves the equality filter, the ORDER BY, and the LIMIT as one
-- ordered index scan.
CREATE INDEX IF NOT EXISTS idx_media_items_type_created
    ON media_items (media_type, created_at DESC, id DESC);

-- Music-home's recently-added shelf joins the newest ~2000 library_files to
-- track_files on library_file_id, but track_files was only indexed by track_id
-- and quality — so that join hashed the whole table (~240k rows) on every
-- dashboard load. Index the join key so it becomes a bounded nested-loop
-- lookup driven by the small recent-files set.
CREATE INDEX IF NOT EXISTS idx_track_files_library_file
    ON track_files (library_file_id);

-- +goose Down
DROP INDEX IF EXISTS idx_track_files_library_file;
DROP INDEX IF EXISTS idx_media_items_type_created;
