-- Subsonic-compatible API (internal/subsonic) storage.
--
-- subsonic_credentials: one app-password per user. Subsonic's token auth is
-- t = md5(password + salt), which requires the server to KNOW the shared
-- secret — Heya's bcrypt login hashes can't answer it. So each user gets a
-- server-generated random secret (never user-chosen, so no password reuse),
-- retrievable for the md5 check and rotatable/revocable at will. The same
-- secret doubles as the OpenSubsonic apiKey, hence the UNIQUE index for
-- reverse lookup.
--
-- subsonic_play_queues: getPlayQueue/savePlayQueue state — one queue per
-- user (spec semantics: last writer wins across devices).

-- +goose Up
CREATE TABLE public.subsonic_credentials (
    user_id bigint PRIMARY KEY REFERENCES public.users(id) ON DELETE CASCADE,
    secret text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    rotated_at timestamp with time zone DEFAULT now() NOT NULL,
    last_used_at timestamp with time zone
);

CREATE UNIQUE INDEX idx_subsonic_credentials_secret
  ON public.subsonic_credentials USING btree (secret);

CREATE TABLE public.subsonic_play_queues (
    user_id bigint PRIMARY KEY REFERENCES public.users(id) ON DELETE CASCADE,
    track_ids bigint[] DEFAULT '{}'::bigint[] NOT NULL,
    current_track_id bigint DEFAULT 0 NOT NULL,
    position_ms bigint DEFAULT 0 NOT NULL,
    changed_at timestamp with time zone DEFAULT now() NOT NULL,
    changed_by text DEFAULT ''::text NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS public.subsonic_play_queues;
DROP INDEX IF EXISTS public.idx_subsonic_credentials_secret;
DROP TABLE IF EXISTS public.subsonic_credentials;
