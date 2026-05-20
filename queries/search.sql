-- Fuzzy + token search across all entity types.
-- Combines pg_trgm similarity, tsvector matching, and case-insensitive prefix
-- so it works for typos, partial words, and short queries alike. Ranked by
-- greatest of those signals; popularity is a tie-breaker.

-- name: SearchMediaByType :many
SELECT mi.*
FROM media_items mi
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
FROM media_items mi
WHERE mi.media_type = $2
  AND (
    lower(mi.title) % lower($1)
    OR mi.search_vector @@ websearch_to_tsquery('english', $1)
    OR lower(mi.title) ILIKE lower($1) || '%'
  );

-- name: SearchAllMedia :many
SELECT mi.*
FROM media_items mi
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
SELECT p.*,
       (SELECT count(*) FROM media_cast WHERE person_id = p.id)::int AS cast_count,
       (SELECT count(*) FROM media_crew WHERE person_id = p.id)::int AS crew_count
FROM people p
WHERE (
    lower(p.name) % lower($1)
    OR p.search_vector @@ websearch_to_tsquery('simple', $1)
    OR lower(p.name) ILIKE lower($1) || '%'
  )
ORDER BY
  greatest(
    similarity(lower(p.name), lower($1)),
    ts_rank(p.search_vector, websearch_to_tsquery('simple', $1)),
    CASE WHEN lower(p.name) ILIKE lower($1) || '%' THEN 1.0 ELSE 0.0 END
  ) DESC,
  p.popularity DESC,
  p.name ASC
LIMIT $2 OFFSET $3;

-- name: SearchPeopleCount :one
SELECT count(*)
FROM people p
WHERE (
    lower(p.name) % lower($1)
    OR p.search_vector @@ websearch_to_tsquery('simple', $1)
    OR lower(p.name) ILIKE lower($1) || '%'
  );

-- name: SearchAlbums :many
SELECT a.*,
       mi.id AS artist_media_item_id,
       mi.title AS artist_name,
       mi.slug AS artist_slug
FROM albums a
JOIN artists ar ON ar.id = a.artist_id
JOIN media_items mi ON mi.id = ar.media_item_id
WHERE (
    lower(a.title) % lower($1)
    OR a.search_vector @@ websearch_to_tsquery('simple', $1)
    OR lower(a.title) ILIKE lower($1) || '%'
  )
ORDER BY
  greatest(
    similarity(lower(a.title), lower($1)),
    ts_rank(a.search_vector, websearch_to_tsquery('simple', $1)),
    CASE WHEN lower(a.title) ILIKE lower($1) || '%' THEN 1.0 ELSE 0.0 END
  ) DESC,
  a.title ASC
LIMIT $2 OFFSET $3;

-- name: SearchAlbumsCount :one
SELECT count(*)
FROM albums a
WHERE (
    lower(a.title) % lower($1)
    OR a.search_vector @@ websearch_to_tsquery('simple', $1)
    OR lower(a.title) ILIKE lower($1) || '%'
  );

-- name: SearchTracks :many
SELECT t.*,
       a.title AS album_title,
       a.cover_path AS album_cover_path,
       mi.id AS artist_media_item_id,
       mi.title AS artist_name,
       mi.slug AS artist_slug
FROM tracks t
JOIN albums a ON a.id = t.album_id
JOIN artists ar ON ar.id = a.artist_id
JOIN media_items mi ON mi.id = ar.media_item_id
WHERE (
    lower(t.title) % lower($1)
    OR t.search_vector @@ websearch_to_tsquery('simple', $1)
    OR lower(t.title) ILIKE lower($1) || '%'
  )
ORDER BY
  greatest(
    similarity(lower(t.title), lower($1)),
    ts_rank(t.search_vector, websearch_to_tsquery('simple', $1)),
    CASE WHEN lower(t.title) ILIKE lower($1) || '%' THEN 1.0 ELSE 0.0 END
  ) DESC,
  t.title ASC
LIMIT $2 OFFSET $3;

-- name: SearchTracksCount :one
SELECT count(*)
FROM tracks t
WHERE (
    lower(t.title) % lower($1)
    OR t.search_vector @@ websearch_to_tsquery('simple', $1)
    OR lower(t.title) ILIKE lower($1) || '%'
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
