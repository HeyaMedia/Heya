-- +goose Up

CREATE TABLE artists (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id  BIGINT NOT NULL UNIQUE REFERENCES media_items(id) ON DELETE CASCADE,
    musicbrainz_id TEXT   NOT NULL DEFAULT '',
    name           TEXT   NOT NULL DEFAULT '',
    sort_name      TEXT   NOT NULL DEFAULT '',
    disambiguation TEXT   NOT NULL DEFAULT '',
    biography      TEXT   NOT NULL DEFAULT '',
    search_vector  tsvector GENERATED ALWAYS AS (
                       setweight(to_tsvector('simple', coalesce(name, '')), 'A') ||
                       setweight(to_tsvector('simple', coalesce(sort_name, '')), 'A') ||
                       setweight(to_tsvector('english', coalesce(biography, '')), 'D')
                   ) STORED
);

CREATE INDEX idx_artists_musicbrainz_id ON artists (musicbrainz_id) WHERE musicbrainz_id != '';
CREATE INDEX idx_artists_search ON artists USING GIN (search_vector);
CREATE INDEX idx_artists_name_trgm ON artists USING GIN (lower(name) gin_trgm_ops);
CREATE INDEX idx_artists_sort_name_trgm ON artists USING GIN (lower(sort_name) gin_trgm_ops);
CREATE UNIQUE INDEX uq_artists_name_disambig ON artists (lower(name), lower(disambiguation)) WHERE name != '';

CREATE TABLE albums (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    artist_id      BIGINT  NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    title          TEXT    NOT NULL,
    year           TEXT    NOT NULL DEFAULT '',
    musicbrainz_id TEXT    NOT NULL DEFAULT '',
    album_type     TEXT    NOT NULL DEFAULT 'album',
    genres         TEXT[]  NOT NULL DEFAULT '{}',
    cover_path     TEXT    NOT NULL DEFAULT '',
    release_date   DATE,
    label          TEXT    NOT NULL DEFAULT '',
    country        TEXT    NOT NULL DEFAULT '',
    barcode        TEXT    NOT NULL DEFAULT '',
    total_tracks   INTEGER NOT NULL DEFAULT 0,
    total_discs    INTEGER NOT NULL DEFAULT 0,
    tags           TEXT[]  NOT NULL DEFAULT '{}',
    search_vector  tsvector GENERATED ALWAYS AS (
                       setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
                       setweight(to_tsvector('simple', immutable_array_to_string(coalesce(tags, '{}'::text[]), ' ')), 'C')
                   ) STORED
);

CREATE INDEX idx_albums_artist_id ON albums (artist_id);
CREATE INDEX idx_albums_musicbrainz_id ON albums (musicbrainz_id) WHERE musicbrainz_id != '';
CREATE INDEX idx_albums_search ON albums USING GIN (search_vector);
CREATE INDEX idx_albums_title_trgm ON albums USING GIN (lower(title) gin_trgm_ops);
CREATE UNIQUE INDEX uq_albums_artist_title_year ON albums (artist_id, lower(title), year);

CREATE TABLE tracks (
    id            BIGINT  GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    album_id      BIGINT  NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    disc_number   INTEGER NOT NULL DEFAULT 1,
    track_number  INTEGER NOT NULL,
    title         TEXT    NOT NULL,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    file_path     TEXT    NOT NULL DEFAULT '',
    lyrics_path   TEXT    NOT NULL DEFAULT '',
    search_vector tsvector GENERATED ALWAYS AS (
                      to_tsvector('simple', coalesce(title, ''))
                  ) STORED,
    UNIQUE (album_id, disc_number, track_number)
);

CREATE INDEX idx_tracks_search ON tracks USING GIN (search_vector);
CREATE INDEX idx_tracks_title_trgm ON tracks USING GIN (lower(title) gin_trgm_ops);

CREATE TABLE authors (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name           TEXT   NOT NULL,
    openlibrary_id TEXT   NOT NULL DEFAULT '',
    biography      TEXT   NOT NULL DEFAULT '',
    birth_date     TEXT   NOT NULL DEFAULT '',
    death_date     TEXT   NOT NULL DEFAULT '',
    search_vector  tsvector GENERATED ALWAYS AS (
                       setweight(to_tsvector('simple', coalesce(name, '')), 'A') ||
                       setweight(to_tsvector('english', coalesce(biography, '')), 'D')
                   ) STORED
);

CREATE INDEX idx_authors_openlibrary_id ON authors (openlibrary_id) WHERE openlibrary_id != '';
CREATE INDEX idx_authors_search ON authors USING GIN (search_vector);
CREATE INDEX idx_authors_name_trgm ON authors USING GIN (lower(name) gin_trgm_ops);

CREATE TABLE books (
    id             BIGINT  GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id  BIGINT  NOT NULL UNIQUE REFERENCES media_items(id) ON DELETE CASCADE,
    author_id      BIGINT  REFERENCES authors(id) ON DELETE SET NULL,
    isbn           TEXT    NOT NULL DEFAULT '',
    openlibrary_id TEXT    NOT NULL DEFAULT '',
    page_count     INTEGER NOT NULL DEFAULT 0,
    publisher      TEXT    NOT NULL DEFAULT '',
    publish_date   DATE,
    file_path      TEXT    NOT NULL DEFAULT '',
    subjects       TEXT[]  NOT NULL DEFAULT '{}',
    language       TEXT    NOT NULL DEFAULT '',
    series_name    TEXT    NOT NULL DEFAULT '',
    series_number  INTEGER NOT NULL DEFAULT 0,
    format         TEXT    NOT NULL DEFAULT '',
    description    TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX idx_books_author_id ON books (author_id);
CREATE INDEX idx_books_isbn ON books (isbn) WHERE isbn != '';

-- +goose Down
DROP TABLE books;
DROP TABLE authors;
DROP TABLE tracks;
DROP TABLE albums;
DROP TABLE artists;
