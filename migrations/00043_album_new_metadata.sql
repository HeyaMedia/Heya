-- +goose Up
-- Album slice of the heya.media 2026-07 provider expansion (TheAudioDB /
-- Bandcamp / Discogs depth). Filled on the next enrich/refresh.
--
-- description: editorial prose (English-preferred provider_description).
-- review: TheAudioDB's editorial review (annotations type=provider_review).
-- ratings: per-system provider-native ratings
--   [{system, value, scale_max, votes}] — musicbrainz is 0-5, audiodb 0-10;
--   consumers normalize by value/scale_max.
-- editions: issued pressings [{provider, title, date, country, formats[],
--   labels[{name, catalog_number}], link, ...}].
-- sales: TheAudioDB's reported sales figure.
ALTER TABLE albums ADD COLUMN description text NOT NULL DEFAULT '';
ALTER TABLE albums ADD COLUMN review text NOT NULL DEFAULT '';
ALTER TABLE albums ADD COLUMN ratings jsonb NOT NULL DEFAULT '[]';
ALTER TABLE albums ADD COLUMN editions jsonb NOT NULL DEFAULT '[]';
ALTER TABLE albums ADD COLUMN sales bigint NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE albums DROP COLUMN sales;
ALTER TABLE albums DROP COLUMN editions;
ALTER TABLE albums DROP COLUMN ratings;
ALTER TABLE albums DROP COLUMN review;
ALTER TABLE albums DROP COLUMN description;
