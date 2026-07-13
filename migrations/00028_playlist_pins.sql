-- +goose Up

-- Playlist pins: `pinned` floats a playlist to the top of the playlists
-- page; `sidebar_pinned` + `sidebar_position` drive the left sidebar's
-- separate pin set and manual drag order.
ALTER TABLE public.user_playlists
    ADD COLUMN IF NOT EXISTS pinned boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS sidebar_pinned boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS sidebar_position integer NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE public.user_playlists
    DROP COLUMN IF EXISTS sidebar_position,
    DROP COLUMN IF EXISTS sidebar_pinned,
    DROP COLUMN IF EXISTS pinned;
