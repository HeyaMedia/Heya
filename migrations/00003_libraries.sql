-- +goose Up
CREATE TYPE media_type AS ENUM (
    'movie',
    'tv',
    'music',
    'book',
    'comic',
    'podcast',
    'radio'
);

CREATE TABLE libraries (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name            TEXT        NOT NULL,
    media_type      media_type  NOT NULL,
    paths           TEXT[]      NOT NULL DEFAULT '{}',
    scan_interval   INTERVAL    NOT NULL DEFAULT '1 hour',
    created_by      BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE libraries;
DROP TYPE media_type;
