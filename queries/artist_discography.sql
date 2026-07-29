-- Per-artist release-group catalog from the metadata provider. Synced on
-- enrich/refresh as an upsert keyed by natural_key (stable row ids — the
-- manager addresses catalog-only releases as d<row id>), with a stale-delete
-- pass for releases the provider no longer reports.

-- name: UpsertArtistDiscographyEntry :exec
INSERT INTO artist_discography (artist_id, natural_key, canonical_id, title, album_type, secondary_types, release_date, year, track_count, external_ids, cover_url, album_id, edition_key)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (artist_id, natural_key) DO UPDATE SET
    canonical_id = EXCLUDED.canonical_id,
    title = EXCLUDED.title,
    album_type = EXCLUDED.album_type,
    secondary_types = EXCLUDED.secondary_types,
    release_date = EXCLUDED.release_date,
    year = EXCLUDED.year,
    track_count = EXCLUDED.track_count,
    external_ids = EXCLUDED.external_ids,
    cover_url = EXCLUDED.cover_url,
    album_id = EXCLUDED.album_id,
    edition_key = EXCLUDED.edition_key,
    updated_at = now();

-- name: DeleteStaleArtistDiscography :exec
DELETE FROM artist_discography
WHERE artist_id = $1 AND NOT (natural_key = ANY(@natural_keys::text[]));

-- name: ListArtistDiscography :many
SELECT * FROM artist_discography
WHERE artist_id = $1
ORDER BY release_date DESC NULLS LAST, year DESC, title ASC;

-- name: CountArtistDiscography :one
SELECT count(*) FROM artist_discography WHERE artist_id = $1;
