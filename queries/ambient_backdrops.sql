-- name: SampleAmbientBackdrops :many
-- Ambient-background candidates: random media items of the requested types
-- that have any artwork. has_backdrop tells the FE whether to request the
-- backdrop image or fall back to the poster (books rarely ship backdrops).
-- ORDER BY random() is fine here — the table is library-sized (thousands),
-- the endpoint is hit once per route-context change, and results are cached
-- client-side.
SELECT
  e.id,
  e.public_id,
  e.media_type,
  COALESCE(p.title, '') AS title,
  e.slug,
  (
    COALESCE(p.backdrop_path, '') <> ''
    OR EXISTS (
      SELECT 1 FROM media_assets a
      WHERE a.media_item_id = e.id AND a.asset_type = 'backdrop'
    )
  ) AS has_backdrop
FROM media_items e
JOIN media_item_profiles p ON p.media_item_id = e.id
-- text[] comparison: pgx has no encode plan for a []MediaType enum array
-- (unknown array OID), and this query never needs the enum index anyway.
WHERE e.media_type::text = ANY (sqlc.arg(media_types)::text[])
  AND (
    COALESCE(p.backdrop_path, '') <> ''
    OR COALESCE(p.poster_path, '') <> ''
    OR EXISTS (
      SELECT 1 FROM media_assets a
      WHERE a.media_item_id = e.id AND a.asset_type IN ('backdrop', 'poster')
    )
  )
ORDER BY random()
LIMIT sqlc.arg(max_items);
