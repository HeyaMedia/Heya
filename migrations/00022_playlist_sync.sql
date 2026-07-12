-- +goose Up

-- Provider-neutral playlist links. A row exists only while synchronization is
-- enabled, so playlist sync is opt-in by construction. snapshot_track_ids is
-- the last common provider-ID sequence and acts as the merge base for the next
-- two-way pass (recording MBIDs for ListenBrainz, service track IDs later).
CREATE TABLE public.user_playlist_syncs (
    user_id bigint NOT NULL,
    playlist_id bigint NOT NULL,
    service text NOT NULL,
    external_id text NOT NULL,
    snapshot_track_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
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

CREATE INDEX user_playlist_syncs_due_idx
    ON public.user_playlist_syncs (last_synced_at NULLS FIRST);

-- +goose Down

DROP TABLE IF EXISTS public.user_playlist_syncs;
