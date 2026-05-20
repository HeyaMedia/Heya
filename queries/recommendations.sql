-- name: CreateMediaRecommendation :exec
INSERT INTO media_recommendations (media_item_id, recommended_tmdb_id, title, poster_path, media_type, vote_average, release_date)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (media_item_id, recommended_tmdb_id) DO UPDATE SET
  title = EXCLUDED.title,
  poster_path = EXCLUDED.poster_path,
  vote_average = EXCLUDED.vote_average;

-- name: ListMediaRecommendations :many
SELECT * FROM media_recommendations WHERE media_item_id = $1 ORDER BY vote_average DESC;

-- name: ListMediaRecommendationsWithLibrary :many
SELECT mr.*,
  mi.id as local_media_item_id,
  mi.poster_path as local_poster_path
FROM media_recommendations mr
LEFT JOIN media_items mi ON mi.external_ids::jsonb @> jsonb_build_object('tmdb', mr.recommended_tmdb_id::text)
WHERE mr.media_item_id = $1
ORDER BY (mi.id IS NOT NULL) DESC, mr.vote_average DESC;

-- Top recommendations for home page: items recommended by multiple sources, weighted by vote
-- name: ListTopRecommendations :many
SELECT mr.recommended_tmdb_id, mr.title, mr.poster_path, mr.media_type, mr.vote_average, mr.release_date,
  mi.id as local_media_item_id,
  mi.slug as local_slug,
  mi.poster_path as local_poster_path,
  count(DISTINCT mr.media_item_id)::int as source_count
FROM media_recommendations mr
LEFT JOIN media_items mi ON mi.external_ids::jsonb @> jsonb_build_object('tmdb', mr.recommended_tmdb_id::text)
GROUP BY mr.recommended_tmdb_id, mr.title, mr.poster_path, mr.media_type, mr.vote_average, mr.release_date,
         mi.id, mi.slug, mi.poster_path
ORDER BY source_count DESC, mr.vote_average DESC
LIMIT $1;
