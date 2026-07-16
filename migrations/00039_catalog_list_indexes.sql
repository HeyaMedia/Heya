-- +goose Up
-- Catalog-listing performance pass (2026-07-16 audit, measured on prod):
--
-- 1. Songs/Albums deep OFFSET pages. The music grids random-access the whole
--    catalog through the virtual scroller (dragging the scrollbar fires
--    offset≈224k page fetches), and the list sort spans three tables
--    (artist name → album year/title → disc/track), which no index could
--    serve: every deep page hash-joined all 280k tracks and quicksorted
--    ~86MB (464ms at offset 100k; page 1 was 3ms). Denormalizing the sort
--    keys onto tracks/albums makes the whole ORDER BY single-table and
--    index-backed, so a deep page becomes an index-only skip (measured 63ms
--    at offset 224k on a sim without a visibility map — real table with
--    vacuum lands lower). Triggers below keep the copies in sync; they are
--    write-cheap (one parent probe per track insert) next to what the music
--    matcher already does per row.
--
-- 2. Recently-added rails walked idx_library_files_created_at newest-first
--    filtering by the JOINED media item's type — mid music-import the TV
--    rail walked 434k entries to find 500 TV files (368ms per home load).
--    (library_id, created_at DESC) lets the rail queries take a per-library
--    top-N via LATERAL instead (measured 0.7ms).
--
-- 3. "More in <genre>" seq-scanned albums applying genre = ANY(genres)
--    (74ms per shelf rotation). GIN on genres + a containment predicate
--    makes it index-driven.

ALTER TABLE tracks ADD COLUMN sort_artist text NOT NULL DEFAULT '';
ALTER TABLE tracks ADD COLUMN sort_album_year text NOT NULL DEFAULT '';
ALTER TABLE tracks ADD COLUMN sort_album text NOT NULL DEFAULT '';
ALTER TABLE albums ADD COLUMN sort_artist text NOT NULL DEFAULT '';

UPDATE albums al
SET sort_artist = lower(a.name)
FROM artists a
WHERE a.id = al.artist_id;

UPDATE tracks t
SET sort_artist     = al.sort_artist,
    sort_album_year = al.year,
    sort_album      = lower(al.title)
FROM albums al
WHERE al.id = t.album_id;

CREATE INDEX idx_tracks_catalog_order
    ON tracks (sort_artist, sort_album_year, sort_album, disc_number, track_number, id);
CREATE INDEX idx_albums_catalog_order
    ON albums (sort_artist, year, lower(title), id);

CREATE INDEX idx_library_files_library_created
    ON library_files (library_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_albums_genres_gin ON albums USING gin (genres);

-- +goose StatementBegin
-- Albums copy the artist's lowercased name; set on insert and artist moves.
CREATE FUNCTION albums_fill_sort_artist() RETURNS trigger AS $$
BEGIN
    SELECT lower(a.name) INTO NEW.sort_artist FROM artists a WHERE a.id = NEW.artist_id;
    NEW.sort_artist := COALESCE(NEW.sort_artist, '');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
-- Tracks copy (sort_artist, year, lower(title)) from their album; set on
-- insert and album moves.
CREATE FUNCTION tracks_fill_sort_keys() RETURNS trigger AS $$
BEGIN
    SELECT al.sort_artist, al.year, lower(al.title)
      INTO NEW.sort_artist, NEW.sort_album_year, NEW.sort_album
      FROM albums al WHERE al.id = NEW.album_id;
    NEW.sort_artist     := COALESCE(NEW.sort_artist, '');
    NEW.sort_album_year := COALESCE(NEW.sort_album_year, '');
    NEW.sort_album      := COALESCE(NEW.sort_album, '');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
-- Album identity edits (title/year/artist reassignment) cascade to the
-- album's tracks. sort_artist is already refreshed on NEW by the BEFORE
-- trigger when artist_id changed.
CREATE FUNCTION albums_cascade_sort_keys() RETURNS trigger AS $$
BEGIN
    UPDATE tracks t
       SET sort_artist     = NEW.sort_artist,
           sort_album_year = NEW.year,
           sort_album      = lower(NEW.title)
     WHERE t.album_id = NEW.id;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
-- Artist renames cascade to albums AND tracks directly — the albums AFTER
-- trigger only watches title/year/artist_id, so writing sort_artist there
-- would not reach the tracks on its own.
CREATE FUNCTION artists_cascade_sort_keys() RETURNS trigger AS $$
BEGIN
    UPDATE albums al SET sort_artist = lower(NEW.name) WHERE al.artist_id = NEW.id;
    UPDATE tracks t
       SET sort_artist = lower(NEW.name)
      FROM albums al
     WHERE al.id = t.album_id AND al.artist_id = NEW.id;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_albums_fill_sort_artist
    BEFORE INSERT OR UPDATE OF artist_id ON albums
    FOR EACH ROW EXECUTE FUNCTION albums_fill_sort_artist();

CREATE TRIGGER trg_tracks_fill_sort_keys
    BEFORE INSERT OR UPDATE OF album_id ON tracks
    FOR EACH ROW EXECUTE FUNCTION tracks_fill_sort_keys();

CREATE TRIGGER trg_albums_cascade_sort_keys
    AFTER UPDATE OF title, year, artist_id ON albums
    FOR EACH ROW
    WHEN (OLD.title IS DISTINCT FROM NEW.title
       OR OLD.year IS DISTINCT FROM NEW.year
       OR OLD.artist_id IS DISTINCT FROM NEW.artist_id)
    EXECUTE FUNCTION albums_cascade_sort_keys();

CREATE TRIGGER trg_artists_cascade_sort_keys
    AFTER UPDATE OF name ON artists
    FOR EACH ROW
    WHEN (OLD.name IS DISTINCT FROM NEW.name)
    EXECUTE FUNCTION artists_cascade_sort_keys();

-- +goose Down
DROP TRIGGER IF EXISTS trg_artists_cascade_sort_keys ON artists;
DROP TRIGGER IF EXISTS trg_albums_cascade_sort_keys ON albums;
DROP TRIGGER IF EXISTS trg_tracks_fill_sort_keys ON tracks;
DROP TRIGGER IF EXISTS trg_albums_fill_sort_artist ON albums;
DROP FUNCTION IF EXISTS artists_cascade_sort_keys();
DROP FUNCTION IF EXISTS albums_cascade_sort_keys();
DROP FUNCTION IF EXISTS tracks_fill_sort_keys();
DROP FUNCTION IF EXISTS albums_fill_sort_artist();
DROP INDEX IF EXISTS idx_albums_genres_gin;
DROP INDEX IF EXISTS idx_library_files_library_created;
DROP INDEX IF EXISTS idx_albums_catalog_order;
DROP INDEX IF EXISTS idx_tracks_catalog_order;
ALTER TABLE albums DROP COLUMN IF EXISTS sort_artist;
ALTER TABLE tracks DROP COLUMN IF EXISTS sort_album;
ALTER TABLE tracks DROP COLUMN IF EXISTS sort_album_year;
ALTER TABLE tracks DROP COLUMN IF EXISTS sort_artist;
