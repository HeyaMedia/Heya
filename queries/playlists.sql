-- name: CreateUserPlaylist :one
INSERT INTO user_playlists (user_id, name, description, cover_path, slug)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUserPlaylist :one
SELECT * FROM user_playlists WHERE id = $1 AND user_id = $2;

-- name: GetUserPlaylistBySlug :one
SELECT * FROM user_playlists WHERE slug = $1 AND user_id = $2;

-- name: UserPlaylistSlugExists :one
-- Collision check for slug.GenerateUnique — scoped per-user (playlist slugs
-- are only unique within one user's library, not globally) and excludes the
-- row being renamed so re-saving with an unchanged name doesn't self-collide.
SELECT EXISTS(SELECT 1 FROM user_playlists WHERE user_id = $1 AND slug = $2 AND id != $3) AS exists;

-- name: ListUserPlaylists :many
-- Used by the sidebar — small payload, includes a synthesized cover (first
-- track's album cover) and track count so the UI doesn't need follow-up calls.
SELECT p.*,
       (SELECT count(*) FROM user_playlist_tracks WHERE playlist_id = p.id) AS track_count,
       -- First track's addressing pair — the FE builds the canonical
       -- /api album-cover URL from these (image URLs are unconditional:
       -- filtering on al.cover_path here re-created the documented
       -- empty-column-means-no-image bug, so no cover filter).
       COALESCE(
           (SELECT al.slug
            FROM user_playlist_tracks upt
            JOIN tracks t ON t.id = upt.track_id
            JOIN albums al ON al.id = t.album_id
            WHERE upt.playlist_id = p.id
            ORDER BY (al.cover_path != '') DESC, upt.position ASC LIMIT 1),
           ''
       ) AS auto_album_slug,
       COALESCE(
           (SELECT mi.slug
            FROM user_playlist_tracks upt
            JOIN tracks t ON t.id = upt.track_id
            JOIN albums al ON al.id = t.album_id
            JOIN artists a ON a.id = al.artist_id
            JOIN media_item_cards mi ON mi.id = a.media_item_id
            WHERE upt.playlist_id = p.id
            ORDER BY (al.cover_path != '') DESC, upt.position ASC LIMIT 1),
           ''
       ) AS auto_artist_slug,
       (p.cover_path != '') AS has_cover,
       -- Active sync links (row presence = syncing); deleting the playlist
       -- cascades these away, which is what stops the sync.
       COALESCE((SELECT array_agg(DISTINCT s.service ORDER BY s.service)
                 FROM user_playlist_syncs s WHERE s.playlist_id = p.id), '{}') AS sync_services
FROM user_playlists p
WHERE p.user_id = $1
ORDER BY p.created_at DESC;

-- name: UpdateUserPlaylist :exec
UPDATE user_playlists
SET name = $3, description = $4, cover_path = $5, slug = $6, tags = $7, updated_at = now()
WHERE id = $1 AND user_id = $2;

-- name: UpdateUserPlaylistCoverPath :exec
-- Cover-only update — used by SetUserPlaylistCover / ClearUserPlaylistCover
-- so an upload/clear doesn't touch name/description/slug.
UPDATE user_playlists
SET cover_path = $3, updated_at = now()
WHERE id = $1 AND user_id = $2;

-- name: GetUserPlaylistCoverPathByID :one
-- Owner-agnostic cover_path lookup for the public image-byte GET route
-- (registered without auth in binary_huma.go, same as every other image
-- endpoint — browsers can't attach a bearer token to an <img> tag). The
-- cover bytes aren't sensitive on their own; playlist metadata stays
-- ownership-gated via GetUserPlaylist / GetUserPlaylistBySlug.
SELECT cover_path FROM user_playlists WHERE id = $1;

-- name: SetPlaylistPagePin :exec
-- Pins deliberately do NOT bump updated_at — pinning isn't a content change
-- and must not reshuffle "recently updated" sorts.
UPDATE user_playlists SET pinned = $3 WHERE id = $1 AND user_id = $2;

-- name: SetPlaylistSidebarPin :exec
UPDATE user_playlists SET sidebar_pinned = $3 WHERE id = $1 AND user_id = $2;

-- name: SetPlaylistSidebarPosition :exec
UPDATE user_playlists SET sidebar_position = $3 WHERE id = $1 AND user_id = $2;

-- name: DeleteUserPlaylist :exec
DELETE FROM user_playlists WHERE id = $1 AND user_id = $2;

-- name: AddTrackToPlaylist :exec
INSERT INTO user_playlist_tracks (playlist_id, track_id, position)
VALUES (
    $1, $2,
    COALESCE((SELECT max(position) + 1 FROM user_playlist_tracks WHERE playlist_id = $1), 1)
)
ON CONFLICT (playlist_id, track_id) DO NOTHING;

-- name: RemoveTrackFromPlaylist :exec
DELETE FROM user_playlist_tracks WHERE playlist_id = $1 AND track_id = $2;

-- name: ListPlaylistTracks :many
-- Tracks in the playlist with full album + artist join so the playlist page
-- renders in a single round trip. The quality fields come from the
-- track's best file (highest quality_score, smallest id as tiebreak —
-- mirrors GetPrimaryTrackFile/ListAlbumTrackFilesForLoudness), resolved via
-- a plain LEFT JOIN whose ON-clause picks the file id with a correlated
-- subquery. NOT `LEFT JOIN LATERAL (...) bf ON true`: sqlc's static analyzer
-- mistypes columns sourced from a LATERAL derived table as non-nullable
-- (verified against sqlc v1.31.1), which then panics scanning NULL for a
-- track with zero files. Joining the real table directly — with the
-- correlation moved into the ON-clause subquery instead of the FROM list —
-- makes sqlc infer the correct nullable pgtype for bf.* columns.
--
-- The composer/artists_display strings are extracted in SQL (not decoded in
-- Go) because jsonb columns embedded raw in a row struct marshal as base64
-- over the wire. jsonb_typeof guards keep jsonb 'null' scalars from blowing
-- up jsonb_array_elements. user_id feeds the caller's rating + play stats;
-- play_events probes ride play_events_user_track_played_idx.
SELECT t.id            AS track_id,
       t.title         AS track_title,
       t.duration      AS duration,
       t.disc_number   AS disc_number,
       t.track_number  AS track_number,
       t.explicit      AS explicit,
       al.id           AS album_id,
       al.title        AS album_title,
       al.cover_path   AS album_cover_path,
       al.year         AS album_year,
       al.slug         AS album_slug,
       al.genres       AS album_genres,
       al.label        AS album_label,
       al.release_date AS album_release_date,
       a.id            AS artist_id,
       a.name          AS artist_name,
       mi.slug         AS artist_slug,
       upt.position    AS position,
       upt.added_at    AS added_at,
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
       utr.rating         AS rating,
       COALESCE((SELECT string_agg((ac.value ->> 'name') || COALESCE(ac.value ->> 'join_phrase', ''), '' ORDER BY ac.ord)
          FROM jsonb_array_elements(CASE WHEN jsonb_typeof(t.artist_credits) = 'array' THEN t.artist_credits ELSE '[]'::jsonb END)
               WITH ORDINALITY AS ac(value, ord)), '')::text AS artists_display,
       COALESCE((SELECT string_agg(DISTINCT cr.value ->> 'artist_name', ', ')
          FROM jsonb_array_elements(CASE WHEN jsonb_typeof(t.credits) = 'array' THEN t.credits ELSE '[]'::jsonb END) AS cr(value)
         WHERE cr.value ->> 'role' = 'composer'), '')::text AS composer,
       (SELECT count(*) FROM play_events pe WHERE pe.user_id = sqlc.arg(user_id) AND pe.track_id = t.id) AS play_count,
       (SELECT max(pe.played_at) FROM play_events pe WHERE pe.user_id = sqlc.arg(user_id) AND pe.track_id = t.id)::timestamptz AS last_played_at
FROM user_playlist_tracks upt
JOIN tracks      t  ON t.id  = upt.track_id
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
LEFT JOIN user_track_ratings utr ON utr.user_id = sqlc.arg(user_id) AND utr.track_id = t.id
WHERE upt.playlist_id = sqlc.arg(playlist_id)
ORDER BY upt.position ASC;

-- name: ReorderPlaylistTrack :exec
UPDATE user_playlist_tracks SET position = $3
WHERE playlist_id = $1 AND track_id = $2;
