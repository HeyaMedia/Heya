-- +goose Up
-- scan_run_artifacts became a write-only ghost table: the queue pipeline stopped
-- writing it (OmitResultArtifacts) and nothing ever read it back — the live
-- resume path uses scanner_entity_artifacts. It only ballooned (5.9GB in TOAST on
-- prod) from the brief window the queue did write full Result blobs into it. All
-- code references are gone; drop the table.
DROP TABLE IF EXISTS public.scan_run_artifacts;

-- +goose Down
-- One-way drop, intentionally not reversible. Every reader/writer of
-- scan_run_artifacts was deleted in the same change, so recreating an empty
-- table would only resurrect dead schema with no code to use it. Consistent
-- with "no backwards-compat shims while in active dev" (CLAUDE.md).
SELECT 1;
