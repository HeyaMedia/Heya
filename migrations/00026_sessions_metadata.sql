-- +goose Up
-- +goose StatementBegin
ALTER TABLE sessions
    ADD COLUMN kind         TEXT        NOT NULL DEFAULT 'session',
    ADD COLUMN name         TEXT,
    ADD COLUMN last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN user_agent   TEXT,
    ADD COLUMN ip           TEXT,
    ALTER COLUMN expires_at DROP NOT NULL;

ALTER TABLE sessions
    ADD CONSTRAINT sessions_kind_check
    CHECK (kind IN ('session', 'api_token'));

CREATE INDEX idx_sessions_user_kind ON sessions (user_id, kind);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_sessions_user_kind;

ALTER TABLE sessions
    DROP CONSTRAINT IF EXISTS sessions_kind_check;

ALTER TABLE sessions
    DROP COLUMN IF EXISTS ip,
    DROP COLUMN IF EXISTS user_agent,
    DROP COLUMN IF EXISTS last_seen_at,
    DROP COLUMN IF EXISTS name,
    DROP COLUMN IF EXISTS kind;

ALTER TABLE sessions
    ALTER COLUMN expires_at SET NOT NULL;
-- +goose StatementEnd
