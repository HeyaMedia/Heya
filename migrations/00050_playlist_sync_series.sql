-- +goose Up

-- Provider-generated recurring playlists (ListenBrainz Weekly Jams, Weekly
-- Exploration, Daily Jams) publish every edition as a brand-new remote
-- playlist. The series key is the stable identity across editions so Heya can
-- keep a single local playlist and re-point it at the newest edition, instead
-- of importing a fresh copy every week.
ALTER TABLE public.user_playlist_syncs
    ADD COLUMN series text NOT NULL DEFAULT '';

CREATE UNIQUE INDEX user_playlist_syncs_series_key
    ON public.user_playlist_syncs (user_id, service, series)
    WHERE series <> '';

-- +goose Down

DROP INDEX IF EXISTS public.user_playlist_syncs_series_key;
ALTER TABLE public.user_playlist_syncs DROP COLUMN IF EXISTS series;
