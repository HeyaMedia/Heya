-- name: CreateMediaRecommendation :exec
INSERT INTO media_recommendations (media_item_id, external_ids, title, poster_path, media_type, vote_average, provider_score, release_date)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (media_item_id, title, media_type) DO UPDATE SET
  external_ids = EXCLUDED.external_ids,
  poster_path = EXCLUDED.poster_path,
  vote_average = EXCLUDED.vote_average,
  provider_score = EXCLUDED.provider_score;

-- name: ListMediaRecommendations :many
SELECT * FROM media_recommendations WHERE media_item_id = $1 ORDER BY provider_score DESC, vote_average DESC;

-- name: DeleteMediaRecommendationsByItem :exec
DELETE FROM media_recommendations WHERE media_item_id = $1;

-- name: ListMediaRecommendationsWithLibrary :many
SELECT mr.*,
  COALESCE(mi.id, 0)::bigint as local_media_item_id,
  COALESCE(mi.public_id::text, '')::text as local_public_id,
  COALESCE(mi.slug, '')::text as local_slug,
  COALESCE(mi.poster_path, '')::text as local_poster_path
FROM media_recommendations mr
LEFT JOIN LATERAL (
  SELECT local_mi.id, local_mi.public_id, local_mi.slug, local_mi.poster_path
  FROM jsonb_each_text(mr.external_ids) AS wanted(provider, external_id)
  JOIN media_item_external_ids ei
    ON ei.provider = wanted.provider
   AND ei.external_id = wanted.external_id
  JOIN media_item_cards local_mi ON local_mi.id = ei.media_item_id
  WHERE mr.external_ids != '{}'
    AND wanted.provider IN ('tmdb', 'imdb', 'tvdb')
  ORDER BY CASE wanted.provider WHEN 'tmdb' THEN 0 WHEN 'imdb' THEN 1 WHEN 'tvdb' THEN 2 ELSE 3 END,
           local_mi.id
  LIMIT 1
) mi ON true
WHERE mr.media_item_id = $1
ORDER BY (mi.id IS NOT NULL) DESC, mr.provider_score DESC, mr.vote_average DESC;

-- Top recommendations for home page: items recommended by multiple sources,
-- ranked by provider-native score with normalized vote as a secondary signal.
-- Aggregate media_recommendations alone first, then join
-- media_items only for the top-N groups — the inlined form ran one GIN probe
-- per rec row (30k) and count(DISTINCT) forced a 30k-row jsonb-keyed sort
-- (~900ms vs ~35ms). count(*) is equivalent to count(DISTINCT media_item_id):
-- the UNIQUE (media_item_id, title, media_type) constraint (which the upsert
-- in CreateMediaRecommendation depends on) plus title/media_type in the GROUP
-- BY means one media_item_id can't repeat within a group.
-- name: ListTopRecommendations :many
WITH agg AS (
  SELECT mr.external_ids, mr.title, mr.poster_path, mr.media_type, mr.vote_average, mr.provider_score, mr.release_date,
         count(*)::int AS source_count
  FROM media_recommendations mr
  GROUP BY mr.external_ids, mr.title, mr.poster_path, mr.media_type, mr.vote_average, mr.provider_score, mr.release_date
  ORDER BY count(*) DESC, mr.provider_score DESC, mr.vote_average DESC
  LIMIT $1
)
SELECT agg.external_ids, agg.title, agg.poster_path, agg.media_type, agg.vote_average, agg.provider_score, agg.release_date,
  COALESCE(mi.id, 0)::bigint as local_media_item_id,
  COALESCE(mi.public_id::text, '')::text as local_public_id,
  COALESCE(mi.slug, '')::text as local_slug,
  COALESCE(mi.poster_path, '')::text as local_poster_path,
  agg.source_count
FROM agg
LEFT JOIN LATERAL (
  SELECT local_mi.id, local_mi.public_id, local_mi.slug, local_mi.poster_path
  FROM jsonb_each_text(agg.external_ids) AS wanted(provider, external_id)
  JOIN media_item_external_ids ei
    ON ei.provider = wanted.provider
   AND ei.external_id = wanted.external_id
  JOIN media_item_cards local_mi ON local_mi.id = ei.media_item_id
  WHERE agg.external_ids != '{}'
    AND wanted.provider IN ('tmdb', 'imdb', 'tvdb')
  ORDER BY CASE wanted.provider WHEN 'tmdb' THEN 0 WHEN 'imdb' THEN 1 WHEN 'tvdb' THEN 2 ELSE 3 END,
           local_mi.id
  LIMIT 1
) mi ON true
ORDER BY agg.source_count DESC, agg.provider_score DESC, agg.vote_average DESC;
