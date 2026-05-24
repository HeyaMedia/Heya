-- +goose Up

-- Per-item enrichment state. The match step writes a search-only stub and
-- stamps matched_at + enrichment_status='pending'. The enrich worker fills
-- each component (base / people / extras / images / structure) and stamps
-- the corresponding *_enriched_at column, flipping enrichment_status to
-- 'partial' or 'complete' as components land. This gives the UI a real
-- per-item signal of what's still pending and lets a failed enrich resume
-- without redoing successful components.
--
-- enrichment_status values:
--   pending   – matched stub only, nothing enriched yet
--   partial   – at least one component enriched, more pending
--   complete  – every applicable component enriched
--   failed    – last enrich attempt errored; last_enrich_error has details
ALTER TABLE media_items
    ADD COLUMN matched_at             TIMESTAMPTZ,
    ADD COLUMN enrichment_status      TEXT        NOT NULL DEFAULT 'pending',
    ADD COLUMN base_enriched_at       TIMESTAMPTZ,
    ADD COLUMN people_enriched_at     TIMESTAMPTZ,
    ADD COLUMN extras_enriched_at     TIMESTAMPTZ,
    ADD COLUMN images_enriched_at     TIMESTAMPTZ,
    ADD COLUMN structure_enriched_at  TIMESTAMPTZ,
    ADD COLUMN last_enrich_attempt_at TIMESTAMPTZ,
    ADD COLUMN last_enrich_error      TEXT        NOT NULL DEFAULT '';

CREATE INDEX idx_media_items_enrichment_status
    ON media_items (library_id, enrichment_status);

CREATE INDEX idx_media_items_enrichment_pending
    ON media_items (media_type, metadata_refreshed_at NULLS FIRST)
    WHERE enrichment_status != 'complete';

-- Replace the coarse artists.enriched_at (added in 00011) with per-component
-- timestamps that mirror the media_items split. No compat shim — we wipe
-- the DB and re-add libraries during dev.
DROP INDEX IF EXISTS idx_artists_enriched_at;
ALTER TABLE artists DROP COLUMN enriched_at;
ALTER TABLE artists
    ADD COLUMN discography_enriched_at TIMESTAMPTZ,
    ADD COLUMN cover_art_enriched_at   TIMESTAMPTZ;

CREATE INDEX idx_artists_discography_enriched_at
    ON artists (discography_enriched_at NULLS FIRST);

-- +goose Down

DROP INDEX IF EXISTS idx_artists_discography_enriched_at;
ALTER TABLE artists DROP COLUMN cover_art_enriched_at;
ALTER TABLE artists DROP COLUMN discography_enriched_at;
ALTER TABLE artists ADD COLUMN enriched_at TIMESTAMPTZ;
CREATE INDEX idx_artists_enriched_at ON artists (enriched_at NULLS FIRST);

DROP INDEX IF EXISTS idx_media_items_enrichment_pending;
DROP INDEX IF EXISTS idx_media_items_enrichment_status;
ALTER TABLE media_items
    DROP COLUMN last_enrich_error,
    DROP COLUMN last_enrich_attempt_at,
    DROP COLUMN structure_enriched_at,
    DROP COLUMN images_enriched_at,
    DROP COLUMN extras_enriched_at,
    DROP COLUMN people_enriched_at,
    DROP COLUMN base_enriched_at,
    DROP COLUMN enrichment_status,
    DROP COLUMN matched_at;
