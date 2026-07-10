-- +goose Up
-- Supports the scan-progress emitter's latest-row-per-identity scan
-- (DISTINCT ON (library_id, identity_key) ... ORDER BY updated_at DESC,
-- id DESC): the index order matches, so the 2s ticker streams instead of
-- sorting the whole table. INCLUDE(status) makes it index-only.
CREATE INDEX IF NOT EXISTS idx_scanner_entities_latest
    ON public.scanner_entities (library_id, identity_key, updated_at DESC, id DESC)
    INCLUDE (status);

-- +goose Down
DROP INDEX IF EXISTS public.idx_scanner_entities_latest;
