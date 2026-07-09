-- Fuzzy + token search across all entity types.
-- Combines pg_trgm similarity, tsvector matching, and case-insensitive prefix
-- so it works for typos, partial words, and short queries alike. Ranked by
-- greatest of those signals; popularity is a tie-breaker.

-- name: SearchMediaByType :many
SELECT mi.*
FROM media_item_cards mi
WHERE mi.media_type = $2
  AND (
    lower(mi.title) % lower($1)
    OR mi.search_vector @@ websearch_to_tsquery('english', $1)
    OR lower(mi.title) ILIKE lower($1) || '%'
  )
ORDER BY
  greatest(
    similarity(lower(mi.title), lower($1)),
    ts_rank(mi.search_vector, websearch_to_tsquery('english', $1)),
    CASE WHEN lower(mi.title) ILIKE lower($1) || '%' THEN 1.0 ELSE 0.0 END
  ) DESC,
  mi.title ASC
LIMIT $3 OFFSET $4;

-- name: SearchMediaByTypeCount :one
SELECT count(*)
FROM media_item_cards mi
WHERE mi.media_type = $2
  AND (
    lower(mi.title) % lower($1)
    OR mi.search_vector @@ websearch_to_tsquery('english', $1)
    OR lower(mi.title) ILIKE lower($1) || '%'
  );

-- name: SearchAllMedia :many
SELECT mi.*
FROM media_item_cards mi
WHERE (
    lower(mi.title) % lower($1)
    OR mi.search_vector @@ websearch_to_tsquery('english', $1)
    OR lower(mi.title) ILIKE lower($1) || '%'
  )
ORDER BY
  greatest(
    similarity(lower(mi.title), lower($1)),
    ts_rank(mi.search_vector, websearch_to_tsquery('english', $1)),
    CASE WHEN lower(mi.title) ILIKE lower($1) || '%' THEN 1.0 ELSE 0.0 END
  ) DESC,
  mi.title ASC
LIMIT $2 OFFSET $3;

-- name: SearchPeople :many
-- No trigram % arm here (unlike the other buckets): at 101k people rows it
-- forced a parallel seq scan on every search (~150ms items + ~150ms count);
-- without it both queries take the BitmapOr(tsvector + prefix) path (~20ms).
-- Typo tolerance narrows to token/prefix matches — the tsvector arm indexes
-- also_known_as aliases at weight B, which covers most alias fuzziness.
-- The count query must keep the same arms so totals match the items.
SELECT p.*,
       (SELECT count(*) FROM media_cast WHERE person_id = p.id)::int AS cast_count,
       (SELECT count(*) FROM media_crew WHERE person_id = p.id)::int AS crew_count
FROM people p
WHERE (
    p.search_vector @@ websearch_to_tsquery('simple', sqlc.arg(query))
    OR lower(p.name) ILIKE lower(sqlc.arg(query)) || '%'
  )
ORDER BY
  greatest(
    ts_rank(p.search_vector, websearch_to_tsquery('simple', sqlc.arg(query))),
    CASE WHEN lower(p.name) ILIKE lower(sqlc.arg(query)) || '%' THEN 1.0 ELSE 0.0 END
  ) DESC,
  p.popularity DESC,
  p.name ASC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: SearchPeopleCount :one
SELECT count(*)
FROM people p
WHERE (
    p.search_vector @@ websearch_to_tsquery('simple', sqlc.arg(query))
    OR lower(p.name) ILIKE lower(sqlc.arg(query)) || '%'
  );

-- name: SearchAlbums :many
-- All three match arms stay (the % arm is the only typo path — 'nevermnd'
-- matches 42 albums via % and zero via tsvector/prefix). The derived table
-- ranks + limits over albums alone so the artists/media_items joins run as
-- pkey nested loops on <= LIMIT rows instead of hash joins over every artist.
SELECT a.*,
       mi.id AS artist_media_item_id,
       mi.public_id AS artist_media_item_public_id,
       mi.title AS artist_name,
       mi.slug AS artist_slug
FROM (
  SELECT al.*
  FROM albums al
  WHERE (
      lower(al.title) % lower($1)
      OR al.search_vector @@ websearch_to_tsquery('simple', $1)
      OR lower(al.title) ILIKE lower($1) || '%'
    )
  ORDER BY
    greatest(
      similarity(lower(al.title), lower($1)),
      ts_rank(al.search_vector, websearch_to_tsquery('simple', $1)),
      CASE WHEN lower(al.title) ILIKE lower($1) || '%' THEN 1.0 ELSE 0.0 END
    ) DESC,
    al.title ASC
  LIMIT $2 OFFSET $3
) a
JOIN artists ar ON ar.id = a.artist_id
JOIN media_item_cards mi ON mi.id = ar.media_item_id
ORDER BY
  greatest(
    similarity(lower(a.title), lower($1)),
    ts_rank(a.search_vector, websearch_to_tsquery('simple', $1)),
    CASE WHEN lower(a.title) ILIKE lower($1) || '%' THEN 1.0 ELSE 0.0 END
  ) DESC,
  a.title ASC;

-- name: SearchAlbumsCount :one
SELECT count(*)
FROM albums a
WHERE (
    lower(a.title) % lower($1)
    OR a.search_vector @@ websearch_to_tsquery('simple', $1)
    OR lower(a.title) ILIKE lower($1) || '%'
  );

-- name: SearchTracks :many
-- Two deliberate deviations from the generic search shape (measured on the
-- 240k-track prod dataset — 1.46s -> ~50ms worst common term):
--  * No trigram % arm: it forced a parallel seq scan over all tracks, and
--    short-word typos fall below the 0.3 similarity threshold anyway, so it
--    contributed nothing the tsvector + prefix arms don't cover here.
--    SearchTracksCount drops the same arm so totals stay consistent.
--  * Derived table: ranks + limits over tracks alone, then joins album/
--    artist and computes the availability EXISTS on <= LIMIT rows. Inlined,
--    the planner hashes the EXISTS over ALL 673k library_files per search.
SELECT t.*,
       a.title AS album_title,
       a.slug AS album_slug,
       a.cover_path AS album_cover_path,
       mi.id AS artist_media_item_id,
       mi.public_id AS artist_media_item_public_id,
       mi.title AS artist_name,
       mi.slug AS artist_slug,
       EXISTS (SELECT 1 FROM track_files tf JOIN library_files lf ON lf.id = tf.library_file_id WHERE tf.track_id = t.id AND lf.deleted_at IS NULL) AS available
FROM (
  SELECT tr.*
  FROM tracks tr
  WHERE (
      tr.search_vector @@ websearch_to_tsquery('simple', sqlc.arg(query))
      OR lower(tr.title) ILIKE lower(sqlc.arg(query)) || '%'
    )
  ORDER BY
    greatest(
      ts_rank(tr.search_vector, websearch_to_tsquery('simple', sqlc.arg(query))),
      CASE WHEN lower(tr.title) ILIKE lower(sqlc.arg(query)) || '%' THEN 1.0 ELSE 0.0 END
    ) DESC,
    tr.title ASC
  LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset')
) t
JOIN albums a ON a.id = t.album_id
JOIN artists ar ON ar.id = a.artist_id
JOIN media_item_cards mi ON mi.id = ar.media_item_id
ORDER BY
  greatest(
    ts_rank(t.search_vector, websearch_to_tsquery('simple', sqlc.arg(query))),
    CASE WHEN lower(t.title) ILIKE lower(sqlc.arg(query)) || '%' THEN 1.0 ELSE 0.0 END
  ) DESC,
  t.title ASC;

-- name: SearchTracksCount :one
SELECT count(*)
FROM tracks t
WHERE (
    t.search_vector @@ websearch_to_tsquery('simple', sqlc.arg(query))
    OR lower(t.title) ILIKE lower(sqlc.arg(query)) || '%'
  );

-- name: SearchCollections :many
SELECT c.*
FROM collections c
WHERE (
    lower(c.name) % lower($1)
    OR c.search_vector @@ websearch_to_tsquery('english', $1)
    OR lower(c.name) ILIKE lower($1) || '%'
  )
ORDER BY
  greatest(
    similarity(lower(c.name), lower($1)),
    ts_rank(c.search_vector, websearch_to_tsquery('english', $1)),
    CASE WHEN lower(c.name) ILIKE lower($1) || '%' THEN 1.0 ELSE 0.0 END
  ) DESC,
  c.name ASC
LIMIT $2 OFFSET $3;

-- name: SearchCollectionsCount :one
SELECT count(*)
FROM collections c
WHERE (
    lower(c.name) % lower($1)
    OR c.search_vector @@ websearch_to_tsquery('english', $1)
    OR lower(c.name) ILIKE lower($1) || '%'
  );
