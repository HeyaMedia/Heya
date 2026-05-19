-- name: CreateMediaRecommendation :exec
INSERT INTO media_recommendations (media_item_id, recommended_tmdb_id, title, poster_path, media_type, vote_average, release_date)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (media_item_id, recommended_tmdb_id) DO UPDATE SET
  title = EXCLUDED.title,
  poster_path = EXCLUDED.poster_path,
  vote_average = EXCLUDED.vote_average;

-- name: ListMediaRecommendations :many
SELECT * FROM media_recommendations WHERE media_item_id = $1 ORDER BY vote_average DESC;
