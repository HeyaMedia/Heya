-- +goose Up
-- Dedup local-first materialization by NATURAL identity across every media
-- item, replacing the local-only `local_identity_key` anchor.
--
-- The old column was written only for locally-materialized stubs and read only
-- by their re-scan dedup lookup (which filtered `local_identity_key <> ''`), so
-- a remotely-matched / enriched series was invisible to it. Consequence: a
-- transient heya.media miss on a NEW file of an existing show (e.g. a just-aired
-- episode) fell through to materializeLocal, found no keyed row, and forked the
-- show into a second, "local" series — orphaning the episode. Seen in prod with
-- House of the Dragon S03E03.
--
-- Identity now comes straight from the item's own columns
-- (lower(btrim(title)) | year | media_type), so the dedup lookup
-- (FindMediaItemByIdentity) covers enriched and local rows alike. The functional
-- index makes it an indexed equality seek; the query normalizes the probe with
-- the same lower(btrim(...)) so stored and probed values always agree.
DROP INDEX IF EXISTS idx_media_items_local_identity_key;
ALTER TABLE media_items DROP COLUMN IF EXISTS local_identity_key;

CREATE INDEX IF NOT EXISTS idx_media_items_identity
    ON media_items (library_id, media_type, year, lower(btrim(title)));

-- +goose Down
DROP INDEX IF EXISTS idx_media_items_identity;
ALTER TABLE media_items
    ADD COLUMN local_identity_key text NOT NULL DEFAULT '';
CREATE INDEX idx_media_items_local_identity_key
    ON media_items (library_id, local_identity_key)
    WHERE local_identity_key <> '';
