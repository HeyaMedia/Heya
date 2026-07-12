-- +goose Up

-- Raw external listening history (ListenBrainz / Last.fm): EVERY imported
-- listen/love/hate is stored whether or not it matched a library track, so
-- matching becomes a view over the data instead of a filter at ingest.
-- Unmatched rows retro-match later as the library grows (the daily
-- sync_music_services task sweeps them), and the unmatched set doubles as
-- "most-listened music you don't own" — the acquisition signal.
CREATE TABLE IF NOT EXISTS public.external_listens (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    user_id bigint NOT NULL,
    service text NOT NULL,
    kind text NOT NULL DEFAULT 'listen',
    artist_name text NOT NULL DEFAULT '',
    track_name text NOT NULL DEFAULT '',
    release_name text NOT NULL DEFAULT '',
    recording_mbid text NOT NULL DEFAULT '',
    listened_at timestamp with time zone NOT NULL,
    duration_seconds integer NOT NULL DEFAULT 0,
    matched_track_id bigint,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT external_listens_pkey PRIMARY KEY (id),
    CONSTRAINT external_listens_kind_check CHECK (kind IN ('listen', 'love', 'hate')),
    CONSTRAINT external_listens_user_id_fkey FOREIGN KEY (user_id)
        REFERENCES public.users(id) ON DELETE CASCADE,
    CONSTRAINT external_listens_matched_track_id_fkey FOREIGN KEY (matched_track_id)
        REFERENCES public.tracks(id) ON DELETE SET NULL,
    -- Ingest idempotency: one row per (user, service, instant, track text).
    CONSTRAINT external_listens_dedupe_key UNIQUE (user_id, service, kind, listened_at, track_name)
);

CREATE INDEX IF NOT EXISTS external_listens_unmatched_idx
    ON public.external_listens (user_id, service) WHERE matched_track_id IS NULL;
CREATE INDEX IF NOT EXISTS external_listens_user_time_idx
    ON public.external_listens (user_id, service, listened_at DESC);

-- Normalized matching tier: strip parenthetical/bracket suffixes from track
-- titles ("Song (Remastered 2011)" → "song") so listens with version-suffixed
-- titles still resolve. Expression indexes keep the lookups cheap.
CREATE INDEX IF NOT EXISTS tracks_title_norm_idx
    ON public.tracks (lower(regexp_replace(title, '\s*[\(\[].*$', '')));
CREATE INDEX IF NOT EXISTS artists_name_lower_idx
    ON public.artists (lower(name));

-- Daily incremental sync: pulls the last day of listens for every linked
-- service and retro-matches stored unmatched listens against the library.
INSERT INTO public.scheduled_tasks (id, display_name, description, category, enabled, interval_hours, daily_start_time, daily_end_time, max_runtime_minutes) VALUES
('sync_music_services', 'Sync Music Services', 'Pull the last day of ListenBrainz / Last.fm listens for every linked account and retro-match stored unmatched listens against the library. No-op for users without linked services.', 'library', true, 24, '00:00', '23:59', 60)
ON CONFLICT (id) DO NOTHING;

-- +goose Down

DELETE FROM public.scheduled_tasks WHERE id = 'sync_music_services';
DROP INDEX IF EXISTS public.artists_name_lower_idx;
DROP INDEX IF EXISTS public.tracks_title_norm_idx;
DROP INDEX IF EXISTS public.external_listens_user_time_idx;
DROP INDEX IF EXISTS public.external_listens_unmatched_idx;
DROP TABLE IF EXISTS public.external_listens;
