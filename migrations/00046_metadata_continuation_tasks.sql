-- +goose Up

ALTER TABLE public.scanner_metadata_continuations
    ADD COLUMN scheduled_task_id text NOT NULL DEFAULT '';

CREATE INDEX scanner_metadata_continuations_task_idx
    ON public.scanner_metadata_continuations (scheduled_task_id, kind)
    WHERE scheduled_task_id <> '';

-- +goose Down

DROP INDEX IF EXISTS public.scanner_metadata_continuations_task_idx;
ALTER TABLE public.scanner_metadata_continuations DROP COLUMN IF EXISTS scheduled_task_id;
