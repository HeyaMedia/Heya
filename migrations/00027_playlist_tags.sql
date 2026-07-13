-- +goose Up

-- Free-form playlist tags — filter/organize axes for the playlists page.
ALTER TABLE public.user_playlists
    ADD COLUMN IF NOT EXISTS tags text[] NOT NULL DEFAULT '{}';

-- +goose Down

ALTER TABLE public.user_playlists DROP COLUMN IF EXISTS tags;
