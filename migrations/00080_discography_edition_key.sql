-- +goose Up
-- Edition grouping: "(Deluxe)" / "(Extended)" / "(Anniversary Edition)"
-- variants of one release share an edition_key (base title, normalized) so
-- the manager counts the release once — owning the Deluxe means the album
-- isn't missing. Computed in Go at sync time (internal/discography.EditionKey).
ALTER TABLE artist_discography ADD COLUMN edition_key text DEFAULT ''::text NOT NULL;
CREATE INDEX artist_discography_edition_idx ON artist_discography (artist_id, edition_key);

-- +goose Down
DROP INDEX artist_discography_edition_idx;
ALTER TABLE artist_discography DROP COLUMN edition_key;
