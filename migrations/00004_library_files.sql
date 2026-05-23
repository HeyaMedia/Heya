-- +goose Up

CREATE TYPE file_status AS ENUM (
    'pending',
    'matched',
    'unmatched',
    'ignored',
    'error'
);

CREATE TABLE library_files (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    library_id    BIGINT      NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    path          TEXT        NOT NULL,
    size          BIGINT      NOT NULL DEFAULT 0,
    mtime         TIMESTAMPTZ,
    media_item_id BIGINT      REFERENCES media_items(id) ON DELETE SET NULL,
    parse_result  JSONB       NOT NULL DEFAULT '{}',
    status        file_status NOT NULL DEFAULT 'pending',
    error_message TEXT        NOT NULL DEFAULT '',
    deleted_at    TIMESTAMPTZ,
    media_info    JSONB       NOT NULL DEFAULT '{}',
    keyframes     JSONB,
    has_trickplay BOOLEAN     NOT NULL DEFAULT false,
    content_hash  TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (library_id, path)
);

CREATE INDEX idx_library_files_library_id ON library_files (library_id);
CREATE INDEX idx_library_files_status ON library_files (status);
CREATE INDEX idx_library_files_media_item_id ON library_files (media_item_id);
CREATE INDEX idx_library_files_deleted ON library_files (deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX idx_library_files_content_hash ON library_files (library_id, content_hash) WHERE content_hash != '';

CREATE TABLE match_candidates (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    library_file_id BIGINT       NOT NULL REFERENCES library_files(id) ON DELETE CASCADE,
    provider_name   TEXT         NOT NULL,
    provider_id     TEXT         NOT NULL,
    title           TEXT         NOT NULL,
    year            TEXT         NOT NULL DEFAULT '',
    description     TEXT         NOT NULL DEFAULT '',
    poster_url      TEXT         NOT NULL DEFAULT '',
    confidence      NUMERIC(4,3) NOT NULL,
    raw_data        JSONB        NOT NULL DEFAULT '{}',
    chosen          BOOLEAN      NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (library_file_id, provider_id)
);

CREATE INDEX idx_match_candidates_file ON match_candidates (library_file_id);
CREATE INDEX idx_match_candidates_confidence ON match_candidates (confidence DESC);

-- +goose Down
DROP TABLE match_candidates;
DROP TABLE library_files;
DROP TYPE file_status;
