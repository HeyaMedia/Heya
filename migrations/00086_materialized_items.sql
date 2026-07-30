-- +goose Up
-- Fileless (manager-added) items. materialized_at is a one-way fact: set
-- when the first real file links to the item, never cleared when files later
-- vanish (that's the existing Available=false state, a different lifecycle).
-- DEFAULT now() deliberately covers every existing insert path — including
-- the old prod worker binary that doesn't know this column — so ONLY the
-- manager add path (which inserts NULL explicitly) creates hidden items.
ALTER TABLE media_items ADD COLUMN materialized_at timestamptz DEFAULT now();
ALTER TABLE media_items ADD COLUMN added_source text NOT NULL DEFAULT 'scanner'
    CHECK (added_source IN ('scanner','matcher','manager','migration'));

UPDATE media_items SET materialized_at = created_at;

-- Materialization stamps ride triggers so every binary version and every
-- link/reparent path is covered atomically (deliberate first-use of
-- triggers; scope reviewed): direct file links, file relinks, and the music
-- reparent paths (a manager-created artist gains files via track_files
-- inserts, track moves, and album moves — none of which touch media_items).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION heya_mark_materialized() RETURNS trigger AS $$
BEGIN
    IF NEW.media_item_id IS NOT NULL THEN
        UPDATE media_items SET materialized_at = now()
        WHERE id = NEW.media_item_id AND materialized_at IS NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION heya_mark_materialized_track() RETURNS trigger AS $$
BEGIN
    UPDATE media_items SET materialized_at = now()
    WHERE materialized_at IS NULL AND id = (
        SELECT ar.media_item_id FROM tracks t
        JOIN albums al ON al.id = t.album_id
        JOIN artists ar ON ar.id = al.artist_id
        WHERE t.id = NEW.track_id)
    ;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION heya_mark_materialized_album() RETURNS trigger AS $$
BEGIN
    UPDATE media_items SET materialized_at = now()
    WHERE materialized_at IS NULL AND id = (
        SELECT ar.media_item_id FROM artists ar WHERE ar.id = NEW.artist_id)
      AND EXISTS (
        SELECT 1 FROM tracks t JOIN track_files tf ON tf.track_id = t.id
        WHERE t.album_id = NEW.id)
    ;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_materialize_file_link
    AFTER INSERT OR UPDATE OF media_item_id ON library_file_links
    FOR EACH ROW EXECUTE FUNCTION heya_mark_materialized();

CREATE TRIGGER trg_materialize_track_file
    AFTER INSERT OR UPDATE OF track_id ON track_files
    FOR EACH ROW EXECUTE FUNCTION heya_mark_materialized_track();

CREATE TRIGGER trg_materialize_album_move
    AFTER UPDATE OF artist_id ON albums
    FOR EACH ROW EXECUTE FUNCTION heya_mark_materialized_album();

-- Track→album moves resolve through the same track-file ownership walk.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION heya_mark_materialized_track_move() RETURNS trigger AS $$
BEGIN
    UPDATE media_items SET materialized_at = now()
    WHERE materialized_at IS NULL AND id = (
        SELECT ar.media_item_id FROM albums al
        JOIN artists ar ON ar.id = al.artist_id
        WHERE al.id = NEW.album_id)
      AND EXISTS (SELECT 1 FROM track_files tf WHERE tf.track_id = NEW.id)
    ;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_materialize_track_reparent
    AFTER UPDATE OF album_id ON tracks
    FOR EACH ROW EXECUTE FUNCTION heya_mark_materialized_track_move();

-- The public read model: same shape as media_item_cards, materialized rows
-- only. Public browse/search/rails swap FROM to this view; internal paths
-- (scanner adoption, matcher, enrichment, manager) stay on the full view.
CREATE VIEW materialized_media_item_cards AS
SELECT c.* FROM media_item_cards c
JOIN media_items mi ON mi.id = c.id
WHERE mi.materialized_at IS NOT NULL;

-- +goose Down
DROP VIEW materialized_media_item_cards;
DROP TRIGGER trg_materialize_track_reparent ON tracks;
DROP TRIGGER trg_materialize_album_move ON albums;
DROP TRIGGER trg_materialize_track_file ON track_files;
DROP TRIGGER trg_materialize_file_link ON library_file_links;
DROP FUNCTION heya_mark_materialized_track_move();
DROP FUNCTION heya_mark_materialized_album();
DROP FUNCTION heya_mark_materialized_track();
DROP FUNCTION heya_mark_materialized();
ALTER TABLE media_items DROP COLUMN added_source;
ALTER TABLE media_items DROP COLUMN materialized_at;
