-- +goose Up

-- Per-user external music service links (ListenBrainz / Last.fm): credentials,
-- outbound scrobbling toggle, and listen-history import state. Import turns
-- external scrobble history into play_events rows (source = the service name),
-- which is the taste-model signal the mixes engine feeds on.
CREATE TABLE IF NOT EXISTS public.user_music_services (
    user_id bigint NOT NULL,
    service text NOT NULL,
    username text NOT NULL DEFAULT '',
    -- ListenBrainz: the user token. Last.fm: the session key from the auth
    -- handshake. Secret-at-rest like HEYA AI keys; never echoed by the API.
    token text NOT NULL DEFAULT '',
    scrobble_enabled boolean NOT NULL DEFAULT false,
    import_state jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT user_music_services_pkey PRIMARY KEY (user_id, service),
    CONSTRAINT user_music_services_service_check CHECK (service IN ('listenbrainz', 'lastfm')),
    CONSTRAINT user_music_services_user_id_fkey FOREIGN KEY (user_id)
        REFERENCES public.users(id) ON DELETE CASCADE
);

-- Import dedupe lookup: "does this exact listen already exist?"
CREATE INDEX IF NOT EXISTS play_events_user_track_played_idx
    ON public.play_events (user_id, track_id, played_at);

-- +goose Down

DROP INDEX IF EXISTS public.play_events_user_track_played_idx;
DROP TABLE IF EXISTS public.user_music_services;
