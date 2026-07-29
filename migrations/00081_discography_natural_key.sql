-- +goose Up
-- Stable identity for discography rows. Sync used to full-replace per artist,
-- which reminted every row id on every enrich — and the manager's album pages
-- address catalog-only releases as d<row id>, so open pages and bookmarks
-- broke on refresh. natural_key mirrors Sync's dedup key (canonical release
-- group id, else a normalized title+year fingerprint) so Sync can upsert in
-- place; the unique index also stops concurrent syncs duplicating rows.
ALTER TABLE artist_discography ADD COLUMN natural_key text NOT NULL DEFAULT '';

-- Backfill: canonical rows get their canonical id (the next sync matches and
-- keeps the row id); the few canonical-less rows get a placeholder that the
-- next sync retires through the stale-delete pass.
UPDATE artist_discography
SET natural_key = CASE WHEN canonical_id <> '' THEN canonical_id ELSE 'row:' || id END;

CREATE UNIQUE INDEX idx_artist_discography_natural_key
    ON artist_discography (artist_id, natural_key);

-- The profiles page counts assigned media per profile; give it an index
-- instead of a per-profile sequential scan.
CREATE INDEX idx_media_items_quality_profile
    ON media_items (quality_profile_id)
    WHERE quality_profile_id IS NOT NULL;

-- +goose Down
DROP INDEX idx_media_items_quality_profile;
DROP INDEX idx_artist_discography_natural_key;
ALTER TABLE artist_discography DROP COLUMN natural_key;
