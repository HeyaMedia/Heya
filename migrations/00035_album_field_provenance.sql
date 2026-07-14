-- +goose Up

-- Album fields are editable in the metadata panel. Track those manual choices
-- so a later artist refresh fills around them instead of immediately undoing
-- the user's save.
ALTER TABLE public.albums
    ADD COLUMN field_provenance jsonb DEFAULT '{}'::jsonb NOT NULL;

-- +goose Down

ALTER TABLE public.albums
    DROP COLUMN IF EXISTS field_provenance;
