-- +goose Up
-- Durable per-library scan-burst totals, maintained by the enqueue helpers
-- (see worker.bumpLibraryScanBurst). Scan progress = units_total − active
-- pipeline jobs: active jobs are undeletable while a scan runs and this
-- total is ours, so progress needs no disposable River history, no emitter
-- memory, and survives restarts, subscriber downtime, job-cleaner pruning,
-- and Cancel-all. The row resets atomically when a unit is enqueued for an
-- idle library (a new burst); one row per library, no cleanup needed.
CREATE TABLE IF NOT EXISTS public.library_scan_bursts (
    library_id bigint PRIMARY KEY REFERENCES public.libraries(id) ON DELETE CASCADE,
    started_at timestamp with time zone NOT NULL DEFAULT now(),
    units_total bigint NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE IF EXISTS public.library_scan_bursts;
