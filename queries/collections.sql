-- name: CreateCollection :one
INSERT INTO collections (external_ids, name, overview, poster_path, backdrop_path, parts)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: FindCollectionByName :one
SELECT * FROM collections WHERE name = $1 LIMIT 1;

-- name: UpdateCollection :exec
-- parts is refreshed only when the incoming list is non-empty, so a movie that
-- enriches without the collection block (partial upstream data) can't blank out
-- a membership list an earlier sibling already populated.
UPDATE collections
SET external_ids = $2, overview = $3, poster_path = $4, backdrop_path = $5,
    parts = CASE WHEN jsonb_array_length($6::jsonb) > 0 THEN $6::jsonb ELSE collections.parts END
WHERE id = $1;

-- name: SetMovieCollection :exec
UPDATE movies SET collection_id = $2 WHERE media_item_id = $1;

-- name: GetCollectionByID :one
SELECT * FROM collections WHERE id = $1;

-- name: ListMoviesByTmdbIDs :many
-- Resolves a collection's franchise-part tmdb ids to local movies (owned vs
-- missing on the collection page). external_ids->>'tmdb' is the string form the
-- enrich mapper writes; parsing back to a part happens in the service.
SELECT mi.id, mi.slug, mi.external_ids
FROM media_item_cards mi
JOIN media_item_external_ids ei ON ei.media_item_id = mi.id
WHERE mi.media_type = 'movie'
  AND ei.provider = 'tmdb'
  AND ei.external_id = ANY(@tmdb_ids::text[]);

-- name: ListCollectionMovies :many
SELECT mi.*
FROM media_item_cards mi
JOIN movies m ON m.media_item_id = mi.id
WHERE m.collection_id = $1
ORDER BY m.release_date NULLS LAST;

-- name: ListCollectionGenres :many
-- Aggregated genres across the collection's owned movies (TMDB collections
-- carry no genres of their own), most-common first for chip display.
SELECT genre, count(*)::int AS count
FROM (
  SELECT unnest(m.genres)::text AS genre
  FROM movies m
  WHERE m.collection_id = $1
) g
WHERE genre <> ''
GROUP BY genre
ORDER BY count DESC, genre;

-- name: ListCollectionKeywords :many
-- Aggregated keyword tags across the collection's owned movies, most-common
-- first (capped for display). The finer folksonomy beyond genres — e.g.
-- "22nd century", "alien planet".
SELECT k.name, count(*)::int AS count
FROM keywords k
JOIN media_keywords mk ON mk.keyword_id = k.id
JOIN movies m ON m.media_item_id = mk.media_item_id
WHERE m.collection_id = $1
GROUP BY k.name
ORDER BY count DESC, k.name
LIMIT 30;

-- name: ListAllCollections :many
SELECT c.*,
       (SELECT count(*) FROM movies m WHERE m.collection_id = c.id)::int AS movie_count
FROM collections c
ORDER BY c.name
LIMIT $1 OFFSET $2;

-- name: CountAllCollections :one
SELECT count(*) FROM collections;

-- name: ListCollectionsWithLocalMedia :many
SELECT c.id, c.name, c.poster_path,
       count(m.id)::int AS movie_count
FROM collections c
JOIN movies m ON m.collection_id = c.id
GROUP BY c.id, c.name, c.poster_path
HAVING count(m.id) > 0
ORDER BY c.name;
