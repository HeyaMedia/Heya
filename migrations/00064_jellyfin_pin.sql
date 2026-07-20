-- Jellyfin-compatible API (internal/jellyfin) per-user PIN.
--
-- jellyfin_credentials: an optional short numeric PIN accepted by the
-- Jellyfin login (/Users/AuthenticateByName) as an alternative to the real
-- account password — TV remotes and 10-foot on-screen keyboards make long
-- passwords miserable. Server-minted (never user-chosen, so it can't leak a
-- reused password), readable back in Settings for typing into a client,
-- rotatable/revocable without touching the account password, and valid ONLY
-- on the Jellyfin surface. No unique index on pin: login is username+PIN,
-- so PIN collisions across users are harmless and, at 6 digits, expected.

-- +goose Up
CREATE TABLE public.jellyfin_credentials (
    user_id bigint PRIMARY KEY REFERENCES public.users(id) ON DELETE CASCADE,
    pin text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    rotated_at timestamp with time zone DEFAULT now() NOT NULL,
    last_used_at timestamp with time zone
);

-- +goose Down
DROP TABLE IF EXISTS public.jellyfin_credentials;
