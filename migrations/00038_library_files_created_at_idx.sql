-- +goose Up

-- The home-page "recently added" rails start from a top-N of library_files
-- ordered by created_at. Without an ordered index the planner has to sort the
-- whole live set (~150k rows) on every rail load; with it the top-N is a
-- backward index scan. Partial on the live set — the rails never surface
-- soft-deleted files.
CREATE INDEX IF NOT EXISTS idx_library_files_created_at
    ON library_files (created_at DESC)
    WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_library_files_created_at;
