-- +goose Up

-- Migration version 22 existed in development before playlist sync was
-- assigned that number. Databases which applied the earlier migration retain
-- version 22 in goose_db_version, so playlist sync owns a fresh version. The
-- IF NOT EXISTS form also repairs databases where an early development build
-- happened to create the table already.
CREATE TABLE IF NOT EXISTS public.user_playlist_syncs (
    user_id bigint NOT NULL,
    playlist_id bigint NOT NULL,
    service text NOT NULL,
    external_id text NOT NULL,
    snapshot_track_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    unmatched_track_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    last_synced_at timestamp with time zone,
    last_error text NOT NULL DEFAULT '',
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT user_playlist_syncs_pkey PRIMARY KEY (playlist_id, service),
    CONSTRAINT user_playlist_syncs_external_key UNIQUE (user_id, service, external_id),
    CONSTRAINT user_playlist_syncs_user_id_fkey FOREIGN KEY (user_id)
        REFERENCES public.users(id) ON DELETE CASCADE,
    CONSTRAINT user_playlist_syncs_playlist_id_fkey FOREIGN KEY (playlist_id)
        REFERENCES public.user_playlists(id) ON DELETE CASCADE
);

-- Also upgrades databases where the table was created by the first form of
-- migration 22, before unresolved provider IDs received their own state.
ALTER TABLE public.user_playlist_syncs
    ADD COLUMN IF NOT EXISTS unmatched_track_ids jsonb NOT NULL DEFAULT '[]'::jsonb;

CREATE INDEX IF NOT EXISTS user_playlist_syncs_due_idx
    ON public.user_playlist_syncs (last_synced_at NULLS FIRST);

-- +goose Down

DROP TABLE IF EXISTS public.user_playlist_syncs;
