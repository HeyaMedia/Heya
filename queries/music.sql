-- name: CreateArtist :one
INSERT INTO artists (media_item_id, musicbrainz_id, sort_name, biography)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetArtistByMediaItemID :one
SELECT * FROM artists WHERE media_item_id = $1;

-- name: GetArtistByMusicBrainzID :one
SELECT * FROM artists WHERE musicbrainz_id = $1;

-- name: UpdateArtist :one
UPDATE artists
SET musicbrainz_id = $2, sort_name = $3, biography = $4
WHERE id = $1
RETURNING *;

-- name: CreateAlbum :one
INSERT INTO albums (artist_id, title, year, musicbrainz_id, album_type, genres, cover_path, release_date,
    label, country, barcode, total_tracks, total_discs, tags)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: ListAlbumsByArtist :many
SELECT * FROM albums WHERE artist_id = $1 ORDER BY year ASC, title ASC;

-- name: GetAlbumByID :one
SELECT * FROM albums WHERE id = $1;

-- name: GetAlbumByMusicBrainzID :one
SELECT * FROM albums WHERE musicbrainz_id = $1;

-- name: UpdateAlbum :one
UPDATE albums
SET title = $2, year = $3, musicbrainz_id = $4, album_type = $5,
    genres = $6, cover_path = $7, release_date = $8,
    label = $9, country = $10, barcode = $11, total_tracks = $12, total_discs = $13, tags = $14
WHERE id = $1
RETURNING *;

-- name: CreateTrack :one
INSERT INTO tracks (album_id, disc_number, track_number, title, duration_ms, file_path)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListTracksByAlbum :many
SELECT * FROM tracks WHERE album_id = $1 ORDER BY disc_number ASC, track_number ASC;

-- name: GetTrackByID :one
SELECT * FROM tracks WHERE id = $1;
