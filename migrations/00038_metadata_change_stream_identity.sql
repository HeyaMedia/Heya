-- +goose Up
ALTER TABLE public.metadata_change_consumers
    ADD COLUMN stream_id uuid;

-- A legacy cursor has no stream identity, so it cannot prove that it belongs
-- to the current HeyaMetadata database. Replay once from zero; refresh jobs and
-- projections are idempotent, while carrying an unscoped cursor forward could
-- silently skip changes after a metadata database rebuild.
UPDATE public.metadata_change_consumers
SET next_cursor = 0,
    updated_at = now();

-- +goose Down
ALTER TABLE public.metadata_change_consumers
    DROP COLUMN stream_id;
