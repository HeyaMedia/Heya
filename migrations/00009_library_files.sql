-- +goose Up
CREATE TYPE file_status AS ENUM (
    'pending',
    'matched',
    'unmatched',
    'ignored',
    'error'
);

CREATE TABLE library_files (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    library_id      BIGINT      NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    path            TEXT        NOT NULL,
    size            BIGINT      NOT NULL DEFAULT 0,
    mtime           TIMESTAMPTZ,
    media_item_id   BIGINT      REFERENCES media_items(id) ON DELETE SET NULL,
    parse_result    JSONB       NOT NULL DEFAULT '{}',
    status          file_status NOT NULL DEFAULT 'pending',
    error_message   TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (library_id, path)
);

CREATE INDEX idx_library_files_library_id ON library_files (library_id);
CREATE INDEX idx_library_files_status ON library_files (status);
CREATE INDEX idx_library_files_media_item_id ON library_files (media_item_id);

-- +goose Down
DROP TABLE library_files;
DROP TYPE file_status;
