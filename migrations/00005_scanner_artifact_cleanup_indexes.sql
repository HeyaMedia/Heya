-- +goose Up

CREATE INDEX IF NOT EXISTS idx_scanner_entities_search_run_scope
    ON public.scanner_entities USING btree (search_scan_run_id, scope_key)
    WHERE search_scan_run_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_scanner_entities_fetch_run_scope
    ON public.scanner_entities USING btree (fetch_scan_run_id, scope_key)
    WHERE fetch_scan_run_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_scanner_entity_artifacts_scan_run_entity
    ON public.scanner_entity_artifacts USING btree (scan_run_id, entity_id)
    WHERE scan_run_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_scanner_entities_library_media_scope_status
    ON public.scanner_entities USING btree (library_id, media_type, scope_key, status);

-- +goose Down

DROP INDEX IF EXISTS public.idx_scanner_entities_library_media_scope_status;
DROP INDEX IF EXISTS public.idx_scanner_entity_artifacts_scan_run_entity;
DROP INDEX IF EXISTS public.idx_scanner_entities_fetch_run_scope;
DROP INDEX IF EXISTS public.idx_scanner_entities_search_run_scope;
