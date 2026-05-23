-- name: CreateArtist :one
INSERT INTO artists (media_item_id, musicbrainz_id, name, sort_name, disambiguation, biography)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetArtistByMediaItemID :one
SELECT * FROM artists WHERE media_item_id = $1;

-- name: GetArtistByID :one
SELECT * FROM artists WHERE id = $1;

-- name: ListArtistsByLibrary :many
SELECT a.* FROM artists a
JOIN media_items mi ON mi.id = a.media_item_id
WHERE mi.library_id = $1
ORDER BY a.name;

-- name: ListStaleArtistsByLibrary :many
-- Artists in the library whose enrichment is older than $2 (or never enriched).
SELECT a.* FROM artists a
JOIN media_items mi ON mi.id = a.media_item_id
WHERE mi.library_id = $1
  AND (a.enriched_at IS NULL OR a.enriched_at < $2)
ORDER BY a.name;

-- name: MarkArtistEnriched :exec
UPDATE artists SET enriched_at = now() WHERE id = $1;

-- name: UpdateArtistEnrichedFields :exec
-- Used by RefreshMusicArtistWorker to write the canonical metadata after a
-- successful heya.media lookup. Only overwrites fields when the new value is
-- non-empty, so NFO data isn't clobbered by a sparse upstream response.
UPDATE artists
   SET musicbrainz_id  = CASE WHEN $2::text  != '' THEN $2 ELSE musicbrainz_id  END,
       name            = CASE WHEN $3::text  != '' THEN $3 ELSE name            END,
       sort_name       = CASE WHEN $4::text  != '' THEN $4 ELSE sort_name       END,
       disambiguation  = CASE WHEN $5::text  != '' THEN $5 ELSE disambiguation  END,
       biography       = CASE WHEN $6::text  != '' THEN $6 ELSE biography       END,
       enriched_at     = now()
 WHERE id = $1;

-- name: UpdateAlbumEnrichedFields :exec
-- Same pattern: only overwrite when the new value is non-empty.
UPDATE albums
   SET musicbrainz_id = CASE WHEN $2::text  != '' THEN $2 ELSE musicbrainz_id END,
       title          = CASE WHEN $3::text  != '' THEN $3 ELSE title          END,
       year           = CASE WHEN $4::text  != '' THEN $4 ELSE year           END,
       album_type     = CASE WHEN $5::text  != '' THEN $5 ELSE album_type     END,
       label          = CASE WHEN $6::text  != '' THEN $6 ELSE label          END,
       country        = CASE WHEN $7::text  != '' THEN $7 ELSE country        END,
       barcode        = CASE WHEN $8::text  != '' THEN $8 ELSE barcode        END,
       release_date   = COALESCE($9::date, release_date),
       cover_path     = CASE WHEN $10::text != '' THEN $10 ELSE cover_path    END
 WHERE id = $1;

-- name: UpdateTrackFromEnrichment :exec
-- Overwrites track title and duration with enriched data (heya.media wins
-- over filename when present). NFO/path data is the seed; this is the upgrade.
UPDATE tracks
   SET title    = CASE WHEN $2::text != '' THEN $2 ELSE title    END,
       duration = CASE WHEN $3::int  > 0   THEN $3 ELSE duration END
 WHERE id = $1;

-- name: UpdateMediaItemExternalIds :exec
UPDATE media_items SET external_ids = $2 WHERE id = $1;

-- name: GetArtistByMusicBrainzID :one
SELECT * FROM artists WHERE musicbrainz_id = $1 AND musicbrainz_id != '';

-- name: GetArtistByNameAndDisambiguation :one
SELECT * FROM artists
WHERE lower(name) = lower($1) AND lower(disambiguation) = lower($2)
LIMIT 1;

-- name: UpdateArtist :one
UPDATE artists
SET musicbrainz_id = $2, name = $3, sort_name = $4, disambiguation = $5, biography = $6
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
SELECT * FROM albums WHERE musicbrainz_id = $1 AND musicbrainz_id != '';

-- name: GetAlbumByArtistTitleYear :one
SELECT * FROM albums
WHERE artist_id = $1 AND lower(title) = lower($2) AND year = $3
LIMIT 1;

-- name: UpdateAlbum :one
UPDATE albums
SET title = $2, year = $3, musicbrainz_id = $4, album_type = $5,
    genres = $6, cover_path = $7, release_date = $8,
    label = $9, country = $10, barcode = $11, total_tracks = $12, total_discs = $13, tags = $14
WHERE id = $1
RETURNING *;

-- name: CreateTrack :one
INSERT INTO tracks (album_id, disc_number, track_number, title, duration, file_path, lyrics_path, library_file_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetOrCreateTrack :one
-- Idempotent track creation: on conflict, return the existing row unchanged.
-- Per-file data (file_path / library_file_id / lyrics_path) lives in
-- track_files now and is recomputed when the primary file changes.
INSERT INTO tracks (album_id, disc_number, track_number, title, duration)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (album_id, disc_number, track_number) DO UPDATE
    SET title = CASE WHEN tracks.title = '' THEN EXCLUDED.title ELSE tracks.title END,
        duration = CASE WHEN tracks.duration = 0 THEN EXCLUDED.duration ELSE tracks.duration END
RETURNING *;

-- name: UpdateTrackTitleAndDuration :one
-- Used by the enrichment pass to overwrite path-derived data with NFO /
-- heya.media canonical values once they're known.
UPDATE tracks SET title = $2, duration = $3 WHERE id = $1 RETURNING *;

-- name: UpdateTrackPrimary :exec
-- Denormalize the chosen primary file onto the track row for fast playback.
UPDATE tracks
   SET file_path = $2, library_file_id = $3, lyrics_path = $4
 WHERE id = $1;

-- name: UpdateTrackLyricsPath :exec
UPDATE tracks SET lyrics_path = $2 WHERE id = $1;

-- name: ListTracksByAlbum :many
SELECT * FROM tracks WHERE album_id = $1 ORDER BY disc_number ASC, track_number ASC;

-- name: GetAlbumReleaseDir :one
-- Returns the on-disk release directory for an album (parent dir of any of
-- its tracks). Used by the music NFO writer to know where to drop album.nfo.
-- Empty string if the album has no files (e.g. tracks all soft-deleted).
SELECT COALESCE(MAX(file_path), '') AS file_path FROM tracks WHERE album_id = $1;

-- name: GetTrackByID :one
SELECT * FROM tracks WHERE id = $1;

-- name: GetTrackByLibraryFileID :one
SELECT * FROM tracks WHERE library_file_id = $1;

-- name: UpsertTrackFile :one
-- One row per physical audio file. UNIQUE(library_file_id) means a file can
-- only back one track at a time; if the matcher re-routes a file to a
-- different track (e.g. after a re-scan with corrected metadata), the upsert
-- moves it.
INSERT INTO track_files (track_id, library_file_id, format, quality_score, lyrics_path, size_bytes)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (library_file_id) DO UPDATE
    SET track_id = EXCLUDED.track_id,
        format = EXCLUDED.format,
        quality_score = EXCLUDED.quality_score,
        lyrics_path = EXCLUDED.lyrics_path,
        size_bytes = EXCLUDED.size_bytes
RETURNING *;

-- name: ListTrackFilesByTrack :many
-- Ordered by quality (best first) so callers can pick [0] as the primary.
SELECT tf.*
FROM track_files tf
JOIN library_files lf ON lf.id = tf.library_file_id
WHERE tf.track_id = $1 AND lf.deleted_at IS NULL
ORDER BY tf.quality_score DESC, tf.id ASC;

-- name: GetTrackFileByLibraryFileID :one
SELECT * FROM track_files WHERE library_file_id = $1;

-- name: UpdateTrackFileProbeData :exec
-- Called by FFProbeWorker after probing an audio file. Updates the per-file
-- physical properties and the refined quality_score that incorporates real
-- bitrate / sample rate / bit depth.
UPDATE track_files
   SET bitrate_kbps   = $2,
       sample_rate_hz = $3,
       bit_depth      = $4,
       channels       = $5,
       duration       = $6,
       quality_score  = $7
 WHERE id = $1;
