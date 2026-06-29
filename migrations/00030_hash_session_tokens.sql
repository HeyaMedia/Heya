-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;
ALTER TABLE sessions RENAME COLUMN token TO token_hash;
UPDATE sessions SET token_hash = encode(digest(token_hash, 'sha256'), 'hex');
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_token_key;
ALTER TABLE sessions ADD CONSTRAINT sessions_token_hash_key UNIQUE (token_hash);
DROP INDEX IF EXISTS idx_sessions_token;
CREATE INDEX idx_sessions_token_hash ON sessions (token_hash);

-- +goose Down
-- Raw tokens cannot be recovered from token_hash. Delete sessions so a
-- rollback restores the old column shape without leaving unusable hash values
-- that look like valid session tokens.
DELETE FROM sessions;
DROP INDEX IF EXISTS idx_sessions_token_hash;
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_token_hash_key;
ALTER TABLE sessions RENAME COLUMN token_hash TO token;
ALTER TABLE sessions ADD CONSTRAINT sessions_token_key UNIQUE (token);
CREATE INDEX idx_sessions_token ON sessions (token);
