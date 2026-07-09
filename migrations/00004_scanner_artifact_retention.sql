-- +goose Up

CREATE INDEX IF NOT EXISTS idx_scan_run_artifacts_created_at
    ON public.scan_run_artifacts USING btree (created_at);

CREATE INDEX IF NOT EXISTS idx_scanner_entity_artifacts_created_at
    ON public.scanner_entity_artifacts USING btree (created_at);

INSERT INTO public.scheduled_tasks (
    id, display_name, description, category, enabled,
    interval_hours, daily_start_time, daily_end_time, max_runtime_minutes
) VALUES (
    'cleanup_scanner_artifacts',
    'Clean Scanner Artifacts',
    'Prune scanner handoff JSON after applied matches have been materialized.',
    'maintenance',
    true,
    24,
    '03:00',
    '06:00',
    60
)
ON CONFLICT (id) DO NOTHING;

-- +goose Down

DELETE FROM public.scheduled_tasks WHERE id = 'cleanup_scanner_artifacts';

DROP INDEX IF EXISTS public.idx_scanner_entity_artifacts_created_at;
DROP INDEX IF EXISTS public.idx_scan_run_artifacts_created_at;
