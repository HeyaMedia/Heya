-- +goose Up
-- The scoped Jellyfin login (CreateJellyfinSession) mints sessions with
-- kind='jellyfin_session'; the baseline CHECK only allowed
-- {session, api_token}, so every successful Jellyfin login 500'd on the
-- constraint while wrong passwords still 401'd — a silent auth outage.
ALTER TABLE sessions DROP CONSTRAINT sessions_kind_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_kind_check
    CHECK (kind = ANY (ARRAY['session'::text, 'api_token'::text, 'jellyfin_session'::text]));

-- +goose Down
DELETE FROM sessions WHERE kind = 'jellyfin_session';
ALTER TABLE sessions DROP CONSTRAINT sessions_kind_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_kind_check
    CHECK (kind = ANY (ARRAY['session'::text, 'api_token'::text]));
