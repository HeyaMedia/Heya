-- +goose Up
-- Provenance strip: which upstream providers fed this artist's canonical
-- document (heya.media freshness.providers keys, e.g. lastfm / audiodb /
-- tidal / musicbrainz). Filled on the next refresh.
ALTER TABLE artists ADD COLUMN metadata_sources text[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE artists DROP COLUMN metadata_sources;
