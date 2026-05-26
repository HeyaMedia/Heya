-- +goose Up
-- +goose StatementBegin

-- Cached per-(library, path) disk usage. Storage page reads from here so the
-- page paint is cheap; population happens in a River job (scan_library_disk
-- worker) because a `du`-equivalent walk on a multi-TB library can take
-- minutes. PRIMARY KEY (library_id, path) means a re-scan upserts in place
-- rather than appending — the page always sees the latest reading per row.
-- ON DELETE CASCADE so removing a library drops its readings automatically.
CREATE TABLE library_disk_usage (
    library_id  BIGINT      NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    path        TEXT        NOT NULL,
    bytes       BIGINT      NOT NULL,
    file_count  BIGINT      NOT NULL,
    scanned_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (library_id, path)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE library_disk_usage;

-- +goose StatementEnd
