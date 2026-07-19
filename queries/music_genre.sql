-- Genre-affinity building blocks shared by seed radio's genre_affinity knob
-- (internal/service/music_genre_affinity.go) and the "<Genre> Mix" slate
-- archetype (internal/service/music_mixes_genre.go, which hand-rolls its
-- own queries alongside the shared musicAffinityCTE constant instead of
-- using sqlc — see that file). Both need the same per-track weighted genre
-- signal: album.genres (text[], broad coverage) and track_facets.top_genres
-- (jsonb [{name,score}], ~18% coverage).

-- name: TrackGenreWeights :many
-- Batched per-track genre-weight rows: one row per (track, genre) pair from
-- BOTH sources, summed by the Go caller into one weighted profile per
-- track_id. album.genres entries carry a flat weight of 1 (no per-entry
-- confidence in that source); track_facets.top_genres entries carry their
-- classifier score (already 0..1) as the weight. Plain UNION ALL over two
-- CROSS JOIN LATERAL set-returning functions — not LEFT JOIN LATERAL
-- (sqlc v1.31 mistypes nullability on those, see
-- reference_sqlc_lateral_nullability) and not SELECT * over
-- jsonb_to_recordset (runtime 42703, see reference_sqlc_recordset_star) —
-- jsonb_array_elements + ->> extraction instead, same pattern as
-- ListGenreBuckets in track_facets.sql.
SELECT t.id AS track_id, genre_name::text AS genre, 1.0::float8 AS weight
FROM tracks t
JOIN albums al ON al.id = t.album_id
CROSS JOIN LATERAL unnest(al.genres) AS genre_name
WHERE t.id = ANY($1::bigint[])
  AND genre_name <> ''
UNION ALL
SELECT tf.track_id, (elem->>'name')::text AS genre, COALESCE((elem->>'score')::float8, 0) AS weight
FROM track_facets tf
CROSS JOIN LATERAL jsonb_array_elements(COALESCE(tf.top_genres, '[]'::jsonb)) AS elem
WHERE tf.track_id = ANY($1::bigint[])
  AND COALESCE((elem->>'name')::text, '') <> '';

-- name: ArtistGenreWeights :many
-- Whole-discography genre frequency for an artist-kind radio seed: how many
-- of the artist's albums carry each genre. Deliberately album-frequency, not
-- track-count, so a 20-track album doesn't drown out a genre that only
-- appears on one other release. Feeds the same seed genre profile as
-- TrackGenreWeights (normalized + merged in Go, see radioSeedGenreProfile).
SELECT genre_name::text AS genre, count(*)::float8 AS weight
FROM albums al
CROSS JOIN LATERAL unnest(al.genres) AS genre_name
WHERE al.artist_id = ANY($1::bigint[])
  AND genre_name <> ''
GROUP BY genre_name;
