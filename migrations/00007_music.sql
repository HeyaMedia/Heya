-- +goose Up
CREATE TABLE artists (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id   BIGINT  NOT NULL UNIQUE REFERENCES media_items(id) ON DELETE CASCADE,
    musicbrainz_id  TEXT        NOT NULL DEFAULT '',
    sort_name       TEXT        NOT NULL DEFAULT '',
    biography       TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX idx_artists_musicbrainz_id ON artists (musicbrainz_id) WHERE musicbrainz_id != '';

CREATE TABLE albums (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    artist_id       BIGINT      NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    title           TEXT        NOT NULL,
    year            TEXT        NOT NULL DEFAULT '',
    musicbrainz_id  TEXT        NOT NULL DEFAULT '',
    album_type      TEXT        NOT NULL DEFAULT 'album',
    genres          TEXT[]      NOT NULL DEFAULT '{}',
    cover_path      TEXT        NOT NULL DEFAULT '',
    release_date    DATE
);

CREATE INDEX idx_albums_artist_id ON albums (artist_id);
CREATE INDEX idx_albums_musicbrainz_id ON albums (musicbrainz_id) WHERE musicbrainz_id != '';

CREATE TABLE tracks (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    album_id        BIGINT      NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    disc_number     INTEGER     NOT NULL DEFAULT 1,
    track_number    INTEGER     NOT NULL,
    title           TEXT        NOT NULL,
    duration_ms     INTEGER     NOT NULL DEFAULT 0,
    file_path       TEXT        NOT NULL DEFAULT '',
    UNIQUE (album_id, disc_number, track_number)
);

-- +goose Down
DROP TABLE tracks;
DROP TABLE albums;
DROP TABLE artists;
