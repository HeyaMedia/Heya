-- +goose Up
-- Post-deploy follow-up to 00039: the lower(title) EXPRESSION column in
-- idx_albums_catalog_order blocks index-only scans and skews the cost model,
-- so the planner kept choosing seq+sort for deep album pages (138ms measured
-- vs 44ms when the index path is forced — and a real column makes it
-- index-only like the tracks index, ~5-10ms). Mirror the tracks approach:
-- denormalize the sort title as a real column.

ALTER TABLE albums ADD COLUMN sort_title text NOT NULL DEFAULT '';
UPDATE albums SET sort_title = lower(title);

DROP INDEX idx_albums_catalog_order;
CREATE INDEX idx_albums_catalog_order
    ON albums (sort_artist, year, sort_title, id);

-- Widen the fill trigger to also maintain sort_title on title changes.
DROP TRIGGER trg_albums_fill_sort_artist ON albums;
DROP FUNCTION albums_fill_sort_artist();

-- +goose StatementBegin
CREATE FUNCTION albums_fill_sort_keys() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'INSERT' OR NEW.artist_id IS DISTINCT FROM OLD.artist_id THEN
        SELECT lower(a.name) INTO NEW.sort_artist FROM artists a WHERE a.id = NEW.artist_id;
        NEW.sort_artist := COALESCE(NEW.sort_artist, '');
    END IF;
    NEW.sort_title := lower(NEW.title);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_albums_fill_sort_keys
    BEFORE INSERT OR UPDATE OF artist_id, title ON albums
    FOR EACH ROW EXECUTE FUNCTION albums_fill_sort_keys();

-- +goose Down
DROP TRIGGER IF EXISTS trg_albums_fill_sort_keys ON albums;
DROP FUNCTION IF EXISTS albums_fill_sort_keys();
-- +goose StatementBegin
CREATE FUNCTION albums_fill_sort_artist() RETURNS trigger AS $$
BEGIN
    SELECT lower(a.name) INTO NEW.sort_artist FROM artists a WHERE a.id = NEW.artist_id;
    NEW.sort_artist := COALESCE(NEW.sort_artist, '');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER trg_albums_fill_sort_artist
    BEFORE INSERT OR UPDATE OF artist_id ON albums
    FOR EACH ROW EXECUTE FUNCTION albums_fill_sort_artist();
DROP INDEX IF EXISTS idx_albums_catalog_order;
CREATE INDEX idx_albums_catalog_order
    ON albums (sort_artist, year, lower(title), id);
ALTER TABLE albums DROP COLUMN IF EXISTS sort_title;
