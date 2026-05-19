-- +goose Up
CREATE TABLE match_candidates (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    library_file_id BIGINT NOT NULL REFERENCES library_files(id) ON DELETE CASCADE,
    provider_name   TEXT   NOT NULL,
    provider_id     TEXT   NOT NULL,
    title           TEXT   NOT NULL,
    year            TEXT   NOT NULL DEFAULT '',
    description     TEXT   NOT NULL DEFAULT '',
    poster_url      TEXT   NOT NULL DEFAULT '',
    confidence      NUMERIC(4,3) NOT NULL,
    raw_data        JSONB  NOT NULL DEFAULT '{}',
    chosen          BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (library_file_id, provider_id)
);

CREATE INDEX idx_match_candidates_file ON match_candidates (library_file_id);
CREATE INDEX idx_match_candidates_confidence ON match_candidates (confidence DESC);

-- +goose Down
DROP TABLE match_candidates;
