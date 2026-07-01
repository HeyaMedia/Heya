-- +goose Up

-- Per-directory NFO state, keyed by the directory's full path (same path
-- convention as library_files.path). The scanner records the canonical NFO
-- (tvshow/movie/artist/album.nfo) it saw in each directory along with the
-- file's mtime — dir listings already carry mtimes, so comparing against this
-- row detects an edited NFO without opening it. A changed/added/removed row
-- re-applies local metadata to just the files beneath that directory.
CREATE TABLE library_nfo_dirs (
    library_id BIGINT      NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    dir_path   TEXT        NOT NULL,
    nfo_name   TEXT        NOT NULL,
    mtime      TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (library_id, dir_path)
);

-- +goose Down

DROP TABLE library_nfo_dirs;
