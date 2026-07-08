-- name: CreateUserPlaylist :one
INSERT INTO user_playlists (user_id, name, description, cover_path)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserPlaylist :one
SELECT * FROM user_playlists WHERE id = $1 AND user_id = $2;

-- name: ListUserPlaylists :many
-- Used by the sidebar — small payload, includes a synthesized cover (first
-- track's album cover) and track count so the UI doesn't need follow-up calls.
SELECT p.*,
       (SELECT count(*) FROM user_playlist_tracks WHERE playlist_id = p.id) AS track_count,
       COALESCE(
           (SELECT al.cover_path
            FROM user_playlist_tracks upt
            JOIN tracks t ON t.id = upt.track_id
            JOIN albums al ON al.id = t.album_id
            WHERE upt.playlist_id = p.id AND al.cover_path != ''
            ORDER BY upt.position ASC LIMIT 1),
           ''
       ) AS auto_cover
FROM user_playlists p
WHERE p.user_id = $1
ORDER BY p.created_at DESC;

-- name: UpdateUserPlaylist :exec
UPDATE user_playlists
SET name = $3, description = $4, cover_path = $5, updated_at = now()
WHERE id = $1 AND user_id = $2;

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
-- renders in a single round trip. The four quality fields come from the
-- track's best file (highest quality_score, smallest id as tiebreak —
-- mirrors GetPrimaryTrackFile/ListAlbumTrackFilesForLoudness), resolved via
-- a plain LEFT JOIN whose ON-clause picks the file id with a correlated
-- subquery. NOT `LEFT JOIN LATERAL (...) bf ON true`: sqlc's static analyzer
-- mistypes columns sourced from a LATERAL derived table as non-nullable
-- (verified against sqlc v1.31.1), which then panics scanning NULL for a
-- track with zero files. Joining the real table directly — with the
-- correlation moved into the ON-clause subquery instead of the FROM list —
-- makes sqlc infer the correct nullable pgtype for bf.* columns.
SELECT t.id            AS track_id,
       t.title         AS track_title,
       t.duration      AS duration,
       t.disc_number   AS disc_number,
       t.track_number  AS track_number,
       al.id           AS album_id,
       al.title        AS album_title,
       al.cover_path   AS album_cover_path,
       al.year         AS album_year,
       al.slug         AS album_slug,
       a.id            AS artist_id,
       a.name          AS artist_name,
       mi.slug         AS artist_slug,
       upt.position    AS position,
       upt.added_at    AS added_at,
       EXISTS (SELECT 1 FROM track_files tf JOIN library_files lf ON lf.id = tf.library_file_id WHERE tf.track_id = t.id AND lf.deleted_at IS NULL) AS available,
       bf.format         AS format,
       bf.bitrate_kbps   AS bitrate_kbps,
       bf.sample_rate_hz AS sample_rate_hz,
       bf.bit_depth      AS bit_depth
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
WHERE upt.playlist_id = $1
ORDER BY upt.position ASC;

-- name: ReorderPlaylistTrack :exec
UPDATE user_playlist_tracks SET position = $3
WHERE playlist_id = $1 AND track_id = $2;
