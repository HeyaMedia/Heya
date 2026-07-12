-- +goose Up

-- Generated/recommendation playlists are owned by the provider and can only
-- flow down into Heya. Existing links remain normal two-way links.
ALTER TABLE public.user_playlist_syncs
    ADD COLUMN sync_mode text NOT NULL DEFAULT 'two_way';
ALTER TABLE public.user_playlist_syncs
    ADD CONSTRAINT user_playlist_syncs_mode_check
    CHECK (sync_mode IN ('two_way', 'pull_only'));

-- Opt-in discovery policies, e.g. automatically link every playlist which
-- ListenBrainz creates for the user now or in the future.
CREATE TABLE public.user_playlist_sync_policies (
    user_id bigint NOT NULL,
    service text NOT NULL,
    collection text NOT NULL,
    enabled boolean NOT NULL DEFAULT false,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT user_playlist_sync_policies_pkey PRIMARY KEY (user_id, service, collection),
    CONSTRAINT user_playlist_sync_policies_user_id_fkey FOREIGN KEY (user_id)
        REFERENCES public.users(id) ON DELETE CASCADE
);

-- +goose Down

DROP TABLE IF EXISTS public.user_playlist_sync_policies;
ALTER TABLE public.user_playlist_syncs DROP CONSTRAINT IF EXISTS user_playlist_syncs_mode_check;
ALTER TABLE public.user_playlist_syncs DROP COLUMN IF EXISTS sync_mode;
