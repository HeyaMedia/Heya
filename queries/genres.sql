-- name: ListAllGenres :many
SELECT genre, count(*) AS count FROM (
  SELECT unnest(genres) AS genre FROM movies
  UNION ALL
  SELECT unnest(genres) AS genre FROM tv_series
) sub
GROUP BY genre
ORDER BY genre;

-- Genre/keyword drilldowns are random-access paged (the browse grid sizes
-- its scroll track to the total and fetches whatever page the scrollbar
-- lands on), so sorting and type-filtering MUST happen server-side — the
-- client never holds the full list. Sort keys mirror the browse UI: title
-- (default), year-desc, year-asc; empty years always sink to the bottom.

-- name: ListMediaByGenre :many
SELECT mi.*
FROM media_item_cards mi
LEFT JOIN movies m ON m.media_item_id = mi.id
LEFT JOIN tv_series ts ON ts.media_item_id = mi.id
WHERE (sqlc.arg(genre)::text = ANY(m.genres) OR sqlc.arg(genre)::text = ANY(ts.genres))
  AND (sqlc.arg(media_type)::text = '' OR mi.media_type::text = sqlc.arg(media_type)::text)
ORDER BY
  CASE WHEN sqlc.arg(sort)::text = 'year-desc' THEN NULLIF(mi.year, '') END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::text = 'year-asc'  THEN NULLIF(mi.year, '') END ASC NULLS LAST,
  mi.sort_title ASC, mi.title ASC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: CountMediaByGenreByType :many
-- Per-media_type counts for one genre — feeds both the type-filter segment
-- labels and (summed / picked) the paging total.
SELECT mi.media_type::text AS media_type, count(*)::bigint AS count
FROM media_item_cards mi
LEFT JOIN movies m ON m.media_item_id = mi.id
LEFT JOIN tv_series ts ON ts.media_item_id = mi.id
WHERE (sqlc.arg(genre)::text = ANY(m.genres) OR sqlc.arg(genre)::text = ANY(ts.genres))
GROUP BY mi.media_type;

-- name: ListMediaByKeyword :many
SELECT mi.*
FROM media_item_cards mi
JOIN media_keywords mk ON mk.media_item_id = mi.id
JOIN keywords k ON k.id = mk.keyword_id
WHERE lower(k.name) = lower(sqlc.arg(keyword)::text)
  AND (sqlc.arg(media_type)::text = '' OR mi.media_type::text = sqlc.arg(media_type)::text)
ORDER BY
  CASE WHEN sqlc.arg(sort)::text = 'year-desc' THEN NULLIF(mi.year, '') END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::text = 'year-asc'  THEN NULLIF(mi.year, '') END ASC NULLS LAST,
  mi.sort_title ASC, mi.title ASC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: CountMediaByKeywordByType :many
SELECT mi.media_type::text AS media_type, count(*)::bigint AS count
FROM media_item_cards mi
JOIN media_keywords mk ON mk.media_item_id = mi.id
JOIN keywords k ON k.id = mk.keyword_id
WHERE lower(k.name) = lower(sqlc.arg(keyword)::text)
GROUP BY mi.media_type;
