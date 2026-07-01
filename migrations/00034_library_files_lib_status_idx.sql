-- +goose Up

-- Composite index for the hottest library_files scan path. Every scan/kickoff
-- query filters (library_id, status): ListLibraryFilesByStatus,
-- ListUnprobedProbeableFiles, ListRetryableUnmatchedFiles, plus the pending
-- enqueue. With 600k+ rows in a single library the existing single-column
-- idx_library_files_library_id forces the planner to walk ~all of a library's
-- rows and then filter by status; a composite lets it seek straight to the
-- (library, status) slice. Partial on the live set — soft-deleted rows are never
-- in these predicates.
CREATE INDEX IF NOT EXISTS idx_library_files_lib_status
    ON library_files (library_id, status)
    WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_library_files_lib_status;
