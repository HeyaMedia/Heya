-- +goose Up
CREATE TABLE media_items (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    library_id      BIGINT      NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    media_type      media_type  NOT NULL,
    title           TEXT        NOT NULL,
    sort_title      TEXT        NOT NULL DEFAULT '',
    year            TEXT        NOT NULL DEFAULT '',
    description     TEXT        NOT NULL DEFAULT '',
    poster_path     TEXT        NOT NULL DEFAULT '',
    backdrop_path   TEXT        NOT NULL DEFAULT '',
    external_ids    JSONB       NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_media_items_library_id ON media_items (library_id);
CREATE INDEX idx_media_items_media_type ON media_items (media_type);
CREATE INDEX idx_media_items_title ON media_items (title);

ALTER TABLE media_items ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (to_tsvector('english', title || ' ' || coalesce(description, ''))) STORED;

CREATE INDEX idx_media_items_search ON media_items USING GIN (search_vector);

-- +goose Down
DROP TABLE media_items;
