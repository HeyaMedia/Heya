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
-- Artists in the library whose discography enrichment is older than $2 (or never enriched).
SELECT a.* FROM artists a
JOIN media_items mi ON mi.id = a.media_item_id
WHERE mi.library_id = $1
  AND (a.discography_enriched_at IS NULL OR a.discography_enriched_at < $2)
ORDER BY a.name;

-- name: MarkArtistDiscographyEnriched :exec
UPDATE artists SET discography_enriched_at = now() WHERE id = $1;

-- name: MarkArtistCoverArtEnriched :exec
UPDATE artists SET cover_art_enriched_at = now() WHERE id = $1;

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
       discography_enriched_at = now()
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

-- name: GetArtistByNameAndDisambiguationExcludingID :one
-- Used by the merge-detection path in RefreshMusicArtist when no MBID
-- helped resolve a canonical sibling — falls back to "is there an
-- existing row whose (name, disambig) already matches what we're about
-- to write?" so the unique-constraint collision turns into a merge.
SELECT * FROM artists
WHERE lower(name) = lower($1)
  AND lower(disambiguation) = lower($2)
  AND id != sqlc.arg(exclude_id)
LIMIT 1;

-- name: UpdateArtist :one
UPDATE artists
SET musicbrainz_id = $2, name = $3, sort_name = $4, disambiguation = $5, biography = $6
WHERE id = $1
RETURNING *;

-- name: CreateAlbum :one
INSERT INTO albums (artist_id, title, slug, year, musicbrainz_id, album_type, genres, cover_path, release_date,
    label, country, barcode, total_tracks, total_discs, tags)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: SetAlbumSlug :exec
UPDATE albums SET slug = $2 WHERE id = $1;

-- name: AlbumSlugExists :one
SELECT EXISTS (
    SELECT 1 FROM albums WHERE artist_id = $1 AND slug = $2 AND id <> $3
);

-- name: GetAlbumByArtistAndSlug :one
SELECT al.*
FROM albums al
JOIN artists a ON a.id = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
WHERE mi.slug = $1 AND al.slug = $2
LIMIT 1;

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
SET title = $2, slug = $3, year = $4, musicbrainz_id = $5, album_type = $6,
    genres = $7, cover_path = $8, release_date = $9,
    label = $10, country = $11, barcode = $12, total_tracks = $13, total_discs = $14, tags = $15
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

-- name: UpdateAlbumCoverPath :exec
-- Writes a local cover-art path detected from the album folder
-- (cover.jpg/folder.jpg/front.jpg). Local detection always wins over the
-- remote URL the matcher captured from heya.media, so this is an
-- unconditional overwrite (callers gate the call themselves).
UPDATE albums SET cover_path = $2 WHERE id = $1;

-- name: UpdateArtistExtendedMetadata :exec
-- Writes the post-00019 fields on artists (everything beyond name / bio /
-- MBID, which UpdateArtistEnrichedFields handles separately). All fields
-- are written unconditionally — heya.media is the source of truth for
-- this slice of the artist row, and a refresh that retrieves fresh
-- listeners / playcount / popularity should replace the old values.
UPDATE artists SET
    listeners        = $2,
    playcount        = $3,
    popularity       = $4,
    annotation       = $5,
    urls             = $6,
    wikipedia_links  = $7,
    profiles         = $8,
    aliases          = $9,
    groups           = $10,
    members          = $11,
    artist_type      = $12,
    begin_date       = $13,
    begin_year       = $14,
    end_date         = $15,
    ended            = $16,
    deathday         = $17,
    birthplace       = $18,
    tags             = $19
WHERE id = $1;

-- name: ReplaceArtistTopTracks :exec
-- Top tracks are a small ranked list per artist. We replace-on-refresh
-- rather than upsert because the rank ordering is what we actually
-- care about — partial updates would leave stale rows interleaved with
-- new ones. Caller is expected to issue this DELETE followed by N inserts
-- inside the same transaction.
DELETE FROM artist_top_tracks WHERE artist_id = $1;

-- name: CreateArtistTopTrack :exec
INSERT INTO artist_top_tracks (artist_id, rank, title, mbid, playcount, listeners, url)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ReplaceArtistSimilarArtists :exec
-- Same replace-on-refresh story as top_tracks. The match scores and
-- ordering shift slightly on every Last.fm/ListenBrainz refresh, so
-- treating the list as a transactional swap keeps the page snappy.
DELETE FROM artist_similar_artists WHERE artist_id = $1;

-- name: CreateArtistSimilarArtist :exec
INSERT INTO artist_similar_artists (
    artist_id, rank, name, mbid, match_score, url, local_artist_id
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: UpdateAlbumExtendedMetadata :exec
-- The post-00019 album columns. Mirrors UpdateAlbumEnrichedFields' style
-- (CASE WHEN $N != '' THEN $N ELSE col END) so a sparse heya.media
-- payload doesn't clobber NFO-derived values that happened to be richer.
UPDATE albums SET
    catalog_no       = CASE WHEN $2::text   != '' THEN $2 ELSE catalog_no END,
    original_title   = CASE WHEN $3::text   != '' THEN $3 ELSE original_title END,
    language         = CASE WHEN $4::text   != '' THEN $4 ELSE language END,
    explicit         = $5,
    duration_seconds = CASE WHEN $6::int    != 0  THEN $6 ELSE duration_seconds END,
    rating           = CASE WHEN $7::numeric != 0 THEN $7 ELSE rating END,
    popularity       = $8,
    listeners        = $9,
    playcount        = $10,
    secondary_types  = $11,
    styles           = $12,
    isrcs            = $13,
    external_ids     = $14,
    artist_credits   = $15
WHERE id = $1;

-- name: UpdateTrackExtendedMetadata :exec
-- The post-00019 track columns. external_ids / isrc / recording_mbid are
-- the keys that unlock LRCLIB lyrics lookups + cross-service deep links.
-- preview_url is the iTunes/Deezer 30-second sample for hover previews.
UPDATE tracks SET
    external_ids   = $2,
    isrc           = CASE WHEN $3::text != '' THEN $3 ELSE isrc END,
    recording_mbid = CASE WHEN $4::text != '' THEN $4 ELSE recording_mbid END,
    preview_url    = CASE WHEN $5::text != '' THEN $5 ELSE preview_url END,
    explicit       = $6,
    artist_credits = $7
WHERE id = $1;

-- name: ListTracksByAlbum :many
SELECT * FROM tracks WHERE album_id = $1 ORDER BY disc_number ASC, track_number ASC;

-- name: GetAlbumReleaseDir :one
-- Returns the on-disk release directory for an album (parent dir of any of
-- its tracks). Used by the music NFO writer to know where to drop album.nfo.
-- Empty string if the album has no files (e.g. tracks all soft-deleted).
SELECT COALESCE(MAX(file_path), '') AS file_path FROM tracks WHERE album_id = $1;

-- name: GetTrackByID :one
SELECT * FROM tracks WHERE id = $1;

-- name: GetTrackDetailByID :one
-- One-shot track detail: track row + album + artist context. Pair with
-- ListTrackFilesByTrack on the service side to attach per-file formats.
SELECT t.id,
       t.album_id,
       t.disc_number,
       t.track_number,
       t.title,
       t.duration,
       t.lyrics_path,
       al.title          AS album_title,
       al.slug           AS album_slug,
       al.year           AS album_year,
       al.cover_path     AS album_cover_path,
       a.id              AS artist_id,
       a.name            AS artist_name,
       mi.slug           AS artist_slug
FROM tracks t
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
WHERE t.id = $1
LIMIT 1;

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

-- name: ListMusicArtists :many
-- Merged listing across every music library, with album + track counts so
-- the Artists grid can show density at a glance. Sorted by sort_name when
-- present, falling back to name.
SELECT a.*,
       mi.slug         AS slug,
       mi.poster_path  AS poster_path,
       (SELECT count(*) FROM albums  al WHERE al.artist_id = a.id)                              AS album_count,
       (SELECT count(*) FROM tracks  t  JOIN albums al ON al.id = t.album_id WHERE al.artist_id = a.id) AS track_count
FROM artists a
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
ORDER BY lower(coalesce(NULLIF(a.sort_name, ''), a.name)) ASC
LIMIT $1 OFFSET $2;

-- name: CountMusicArtists :one
SELECT count(*) FROM artists a
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music';

-- name: GetMusicArtistBySlug :one
-- Direct artist lookup by media-item slug. Same row shape as ListMusicArtists
-- so the FE can render headers from either feed without branching.
SELECT a.*,
       mi.slug         AS slug,
       mi.poster_path  AS poster_path,
       (SELECT count(*) FROM albums  al WHERE al.artist_id = a.id)                              AS album_count,
       (SELECT count(*) FROM tracks  t  JOIN albums al ON al.id = t.album_id WHERE al.artist_id = a.id) AS track_count
FROM artists a
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE mi.slug = $1 AND l.media_type = 'music'
LIMIT 1;

-- name: ListAlbumsByArtistSlug :many
-- Paginated album listing for one artist. Same row shape as ListMusicAlbums
-- so the FE can reuse the album-row component without branching.
SELECT al.*,
       a.name           AS artist_name,
       mi.slug          AS artist_slug,
       (SELECT count(*) FROM tracks t WHERE t.album_id = al.id) AS track_count
FROM albums al
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
WHERE mi.slug = $1
ORDER BY al.year DESC NULLS LAST, lower(al.title) ASC
LIMIT $2 OFFSET $3;

-- name: CountAlbumsByArtistSlug :one
SELECT count(*) FROM albums al
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
WHERE mi.slug = $1;

-- name: ListTracksByArtistSlug :many
-- Paginated flat-track listing for one artist. Same row shape as
-- ListMusicTracks. Newest album first, then disc/track order within the album.
SELECT t.id              AS track_id,
       t.title           AS track_title,
       t.duration        AS duration,
       t.disc_number     AS disc_number,
       t.track_number    AS track_number,
       al.id             AS album_id,
       al.title          AS album_title,
       al.cover_path     AS album_cover_path,
       al.year           AS album_year,
       a.id              AS artist_id,
       a.name            AS artist_name,
       mi.slug           AS artist_slug
FROM tracks t
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
WHERE mi.slug = $1
ORDER BY al.year DESC NULLS LAST, lower(al.title) ASC,
         t.disc_number ASC, t.track_number ASC
LIMIT $2 OFFSET $3;

-- name: CountTracksByArtistSlug :one
SELECT count(*) FROM tracks t
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
WHERE mi.slug = $1;

-- name: ListMusicAlbums :many
-- Merged listing of every album across every music library. Joins the artist
-- so the Albums grid can render "Title — Artist" without a second round-trip.
-- album_slug is unique within the artist; artist_slug routes to the artist
-- detail page.
SELECT al.*,
       a.name           AS artist_name,
       mi.slug          AS artist_slug,
       (SELECT count(*) FROM tracks t WHERE t.album_id = al.id) AS track_count
FROM albums al
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
ORDER BY lower(a.name) ASC, al.year ASC, lower(al.title) ASC
LIMIT $1 OFFSET $2;

-- name: CountMusicAlbums :one
SELECT count(*) FROM albums al
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music';

-- name: ListMusicTracks :many
-- Flat listing for the Songs tab. Carries everything the row needs:
-- title, duration, album title, artist name, slugs for navigation, and the
-- album cover for the thumbnail. album_slug + artist_slug together address
-- the album cover endpoint without an ID lookup.
SELECT t.id              AS track_id,
       t.title           AS track_title,
       t.duration        AS duration,
       t.disc_number     AS disc_number,
       t.track_number    AS track_number,
       al.id             AS album_id,
       al.title          AS album_title,
       al.slug           AS album_slug,
       al.cover_path     AS album_cover_path,
       al.year           AS album_year,
       a.id              AS artist_id,
       a.name            AS artist_name,
       mi.slug           AS artist_slug
FROM tracks t
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
ORDER BY lower(a.name) ASC, al.year ASC, lower(al.title) ASC,
         t.disc_number ASC, t.track_number ASC
LIMIT $1 OFFSET $2;

-- name: CountMusicTracks :one
SELECT count(*) FROM tracks t
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music';

-- name: GetPrimaryTrackFile :one
-- The single best file for a track: highest quality_score, smallest id as
-- tiebreak. NULL when the track has no playable file (shouldn't happen for
-- matched tracks but guard anyway).
SELECT tf.*
FROM track_files tf
JOIN library_files lf ON lf.id = tf.library_file_id
WHERE tf.track_id = $1 AND lf.deleted_at IS NULL
ORDER BY tf.quality_score DESC, tf.id ASC
LIMIT 1;

-- name: GetTrackFileByID :one
SELECT * FROM track_files WHERE id = $1;

-- name: ListRecentlyAddedAlbums :many
-- Newest albums across every music library, paginated. Newest = highest
-- album id since albums get IDENTITY-generated IDs in insert order.
SELECT al.*,
       a.name           AS artist_name,
       mi.slug          AS artist_slug,
       (SELECT count(*) FROM tracks t WHERE t.album_id = al.id) AS track_count
FROM albums al
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
ORDER BY al.id DESC
LIMIT $1;

-- name: ListRecentlyAddedArtists :many
-- Newest artists across every music library — uses discography_enriched_at
-- when present (signals the artist actually exists with metadata) else falls
-- back to the artist id. Nulls-last keeps fresh additions out of pole position
-- before their enrichment completes.
SELECT a.*,
       mi.slug         AS slug,
       mi.poster_path  AS poster_path,
       (SELECT count(*) FROM albums al WHERE al.artist_id = a.id) AS album_count,
       (SELECT count(*) FROM tracks t JOIN albums al ON al.id = t.album_id WHERE al.artist_id = a.id) AS track_count
FROM artists a
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
ORDER BY a.discography_enriched_at DESC NULLS LAST, a.id DESC
LIMIT $1;

-- name: UpdateTrackFileLoudness :exec
-- Called by ScanTrackLoudnessWorker once ebur128 finishes.
UPDATE track_files
   SET integrated_lufs      = $2,
       true_peak_db         = $3,
       loudness_range_db    = $4,
       sample_peak_db       = $5,
       loudness_analyzed_at = now()
 WHERE id = $1;

-- name: ListTrackFilesPendingLoudness :many
-- Files in music libraries that don't yet have loudness data and aren't
-- soft-deleted. The track-loudness worker pulls from here as a backstop in
-- case the FFProbeWorker hand-off missed something.
SELECT tf.id, tf.library_file_id, tf.track_id, lf.path
FROM track_files tf
JOIN library_files lf ON lf.id = tf.library_file_id
JOIN tracks t  ON t.id  = tf.track_id
JOIN albums al ON al.id = t.album_id
JOIN artists a ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
  AND lf.deleted_at IS NULL
  AND tf.integrated_lufs IS NULL
ORDER BY tf.id
LIMIT $1;

-- name: UpdateAlbumLoudness :exec
UPDATE albums
   SET integrated_lufs      = $2,
       true_peak_db         = $3,
       loudness_range_db    = $4,
       loudness_analyzed_at = now()
 WHERE id = $1;

-- name: ListAlbumsPendingLoudness :many
-- Albums whose track loudness is fully populated but their own album-level
-- loudness has not yet been measured. The album worker picks these up.
SELECT al.id, al.title
FROM albums al
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_items mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
  AND al.loudness_analyzed_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM tracks t
    JOIN track_files tf ON tf.track_id = t.id
    JOIN library_files lf ON lf.id = tf.library_file_id
    WHERE t.album_id = al.id
      AND lf.deleted_at IS NULL
      AND tf.integrated_lufs IS NULL
  )
  AND EXISTS (
    SELECT 1 FROM tracks t WHERE t.album_id = al.id
  )
ORDER BY al.id
LIMIT $1;

-- name: ListAlbumTrackFilesForLoudness :many
-- Returns one file path per track in disc/track order. Album worker concats
-- these and runs ebur128 once. Picks the primary (highest quality) file per
-- track for the album measurement so a present-day MP3 fallback doesn't
-- skew an album whose other tracks are FLAC.
SELECT t.id AS track_id,
       t.disc_number,
       t.track_number,
       lf.path
FROM tracks t
JOIN LATERAL (
    SELECT tf.id, tf.library_file_id
    FROM track_files tf
    JOIN library_files lf2 ON lf2.id = tf.library_file_id
    WHERE tf.track_id = t.id AND lf2.deleted_at IS NULL
    ORDER BY tf.quality_score DESC, tf.id ASC
    LIMIT 1
) primary_file ON true
JOIN library_files lf ON lf.id = primary_file.library_file_id
WHERE t.album_id = $1
ORDER BY t.disc_number, t.track_number;

-- name: GetArtistByMusicBrainzIDExcludingID :one
-- Used by RefreshMusicArtist to detect "we resolved the same MBID as an
-- existing sibling row" so the matcher can merge instead of letting
-- UpdateArtistEnrichedFields collide on uq_artists_name_disambig.
SELECT *
FROM artists
WHERE musicbrainz_id = sqlc.arg(mbid)
  AND id != sqlc.arg(exclude_id)
LIMIT 1;

-- name: ReparentAlbums :exec
-- Move every album from src_id over to dst_id. Tracks follow via
-- albums.artist_id (track_id is keyed on album_id, not artist_id).
UPDATE albums SET artist_id = sqlc.arg(dst_id) WHERE albums.artist_id = sqlc.arg(src_id);

-- name: ReparentSimilarLocalRefs :exec
-- Re-point any "this dupe is a similar artist of X" pointer at the
-- canonical row. The dupe's OWN similar-list rows are deleted by
-- DeleteArtistDerivedChildren below — those get recomputed.
UPDATE artist_similar_artists
SET local_artist_id = sqlc.arg(dst_id)
WHERE local_artist_id = sqlc.arg(src_id);

-- name: DeleteArtistCentroid :exec
DELETE FROM artist_centroids WHERE artist_id = sqlc.arg(src_id);

-- name: DeleteArtistTopTracks :exec
DELETE FROM artist_top_tracks WHERE artist_id = sqlc.arg(src_id);

-- name: DeleteArtistSimilarArtists :exec
DELETE FROM artist_similar_artists WHERE artist_id = sqlc.arg(src_id);

-- name: MergeUserArtistRatings :exec
-- Move user_artist_ratings from src_id to dst_id, keeping the higher
-- rating on collision (closer to what the user actually meant when
-- they rated the same artist twice under different rows).
INSERT INTO user_artist_ratings (user_id, artist_id, rating)
SELECT user_id, sqlc.arg(dst_id), rating
FROM user_artist_ratings
WHERE user_artist_ratings.artist_id = sqlc.arg(src_id)
ON CONFLICT (user_id, artist_id) DO UPDATE
SET rating     = GREATEST(user_artist_ratings.rating, EXCLUDED.rating),
    updated_at = now();

-- name: DeleteUserArtistRatingsByArtist :exec
DELETE FROM user_artist_ratings WHERE artist_id = sqlc.arg(src_id);

-- name: MergeArtistFavorites :exec
-- Move "loved artist" entries. Dupes within (user_id, entity_type,
-- entity_id) collapse to a no-op via the existing unique constraint.
INSERT INTO user_favorites (user_id, entity_type, entity_id)
SELECT user_id, 'artist', sqlc.arg(dst_id)
FROM user_favorites
WHERE entity_type = 'artist' AND user_favorites.entity_id = sqlc.arg(src_id)
ON CONFLICT (user_id, entity_type, entity_id) DO NOTHING;

-- name: DeleteArtistFavorites :exec
DELETE FROM user_favorites WHERE entity_type = 'artist' AND entity_id = sqlc.arg(src_id);

-- name: DeleteArtist :exec
DELETE FROM artists WHERE id = sqlc.arg(id);

-- name: ListArtistTopTracksRawByArtistID :many
-- Raw artist_top_tracks rows. The service layer joins these against the
-- artist's local tracks in Go so it can use kagome-backed romanization
-- (kana/kanji → romaji) for the title fallback. SQL alone can't do that
-- and pg_trgm matches CJK poorly, so the join lives in service code.
SELECT rank, title, mbid, playcount, listeners, url
FROM artist_top_tracks
WHERE artist_id = sqlc.arg(artist_id)
ORDER BY rank ASC
LIMIT sqlc.arg(track_limit);

-- name: ListTracksForArtistMatching :many
-- Minimal track + album projection used by ListArtistTopTracksBySlug to
-- build its in-memory match index. Duration falls back to the best
-- track_file duration when the canonical column is 0.
SELECT
    t.id        AS track_id,
    t.title     AS title,
    t.recording_mbid AS recording_mbid,
    COALESCE(
        NULLIF(t.duration, 0),
        (SELECT MAX(tf.duration) FROM track_files tf WHERE tf.track_id = t.id),
        0
    )::int      AS effective_duration,
    al.id       AS album_id,
    al.title    AS album_title,
    al.slug     AS album_slug,
    al.year     AS album_year,
    al.cover_path AS cover_path
FROM tracks t
JOIN albums al ON al.id = t.album_id
WHERE al.artist_id = sqlc.arg(artist_id);

-- name: ListArtistSimilarLocalArtistsByArtistID :many
-- Persisted Last.fm/ListenBrainz similar list, with local linkage already
-- folded in by the matcher write-side. Used to avoid the heya.media round
-- trip on every artist page render.
SELECT
    asa.rank,
    asa.name,
    asa.mbid,
    asa.match_score,
    asa.url,
    asa.local_artist_id,
    COALESCE(local_mi.slug, '')::text AS local_slug,
    COALESCE(local_mi.id, 0)::bigint  AS local_media_item_id
FROM artist_similar_artists asa
LEFT JOIN artists      local_a  ON local_a.id  = asa.local_artist_id
LEFT JOIN media_items  local_mi ON local_mi.id = local_a.media_item_id
WHERE asa.artist_id = sqlc.arg(artist_id)
ORDER BY asa.rank ASC
LIMIT sqlc.arg(artist_limit);

-- name: AllAlbumTracksHaveLoudness :one
-- Used by ScanTrackLoudnessWorker to decide whether to enqueue the
-- album-level worker after each track finishes. True only when every
-- track in the album has loudness data.
SELECT NOT EXISTS (
    SELECT 1 FROM tracks t
    JOIN track_files tf ON tf.track_id = t.id
    JOIN library_files lf ON lf.id = tf.library_file_id
    WHERE t.album_id = $1
      AND lf.deleted_at IS NULL
      AND tf.integrated_lufs IS NULL
) AS done;
