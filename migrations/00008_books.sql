-- +goose Up
CREATE TABLE authors (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name            TEXT        NOT NULL,
    openlibrary_id  TEXT        NOT NULL DEFAULT '',
    biography       TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX idx_authors_openlibrary_id ON authors (openlibrary_id) WHERE openlibrary_id != '';

CREATE TABLE books (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id   BIGINT  NOT NULL UNIQUE REFERENCES media_items(id) ON DELETE CASCADE,
    author_id       BIGINT      REFERENCES authors(id) ON DELETE SET NULL,
    isbn            TEXT        NOT NULL DEFAULT '',
    openlibrary_id  TEXT        NOT NULL DEFAULT '',
    page_count      INTEGER     NOT NULL DEFAULT 0,
    publisher       TEXT        NOT NULL DEFAULT '',
    publish_date    DATE,
    file_path       TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX idx_books_author_id ON books (author_id);
CREATE INDEX idx_books_isbn ON books (isbn) WHERE isbn != '';

-- +goose Down
DROP TABLE books;
DROP TABLE authors;
