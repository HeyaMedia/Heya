-- +goose Up

-- enriched_at is set by RefreshMusicArtistWorker after a successful
-- heya.media enrichment. Used by scan_task to skip recently-enriched artists
-- when fanning out post-scan refresh jobs (default cooldown ~7 days).
ALTER TABLE artists ADD COLUMN enriched_at TIMESTAMPTZ;

CREATE INDEX idx_artists_enriched_at ON artists (enriched_at NULLS FIRST);

-- +goose Down
DROP INDEX IF EXISTS idx_artists_enriched_at;
ALTER TABLE artists DROP COLUMN enriched_at;
