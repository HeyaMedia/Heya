-- +goose Up
-- Stable edition-group entity for music acquisition. Discography rows are
-- replace-synced and can be rekeyed; monitoring, decisions, and wanted all
-- key off THIS table so user intent survives catalog churn. Rows are
-- created lazily (backfill here, discography sync upserts going forward)
-- and never stale-deleted.
CREATE TABLE manager_music_targets (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    artist_id          bigint NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    album_type         text NOT NULL DEFAULT '',
    edition_key        text NOT NULL,
    title              text NOT NULL,
    year               text NOT NULL DEFAULT '',
    monitored          boolean NOT NULL,
    monitor_updated_at timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (artist_id, album_type, edition_key)
);

-- The decisions column has existed since 00082 (nullable, FK deferred to
-- this migration by design).
ALTER TABLE manager_decisions
    ADD CONSTRAINT manager_decisions_music_target_fk
    FOREIGN KEY (music_target_id) REFERENCES manager_music_targets(id) ON DELETE SET NULL;

-- Backfill: project every existing discography group. Monitoring defaults
-- by primary type — albums and EPs are wanted, singles/live/etc. are not.
INSERT INTO manager_music_targets (artist_id, album_type, edition_key, title, year, monitored)
SELECT DISTINCT ON (d.artist_id, d.album_type, COALESCE(NULLIF(d.edition_key, ''), lower(d.title)))
       d.artist_id, d.album_type,
       COALESCE(NULLIF(d.edition_key, ''), lower(d.title)),
       d.title, COALESCE(d.year, ''),
       d.album_type IN ('album', 'ep')
FROM artist_discography d
ORDER BY d.artist_id, d.album_type, COALESCE(NULLIF(d.edition_key, ''), lower(d.title)), length(d.title)
ON CONFLICT DO NOTHING;

-- +goose Down
ALTER TABLE manager_decisions DROP CONSTRAINT IF EXISTS manager_decisions_music_target_fk;
DROP TABLE IF EXISTS manager_music_targets;
