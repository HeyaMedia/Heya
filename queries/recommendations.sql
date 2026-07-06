-- name: CreateMediaRecommendation :exec
INSERT INTO media_recommendations (media_item_id, external_ids, title, poster_path, media_type, vote_average, release_date)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (media_item_id, title, media_type) DO UPDATE SET
  external_ids = EXCLUDED.external_ids,
  poster_path = EXCLUDED.poster_path,
  vote_average = EXCLUDED.vote_average;

-- name: ListMediaRecommendations :many
SELECT * FROM media_recommendations WHERE media_item_id = $1 ORDER BY vote_average DESC;

-- name: DeleteMediaRecommendationsByItem :exec
DELETE FROM media_recommendations WHERE media_item_id = $1;

-- name: ListMediaRecommendationsWithLibrary :many
SELECT mr.*,
  mi.id as local_media_item_id,
  mi.slug as local_slug,
  mi.poster_path as local_poster_path
FROM media_recommendations mr
LEFT JOIN media_items mi ON mi.external_ids @> mr.external_ids AND mr.external_ids != '{}'
WHERE mr.media_item_id = $1
ORDER BY (mi.id IS NOT NULL) DESC, mr.vote_average DESC;

-- Top recommendations for home page: items recommended by multiple sources,
-- weighted by vote. Aggregate media_recommendations alone first, then join
-- media_items only for the top-N groups — the inlined form ran one GIN probe
-- per rec row (30k) and count(DISTINCT) forced a 30k-row jsonb-keyed sort
-- (~900ms vs ~35ms). count(*) is equivalent to count(DISTINCT media_item_id):
-- the UNIQUE (media_item_id, title, media_type) constraint (which the upsert
-- in CreateMediaRecommendation depends on) plus title/media_type in the GROUP
-- BY means one media_item_id can't repeat within a group.
-- name: ListTopRecommendations :many
WITH agg AS (
  SELECT mr.external_ids, mr.title, mr.poster_path, mr.media_type, mr.vote_average, mr.release_date,
         count(*)::int AS source_count
  FROM media_recommendations mr
  GROUP BY mr.external_ids, mr.title, mr.poster_path, mr.media_type, mr.vote_average, mr.release_date
  ORDER BY count(*) DESC, mr.vote_average DESC
  LIMIT $1
)
SELECT agg.external_ids, agg.title, agg.poster_path, agg.media_type, agg.vote_average, agg.release_date,
  mi.id as local_media_item_id,
  mi.slug as local_slug,
  mi.poster_path as local_poster_path,
  agg.source_count
FROM agg
LEFT JOIN media_items mi ON mi.external_ids @> agg.external_ids AND agg.external_ids != '{}'
ORDER BY agg.source_count DESC, agg.vote_average DESC;
