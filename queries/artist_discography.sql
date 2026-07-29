-- Per-artist release-group catalog from the metadata provider. Synced as a
-- full replace on enrich/refresh; consumed by the manager's music lens.

-- name: DeleteArtistDiscography :exec
DELETE FROM artist_discography WHERE artist_id = $1;

-- name: InsertArtistDiscographyEntry :exec
INSERT INTO artist_discography (artist_id, canonical_id, title, album_type, secondary_types, release_date, year, track_count, external_ids, cover_url, album_id, edition_key)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);

-- name: ListArtistDiscography :many
SELECT * FROM artist_discography
WHERE artist_id = $1
ORDER BY release_date DESC NULLS LAST, year DESC, title ASC;

-- name: CountArtistDiscography :one
SELECT count(*) FROM artist_discography WHERE artist_id = $1;
