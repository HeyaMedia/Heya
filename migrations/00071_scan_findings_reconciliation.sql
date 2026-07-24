-- +goose Up
-- Scan findings move from resolve-and-reinsert churn to set-reconciliation
-- (2026-07-24 audit: 2.5M resolved copies of ~120k live findings piled up in
-- two weeks — every scan cycle resolved and recreated the identical open
-- finding, 4.4GB of dead evidence nothing ever read).
--
-- The new persistence flow inserts drafts with ON CONFLICT DO NOTHING
-- against this partial unique natural key, so an unchanged finding is zero
-- row writes and keeps its original created_at ("broken since"). Findings
-- that stop being drafted are swept to resolved, and resolved rows are
-- evidence with a TTL — the cleanup_scanner_artifacts task prunes them
-- after 14 days.

-- The open set can hold duplicates under the natural key from the churn
-- era (same finding inserted by racing scans). Keep the newest, resolve
-- the rest, so the unique index can build.
WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY library_id, media_type, coalesce(identity_id, 0), code,
                            md5(rel_path), md5(message)
               ORDER BY id DESC
           ) AS rn
    FROM scan_findings
    WHERE resolved_at IS NULL
)
UPDATE scan_findings sf
SET resolved_at = now()
FROM ranked
WHERE ranked.id = sf.id AND ranked.rn > 1;

CREATE UNIQUE INDEX IF NOT EXISTS idx_scan_findings_open_key
    ON scan_findings (library_id, media_type, coalesce(identity_id, 0::bigint), code,
                      md5(rel_path), md5(message))
    WHERE resolved_at IS NULL;

-- The TTL prune deletes by resolved_at; without this each batch would seq
-- scan the whole table.
CREATE INDEX IF NOT EXISTS idx_scan_findings_resolved_at
    ON scan_findings (resolved_at)
    WHERE resolved_at IS NOT NULL;

-- 00070's rel_path index served ResolveMatchingOpenUnscopedScanFinding,
-- which reconciliation retires — the open-key index above is the arbiter
-- that replaces it.
DROP INDEX IF EXISTS idx_scan_findings_unresolved_path;

-- +goose Down
CREATE INDEX IF NOT EXISTS idx_scan_findings_unresolved_path
    ON scan_findings (library_id, rel_path)
    WHERE resolved_at IS NULL;
DROP INDEX IF EXISTS idx_scan_findings_resolved_at;
DROP INDEX IF EXISTS idx_scan_findings_open_key;
