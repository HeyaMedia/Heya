-- Per-user track ratings. UI displays 5 stars in half-step increments;
-- the column stores 1..10 so half-stars round-trip cleanly.
--
-- List/Count take a [min_rating, max_rating] band (not just a floor): the
-- Favorites page's reaction bands (down 1-3, liked 6-8, loved 9-10) page
-- server-side through the same random-access catalog as everything else.

-- name: SetUserTrackRating :exec
-- Upsert: writes a rating or replaces the existing one. Touch updated_at
-- on conflict so "recently rated" ordering reflects last edit.
INSERT INTO user_track_ratings (user_id, track_id, rating)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, track_id) DO UPDATE
    SET rating = EXCLUDED.rating,
        updated_at = now();

-- name: DeleteUserTrackRating :exec
DELETE FROM user_track_ratings
WHERE user_id = $1 AND track_id = $2;

-- name: GetUserTrackRating :one
SELECT rating FROM user_track_ratings
WHERE user_id = $1 AND track_id = $2;

-- name: GetUserTrackRatingsForTrackIDs :many
-- Bulk lookup for a track-list view — returns (track_id, rating) for the
-- subset of input ids the user has rated. Driver passes the rest as nulls
-- in its merge.
SELECT track_id, rating
FROM user_track_ratings
WHERE user_id = $1 AND track_id = ANY($2::bigint[]);

-- name: ListUserRatedTracks :many
-- Paginated list of every track the user has rated, sorted by rating
-- desc then most-recently-rated. Carries album+artist context so the FE
-- renders self-contained rows. Filters by min_rating for the Favorites
-- view (use 1 to get everything rated).
--
-- Enrichment columns (best file, facets, play stats, composer) mirror
-- ListPlaylistTracks — see the comment there for the LATERAL-avoidance and
-- jsonb-extraction rationale. Kept as direct joins without a keys-subquery
-- wrap: the joined set is one user's rated tracks (thousands at most), not
-- the whole catalog.
SELECT t.id              AS track_id,
       t.title           AS track_title,
       t.duration        AS duration,
       t.disc_number     AS disc_number,
       t.track_number    AS track_number,
       t.explicit        AS explicit,
       al.id             AS album_id,
       al.title          AS album_title,
       al.slug           AS album_slug,
       al.cover_path     AS album_cover_path,
       al.year           AS album_year,
       al.genres         AS album_genres,
       al.label          AS album_label,
       al.release_date   AS album_release_date,
       a.id              AS artist_id,
       a.name            AS artist_name,
       mi.slug           AS artist_slug,
       utr.rating        AS rating,
       utr.updated_at    AS rated_at,
       EXISTS (SELECT 1 FROM track_files tf JOIN library_files lf ON lf.id = tf.library_file_id WHERE tf.track_id = t.id AND lf.deleted_at IS NULL) AS available,
       bf.format          AS format,
       bf.bitrate_kbps    AS bitrate_kbps,
       bf.sample_rate_hz  AS sample_rate_hz,
       bf.bit_depth       AS bit_depth,
       bf.channels        AS channels,
       bf.size_bytes      AS size_bytes,
       bf.integrated_lufs AS integrated_lufs,
       blf.created_at     AS library_added_at,
       tfc.bpm            AS bpm,
       tfc.key_root       AS key_root,
       tfc.key_mode       AS key_mode,
       COALESCE((SELECT string_agg((ac.value ->> 'name') || COALESCE(ac.value ->> 'join_phrase', ''), '' ORDER BY ac.ord)
          FROM jsonb_array_elements(CASE WHEN jsonb_typeof(t.artist_credits) = 'array' THEN t.artist_credits ELSE '[]'::jsonb END)
               WITH ORDINALITY AS ac(value, ord)), '')::text AS artists_display,
       COALESCE((SELECT string_agg(DISTINCT cr.value ->> 'artist_name', ', ')
          FROM jsonb_array_elements(CASE WHEN jsonb_typeof(t.credits) = 'array' THEN t.credits ELSE '[]'::jsonb END) AS cr(value)
         WHERE cr.value ->> 'role' = 'composer'), '')::text AS composer,
       (SELECT count(*) FROM play_events pe WHERE pe.user_id = utr.user_id AND pe.track_id = t.id) AS play_count,
       (SELECT max(pe.played_at) FROM play_events pe WHERE pe.user_id = utr.user_id AND pe.track_id = t.id)::timestamptz AS last_played_at
FROM user_track_ratings utr
JOIN tracks      t  ON t.id  = utr.track_id
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
LEFT JOIN track_files bf ON bf.id = (
    SELECT tf.id
    FROM track_files tf
    JOIN library_files lf ON lf.id = tf.library_file_id
    WHERE tf.track_id = t.id AND lf.deleted_at IS NULL
    ORDER BY tf.quality_score DESC, tf.id ASC
    LIMIT 1
)
LEFT JOIN library_files blf ON blf.id = bf.library_file_id
LEFT JOIN track_facets tfc ON tfc.track_id = t.id
WHERE utr.user_id = sqlc.arg(user_id)
  AND utr.rating  >= sqlc.arg(min_rating)
  AND utr.rating  <= sqlc.arg(max_rating)
ORDER BY utr.rating DESC, utr.updated_at DESC
LIMIT sqlc.arg(track_limit) OFFSET sqlc.arg(offset_);

-- name: CountUserRatedTracks :one
SELECT count(*) FROM user_track_ratings
WHERE user_id = sqlc.arg(user_id)
  AND rating >= sqlc.arg(min_rating)
  AND rating <= sqlc.arg(max_rating);

-- name: GetUserRatedTracksStats :one
-- Aggregates for a rating band's ledger strip (Loved Songs hero): track
-- count, total runtime, distinct artists, most recent rating touch. One
-- pass over the user's rated set — small by construction.
SELECT count(*)                             AS track_count,
       COALESCE(sum(t.duration), 0)::bigint AS total_duration,
       count(DISTINCT al.artist_id)         AS artist_count,
       max(utr.updated_at)::timestamptz     AS last_rated_at
FROM user_track_ratings utr
JOIN tracks t  ON t.id  = utr.track_id
JOIN albums al ON al.id = t.album_id
WHERE utr.user_id = sqlc.arg(user_id)
  AND utr.rating >= sqlc.arg(min_rating)
  AND utr.rating <= sqlc.arg(max_rating);

-- name: GetUserFavoritesThreshold :one
SELECT favorites_threshold FROM users WHERE id = $1;

-- name: UpdateUserFavoritesThreshold :exec
UPDATE users SET favorites_threshold = $2 WHERE id = $1;
