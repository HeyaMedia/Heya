-- name: CreateArtist :one
INSERT INTO artists (media_item_id, musicbrainz_id, name, sort_name, disambiguation, biography)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: CreateArtistIfNotExists :one
-- Scanner apply can process several scoped album folders for the same artist
-- concurrently. A plain INSERT poisons the transaction when another worker
-- wins uq_artists_name_disambig first, so use DO NOTHING and resolve the
-- canonical row in the same statement.
WITH inserted AS (
  INSERT INTO artists (media_item_id, musicbrainz_id, name, sort_name, disambiguation, biography)
  VALUES ($1, $2, $3, $4, $5, $6)
  ON CONFLICT DO NOTHING
  RETURNING 0::int AS ord, artists.*
),
by_media_item AS (
  SELECT 1::int AS ord, artists.*
  FROM artists
  WHERE media_item_id = $1
),
by_name_disambig AS (
  SELECT 2::int AS ord, artists.*
  FROM artists
  WHERE lower(name) = lower($3)
    AND lower(disambiguation) = lower($5)
    AND name <> ''
)
SELECT id, media_item_id, musicbrainz_id, name, sort_name, disambiguation, biography, search_vector,
       discography_enriched_at, cover_art_enriched_at, listeners, playcount, popularity, annotation,
       urls, wikipedia_links, profiles, aliases, groups, members, artist_type, begin_date, begin_year,
       end_date, ended, deathday, birthplace, tags, genres, metadata_sources, followers
FROM (
  SELECT * FROM inserted
  UNION ALL
  SELECT * FROM by_media_item
  UNION ALL
  SELECT * FROM by_name_disambig
) candidates
ORDER BY ord
LIMIT 1;

-- name: GetArtistByMediaItemID :one
SELECT * FROM artists WHERE media_item_id = $1;

-- name: GetArtistByID :one
SELECT * FROM artists WHERE id = $1;

-- name: ListArtistsByLibrary :many
SELECT a.* FROM artists a
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE mi.library_id = $1
ORDER BY a.name;

-- name: ListStaleArtistsByLibrary :many
-- Artists in the library whose discography enrichment is older than $2 (or never enriched).
SELECT a.* FROM artists a
JOIN media_item_cards mi ON mi.id = a.media_item_id
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
       title          = CASE WHEN $3::text  != '' AND COALESCE(field_provenance->>'title', '') <> 'user' THEN $3 ELSE title END,
       year           = CASE WHEN $4::text  != '' AND COALESCE(field_provenance->>'year', '') <> 'user' THEN $4 ELSE year END,
       album_type     = CASE WHEN $5::text  != '' AND COALESCE(field_provenance->>'album_type', '') <> 'user' THEN $5 ELSE album_type END,
       label          = CASE WHEN $6::text  != '' AND COALESCE(field_provenance->>'label', '') <> 'user' THEN $6 ELSE label END,
       country        = CASE WHEN $7::text  != '' AND COALESCE(field_provenance->>'country', '') <> 'user' THEN $7 ELSE country END,
       barcode        = CASE WHEN $8::text  != '' AND COALESCE(field_provenance->>'barcode', '') <> 'user' THEN $8 ELSE barcode END,
       release_date   = CASE WHEN $9::date IS NOT NULL AND COALESCE(field_provenance->>'release_date', '') <> 'user' THEN $9 ELSE release_date END,
       cover_path     = CASE WHEN $10::text != '' THEN $10 ELSE cover_path    END
 WHERE id = $1;

-- name: SetAlbumFieldProvenance :exec
UPDATE albums SET field_provenance = $2 WHERE id = $1;

-- name: UpdateTrackFromEnrichment :exec
-- Overwrites track title and duration with enriched data (heya.media wins
-- over filename when present). NFO/path data is the seed; this is the upgrade.
UPDATE tracks
   SET title    = CASE WHEN $2::text != '' THEN $2 ELSE title    END,
       duration = CASE WHEN $3::int  > 0   THEN $3 ELSE duration END
 WHERE id = $1;

-- name: UpdateMediaItemExternalIds :exec
WITH entity AS (
  SELECT id, library_id FROM media_items WHERE id = $1
),
desired_external_ids AS (
  SELECT kv.key AS provider, kv.value AS external_id
  FROM jsonb_each_text(
    CASE
      WHEN jsonb_typeof(sqlc.arg(external_ids)::jsonb) = 'object' THEN sqlc.arg(external_ids)::jsonb
      ELSE '{}'::jsonb
    END
  ) AS kv(key, value)
  WHERE kv.key <> '' AND kv.value <> ''
),
inserted AS (
  INSERT INTO media_item_external_ids (media_item_id, library_id, provider, external_id, source)
  SELECT entity.id, entity.library_id, desired.provider, desired.external_id, 'music.enrichment'
  FROM entity, desired_external_ids desired
  ON CONFLICT (media_item_id, provider) DO UPDATE SET
    library_id = EXCLUDED.library_id,
    external_id = EXCLUDED.external_id,
    source = EXCLUDED.source,
    updated_at = now()
),
deleted AS (
  DELETE FROM media_item_external_ids existing
  WHERE existing.media_item_id = $1
    AND NOT EXISTS (
      SELECT 1
      FROM desired_external_ids desired
      WHERE desired.provider = existing.provider
    )
)
UPDATE media_items SET updated_at = now() WHERE media_items.id = $1;

-- name: GetArtistByMusicBrainzID :one
SELECT * FROM artists WHERE musicbrainz_id = $1 AND musicbrainz_id != '';

-- name: GetArtistByLibraryMediaItemMBID :one
-- Finds the artist whose parent media_item already claims this MBID in
-- external_ids for the library — the row a CreateMediaItem with the same
-- mbid would collide with on idx_media_items_mbid_unique. The artists row
-- can disagree (enrich merges upstream ids onto the media_item but never
-- rewrites a non-empty artists.musicbrainz_id), so the squatter is only
-- discoverable through this join.
SELECT a.* FROM artists a
JOIN media_items mi ON mi.id = a.media_item_id
JOIN media_item_external_ids ei ON ei.media_item_id = mi.id
WHERE mi.library_id = sqlc.arg(library_id)
  AND ei.provider = 'mbid'
  AND ei.external_id = sqlc.arg(mbid)::text
LIMIT 1;

-- name: GetArtistByNameAndDisambiguation :one
SELECT * FROM artists
WHERE lower(name) = lower($1) AND lower(disambiguation) = lower($2)
LIMIT 1;

-- name: GetArtistByNameAndDisambiguationExcludingID :one
-- Used by the merge-detection path in RefreshMusicArtist when no MBID
-- helped resolve a canonical sibling — falls back to "is there an
-- existing row whose (name, disambig) already matches what we're about
-- to write?" so the unique-constraint collision turns into a merge.
-- The `disambiguation != ''` guard keeps two same-named but undisambiguated
-- acts (e.g. "Ado", "666") from fusing — same name alone is too weak.
SELECT * FROM artists
WHERE lower(name) = lower($1)
  AND lower(disambiguation) = lower($2)
  AND disambiguation != ''
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
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE mi.slug = $1 AND mi.slug <> '' AND al.slug = $2 AND al.slug <> ''
LIMIT 1;

-- name: ListAlbumsByArtist :many
SELECT * FROM albums WHERE artist_id = $1 ORDER BY year ASC, title ASC;

-- name: ListArtistAlbumsWithRemoteCovers :many
-- Albums still pointing at an upstream cover URL — enrich warms these so the
-- first grid view never blocks on a synchronous download.
SELECT id, cover_path FROM albums
WHERE artist_id = $1 AND cover_path LIKE 'http%'
ORDER BY id;

-- name: ListAlbumsWithRemoteCovers :many
-- Sweep variant: pages every album whose cover was never materialized
-- locally, joined to the owning artist's media item for event payloads.
SELECT albums.id, albums.cover_path, artists.media_item_id
FROM albums
JOIN artists ON artists.id = albums.artist_id
WHERE albums.cover_path LIKE 'http%'
  AND albums.id > $1
ORDER BY albums.id
LIMIT $2;

-- name: AlbumHasFileOutsideFolder :one
-- True when the album has at least one track file NOT under `folder` — i.e. it's
-- "mixed" and a whole-album move would drag foreign-folder files along, so the
-- split must work at track-file granularity instead.
SELECT EXISTS (
  SELECT 1 FROM tracks t
  JOIN track_files tf ON tf.track_id = t.id
  JOIN library_files lf ON lf.id = tf.library_file_id
  WHERE t.album_id = sqlc.arg(album_id)
    AND sqlc.arg(folder) <> ALL(string_to_array(lf.path, '/'))
);

-- name: ListAlbumTracksUnderFolder :many
-- Tracks of an album that have at least one track file under `folder`. Used by
-- the split tool to peel a mixed album's foreign-folder files onto sibling
-- tracks under the destination artist.
SELECT DISTINCT t.id, t.disc_number, t.track_number, t.title
FROM tracks t
JOIN track_files tf ON tf.track_id = t.id
JOIN library_files lf ON lf.id = tf.library_file_id
WHERE t.album_id = sqlc.arg(album_id)
  AND sqlc.arg(folder) = ANY(string_to_array(lf.path, '/'))
ORDER BY t.disc_number, t.track_number;

-- name: MoveTrackFilesUnderFolderToTrack :exec
-- Move src_track's files that live under `folder` onto dst_track. track_files are
-- unique on library_file_id only, so re-pointing track_id never collides.
UPDATE track_files tf
SET track_id = sqlc.arg(dst_track_id)
FROM library_files lf
WHERE tf.library_file_id = lf.id
  AND tf.track_id = sqlc.arg(src_track_id)
  AND sqlc.arg(folder) = ANY(string_to_array(lf.path, '/'));

-- name: TrackHasFileOutsideFolder :one
-- True when the track has at least one file NOT under `folder` — i.e. it's a
-- "mixed" track (the same song fused from two folders) whose folder files should
-- be peeled, vs a whole-track move that can carry all its state along.
SELECT EXISTS (
  SELECT 1 FROM track_files tf
  JOIN library_files lf ON lf.id = tf.library_file_id
  WHERE tf.track_id = sqlc.arg(track_id)
    AND sqlc.arg(folder) <> ALL(string_to_array(lf.path, '/'))
);

-- name: MoveTrackToAlbum :exec
-- Relocate a whole track row to another album. track_id is unchanged, so every
-- track-owned row — ratings, playlist entries, play history, facets, metadata,
-- track_files — rides along. Used by the split tool for a track that lives
-- entirely under the split folder (no state lost, unlike a file-peel + delete).
UPDATE tracks SET album_id = sqlc.arg(dst_album_id) WHERE id = sqlc.arg(track_id);

-- name: GetTrackByAlbumDiscTrack :one
SELECT * FROM tracks
WHERE album_id = sqlc.arg(album_id)
  AND disc_number = sqlc.arg(disc_number)
  AND track_number = sqlc.arg(track_number);

-- name: MoveAllTrackFilesToTrack :exec
UPDATE track_files SET track_id = sqlc.arg(dst_track_id) WHERE track_id = sqlc.arg(src_track_id);

-- name: MergeTrackRatingsInto :exec
-- Move user_track_ratings from src to dst track, keeping the higher rating.
INSERT INTO user_track_ratings (user_id, track_id, rating)
SELECT r.user_id, sqlc.arg(dst_track_id), r.rating
FROM user_track_ratings r WHERE r.track_id = sqlc.arg(src_track_id)
ON CONFLICT (user_id, track_id) DO UPDATE
SET rating = GREATEST(user_track_ratings.rating, EXCLUDED.rating), updated_at = now();

-- name: MergeTrackPlaylistsInto :exec
INSERT INTO user_playlist_tracks (playlist_id, track_id, position, added_at)
SELECT p.playlist_id, sqlc.arg(dst_track_id), p.position, p.added_at
FROM user_playlist_tracks p WHERE p.track_id = sqlc.arg(src_track_id)
ON CONFLICT (playlist_id, track_id) DO NOTHING;

-- name: ReparentTrackPlayEventsInto :exec
UPDATE play_events SET track_id = sqlc.arg(dst_track_id) WHERE track_id = sqlc.arg(src_track_id);

-- name: DeleteTrackByID :exec
DELETE FROM tracks WHERE id = sqlc.arg(id);

-- name: DeleteEmptyTracksOfAlbum :exec
-- Drop the album's tracks that no longer have any track files (their files moved
-- out during a split). CASCADE clears facets / play_events / ratings.
DELETE FROM tracks t
WHERE t.album_id = sqlc.arg(album_id)
  AND NOT EXISTS (SELECT 1 FROM track_files tf WHERE tf.track_id = t.id);

-- name: AlbumHasTracks :one
SELECT EXISTS (SELECT 1 FROM tracks WHERE album_id = sqlc.arg(album_id));

-- name: ListAlbumsByArtistUnderFolder :many
-- Albums of an artist that have at least one track file living under the given
-- top-level folder, matched as an EXACT path segment (so "Avicii" never catches
-- "Avicii Presents"). Used by the artist un-fuse tool to find the foreign-folder
-- albums a bad enrichment merge pulled into the wrong artist.
SELECT DISTINCT al.*
FROM albums al
WHERE al.artist_id = sqlc.arg(artist_id)
  AND EXISTS (
    SELECT 1
    FROM tracks t
    JOIN track_files tf ON tf.track_id = t.id
    JOIN library_files lf ON lf.id = tf.library_file_id
    WHERE t.album_id = al.id
      AND sqlc.arg(folder) = ANY(string_to_array(lf.path, '/'))
  )
ORDER BY al.year, al.title;

-- name: GetAlbumByID :one
SELECT * FROM albums WHERE id = $1;

-- name: GetAlbumByMusicBrainzID :one
SELECT * FROM albums WHERE musicbrainz_id = $1 AND musicbrainz_id != '';

-- name: GetAlbumByArtistMusicBrainzID :one
SELECT * FROM albums
WHERE artist_id = $1 AND musicbrainz_id = $2 AND musicbrainz_id != ''
LIMIT 1;

-- name: GetAlbumByArtistTitleYear :one
SELECT * FROM albums
WHERE artist_id = $1 AND lower(title) = lower($2) AND year = $3
LIMIT 1;

-- name: LockArtistAlbumsForApply :one
-- Scanner apply uses a read-before-write identity decision for every album.
-- Serialize those decisions per artist so concurrent scoped scan entities
-- cannot both observe a missing tuple and race into the unique index.
SELECT id FROM artists WHERE id = $1 FOR UPDATE;

-- name: UpdateAlbum :one
UPDATE albums
SET title = $2, slug = $3, year = $4, musicbrainz_id = $5, album_type = $6,
    genres = $7, cover_path = $8, release_date = $9,
    label = $10, country = $11, barcode = $12, total_tracks = $13, total_discs = $14, tags = $15
WHERE id = $1
RETURNING *;

-- name: CreateTrack :one
INSERT INTO tracks (album_id, disc_number, track_number, title, duration)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetOrCreateTrack :one
-- Idempotent track creation: on conflict, return the existing row unchanged.
-- Physical file identity and sidecars live exclusively in track_files.
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

-- name: UpdateTrackFileLyricsPath :exec
UPDATE track_files SET lyrics_path = $2 WHERE id = $1;

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
    tags             = $19,
    genres           = $20,
    metadata_sources = $21,
    followers        = $22
WHERE id = $1;

-- name: InsertArtistTopTracks :exec
-- Top tracks are a small ranked list per artist. We replace-on-refresh
-- rather than upsert because the rank ordering is what we actually care
-- about — the caller sequences DeleteArtistTopTracks first (this pair was
-- one statement with a data-modifying `WITH deleted AS (DELETE ...)` CTE,
-- but an unreferenced CTE's execution order vs the main statement is
-- UNSPECIFIED, and the insert ran first and collided with the old rows
-- on (artist_id, rank)).
-- jsonb_array_elements + per-field ->> extraction, NOT jsonb_to_recordset:
-- sqlc v1.31 can't model a recordset's AS value(...) column list — it
-- expanded `SELECT *` over it into the bare alias (one record column),
-- which broke this write with `column "rank" does not exist` on every
-- refresh since the V2 migration, and it rejects qualified references
-- (value.rank) outright. Every key is always present: the rows are
-- marshaled from a Go struct with no omitempty.
INSERT INTO artist_top_tracks (
  artist_id, rank, provider, provider_rank, title, mbid,
  recording_entity_id, playcount, listeners, url
)
SELECT sqlc.arg(artist_id),
       (t.value->>'rank')::int,
       t.value->>'provider',
       (t.value->>'provider_rank')::int,
       t.value->>'title',
       t.value->>'mbid',
       NULLIF(t.value->>'recording_entity_id', '')::uuid,
       (t.value->>'playcount')::bigint,
       (t.value->>'listeners')::bigint,
       t.value->>'url'
FROM jsonb_array_elements(sqlc.arg(tracks)::jsonb) AS t;

-- name: ReplaceArtistSimilarArtists :exec
-- Same replace-on-refresh story as top_tracks. The match scores and
-- ordering shift slightly on every Last.fm/ListenBrainz refresh, so
-- treating the list as a transactional swap keeps the page snappy.
DELETE FROM artist_similar_artists WHERE artist_id = $1;

-- name: CreateArtistSimilarArtist :exec
INSERT INTO artist_similar_artists (
    artist_id, rank, name, mbid, match_score, url, local_artist_id, provider
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

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
    artist_credits   = $15,
    description      = CASE WHEN $16::text != '' THEN $16 ELSE description END,
    review           = CASE WHEN $17::text != '' THEN $17 ELSE review END,
    ratings          = $18,
    editions         = $19,
    sales            = CASE WHEN $20::bigint != 0 THEN $20 ELSE sales END,
    artwork          = $21,
    script           = CASE WHEN $22::text != '' THEN $22 ELSE script END,
    release_events   = $23
WHERE id = $1;

-- name: UpdateTrackCredits :exec
-- Performance credits from the canonical recording document. Written only
-- when the per-recording fetch succeeded — a failed fetch keeps the last
-- known credits instead of clearing them (its own statement so the
-- extended-metadata write can't half-apply on fetch errors).
UPDATE tracks SET credits = $2 WHERE id = $1;

-- name: UpdateTrackExtendedMetadata :exec
-- The post-00019 track columns. external_ids / isrc / recording_mbid remain
-- indexed matching evidence; lyrics_available comes from HeyaMetadata's batched
-- canonical release projection and must not trigger per-track probes.
-- preview_url is the iTunes/Deezer 30-second sample for hover previews.
UPDATE tracks SET
    external_ids   = $2,
    isrc           = CASE WHEN $3::text != '' THEN $3 ELSE isrc END,
    recording_mbid = CASE WHEN $4::text != '' THEN $4 ELSE recording_mbid END,
    preview_url    = CASE WHEN $5::text != '' THEN $5 ELSE preview_url END,
    explicit       = $6,
    artist_credits = $7,
    lyrics_available = $8
WHERE id = $1;

-- name: ListTracksByAlbum :many
SELECT * FROM tracks WHERE album_id = $1 ORDER BY disc_number ASC, track_number ASC;

-- name: GetAlbumReleaseDir :one
-- Returns the on-disk release directory for an album (parent dir of any of
-- its tracks). Used by the music NFO writer to know where to drop album.nfo.
-- Empty string if the album has no files (e.g. tracks all soft-deleted).
SELECT COALESCE((
  SELECT lf.path
  FROM tracks t
  JOIN track_files tf ON tf.track_id = t.id
  JOIN library_files lf ON lf.id = tf.library_file_id
  WHERE t.album_id = $1 AND lf.deleted_at IS NULL
  ORDER BY tf.quality_score DESC, tf.id ASC
  LIMIT 1
), '')::text AS file_path;

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
       COALESCE(primary_file.lyrics_path, '')::text AS lyrics_path,
       t.lyrics_available,
       t.recording_mbid,
       t.isrc,
       t.explicit,
       -- Primary (best-quality) file's on-disk path for the track-info
       -- dialog; ordering mirrors ListTrackFilesByTrack's [0] pick.
       COALESCE(primary_file.path, '')::text AS file_path,
       al.title          AS album_title,
       al.slug           AS album_slug,
       al.year           AS album_year,
       al.cover_path     AS album_cover_path,
       al.integrated_lufs AS album_integrated_lufs,
       al.true_peak_db    AS album_true_peak_db,
       a.id              AS artist_id,
       a.name            AS artist_name,
       mi.slug           AS artist_slug
FROM tracks t
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
LEFT JOIN LATERAL (
  SELECT lf.path, tf.lyrics_path
  FROM track_files tf
  JOIN library_files lf ON lf.id = tf.library_file_id
  WHERE tf.track_id = t.id AND lf.deleted_at IS NULL
  ORDER BY tf.quality_score DESC, tf.id ASC
  LIMIT 1
) primary_file ON true
WHERE t.id = $1
LIMIT 1;

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
        -- A scanner plan without a lyrics sidecar must not erase a path found
        -- by the local-assets pass (or migrated from the old track column).
        lyrics_path = CASE
            WHEN EXCLUDED.lyrics_path <> '' THEN EXCLUDED.lyrics_path
            ELSE track_files.lyrics_path
        END,
        -- Audio-derived data is only valid for the bytes it was computed
        -- from: when the file changed in place, reset loudness so the pump
        -- re-measures instead of settling stale. Fingerprints are owned by
        -- library_file_fingerprints and invalidate against source size/mtime.
        integrated_lufs = CASE WHEN track_files.size_bytes = EXCLUDED.size_bytes THEN track_files.integrated_lufs ELSE NULL END,
        loudness_range_db = CASE WHEN track_files.size_bytes = EXCLUDED.size_bytes THEN track_files.loudness_range_db ELSE NULL END,
        loudness_analyzed_at = CASE WHEN track_files.size_bytes = EXCLUDED.size_bytes THEN track_files.loudness_analyzed_at ELSE NULL END,
        boundaries_analyzed_at = CASE WHEN track_files.size_bytes = EXCLUDED.size_bytes THEN track_files.boundaries_analyzed_at ELSE NULL END,
        size_bytes = EXCLUDED.size_bytes
RETURNING *;

-- name: ListTrackFilesByTrack :many
-- Ordered by quality (best first) so callers can pick [0] as the primary.
SELECT tf.*
FROM track_files tf
JOIN library_files lf ON lf.id = tf.library_file_id
WHERE tf.track_id = $1 AND lf.deleted_at IS NULL
ORDER BY tf.quality_score DESC, tf.id ASC;

-- name: ListTrackFilesByAlbum :many
-- Whole-album batch of ListTrackFilesByTrack — the album detail page groups
-- these by track_id instead of issuing one query per track (up to 210 round
-- trips on the biggest album). Within each track_id the order matches
-- ListTrackFilesByTrack: best quality first.
SELECT tf.*
FROM track_files tf
JOIN tracks t ON t.id = tf.track_id
JOIN library_files lf ON lf.id = tf.library_file_id
WHERE t.album_id = $1 AND lf.deleted_at IS NULL
ORDER BY tf.track_id ASC, tf.quality_score DESC, tf.id ASC;

-- name: ListTrackFilePathsByAlbum :many
-- Physical-file view for local artwork/lyrics discovery. A track may have
-- several encodings in different directories; scan every live file.
SELECT tf.id, tf.track_id, lf.path AS file_path, tf.lyrics_path
FROM track_files tf
JOIN tracks t ON t.id = tf.track_id
JOIN library_files lf ON lf.id = tf.library_file_id
WHERE t.album_id = $1 AND lf.deleted_at IS NULL
ORDER BY t.disc_number, t.track_number, tf.quality_score DESC, tf.id ASC;

-- name: GetTrackFileByLibraryFileID :one
SELECT * FROM track_files WHERE library_file_id = $1;

-- name: UpdateTrackFileProbeData :exec
-- Called by FFProbeWorker after probing an audio file. Updates the per-file
-- physical properties and the refined quality_score that incorporates real
-- bitrate / sample rate / bit depth. Also backfills tracks.duration when
-- upstream metadata left it 0 — ffprobe is ground truth, and the FE renders
-- tracks.duration everywhere (0 shows as "0:00").
WITH updated AS (
    UPDATE track_files
       SET bitrate_kbps   = $2,
           sample_rate_hz = $3,
           bit_depth      = $4,
           channels       = $5,
           duration       = $6,
           quality_score  = $7
     WHERE track_files.id = $1
     RETURNING track_files.track_id, track_files.duration
)
UPDATE tracks t
   SET duration = u.duration
  FROM updated u
 WHERE t.id = u.track_id
   AND t.duration = 0
   AND u.duration > 0;

-- name: ListMusicArtists :many
-- Merged listing across every music library, with album + track counts so
-- the Artists grid can show density at a glance. Sorted by sort_name when
-- present, falling back to name.
SELECT a.*,
       mi.slug         AS slug,
       mi.public_id    AS media_item_public_id,
       mi.poster_path  AS poster_path,
       (SELECT count(*) FROM albums  al WHERE al.artist_id = a.id)                              AS album_count,
       (SELECT count(*) FROM tracks  t  JOIN albums al ON al.id = t.album_id WHERE al.artist_id = a.id) AS track_count,
       EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = a.media_item_id AND lf.deleted_at IS NULL) AS available
FROM artists a
JOIN media_item_cards mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
ORDER BY lower(coalesce(NULLIF(a.sort_name, ''), a.name)) ASC
LIMIT $1 OFFSET $2;

-- name: CountMusicArtists :one
-- Bare count — artists rows are created solely by the music matcher, so they
-- exist only under music libraries and the joins filtered nothing.
SELECT count(*) FROM artists;

-- name: GetMusicArtistBySlug :one
-- Direct artist lookup by media-item slug. Same row shape as ListMusicArtists
-- so the FE can render headers from either feed without branching.
SELECT a.*,
       mi.slug         AS slug,
       mi.public_id    AS media_item_public_id,
       mi.poster_path  AS poster_path,
       (SELECT count(*) FROM albums  al WHERE al.artist_id = a.id)                              AS album_count,
       (SELECT count(*) FROM tracks  t  JOIN albums al ON al.id = t.album_id WHERE al.artist_id = a.id) AS track_count,
       EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = a.media_item_id AND lf.deleted_at IS NULL) AS available
FROM artists a
JOIN media_item_cards mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE mi.slug = $1 AND mi.slug <> '' AND l.media_type = 'music'
LIMIT 1;

-- name: ListAlbumsByArtistSlug :many
-- Paginated album listing for one artist. Same row shape as ListMusicAlbums
-- so the FE can reuse the album-row component without branching.
SELECT al.*,
       a.name           AS artist_name,
       mi.slug          AS artist_slug,
       (SELECT count(*) FROM tracks t WHERE t.album_id = al.id) AS track_count,
       EXISTS (SELECT 1 FROM tracks t JOIN track_files tf ON tf.track_id = t.id JOIN library_files lf ON lf.id = tf.library_file_id WHERE t.album_id = al.id AND lf.deleted_at IS NULL) AS available
FROM albums al
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE mi.slug = $1 AND mi.slug <> ''
ORDER BY al.year DESC NULLS LAST, lower(al.title) ASC
LIMIT $2 OFFSET $3;

-- name: CountAlbumsByArtistSlug :one
SELECT count(*) FROM albums al
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE mi.slug = $1 AND mi.slug <> '';

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
       mi.slug           AS artist_slug,
       EXISTS (SELECT 1 FROM track_files tf JOIN library_files lf ON lf.id = tf.library_file_id WHERE tf.track_id = t.id AND lf.deleted_at IS NULL) AS available
FROM tracks t
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE mi.slug = $1 AND mi.slug <> ''
ORDER BY al.year DESC NULLS LAST, lower(al.title) ASC,
         t.disc_number ASC, t.track_number ASC
LIMIT $2 OFFSET $3;

-- name: CountTracksByArtistSlug :one
SELECT count(*) FROM tracks t
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE mi.slug = $1 AND mi.slug <> '';

-- name: ListMusicAlbums :many
-- Merged listing of every album across every music library. Joins the artist
-- so the Albums grid can render "Title — Artist" without a second round-trip.
-- album_slug is unique within the artist; artist_slug routes to the artist
-- detail page.
--
-- Perf notes (do not "simplify"):
--  * The keys subquery walks idx_albums_catalog_order (trigger-maintained
--    sort_artist + same-table year/title) as an index-only scan, so the deep
--    offsets the virtual scroller random-accesses cost an index skip instead
--    of sorting all 54k joined rows (222ms at offset 50k before; ~1-10ms
--    after). Joins and the per-album track_count/available subplans run on
--    page rows only — inlined, the planner hashes the EXISTS over all
--    library_files (measured 12x regression on the old shape).
--  * No media-type filter needed: albums exist only under music libraries
--    (see CountMusicAlbums).
--  * al.id tie-break: duplicate (name, year, title) sort keys exist, so
--    OFFSET pagination needs it to stay deterministic across plan changes.
SELECT sub.*,
       (SELECT count(*) FROM tracks t WHERE t.album_id = sub.id) AS track_count,
       EXISTS (SELECT 1 FROM tracks t JOIN track_files tf ON tf.track_id = t.id JOIN library_files lf ON lf.id = tf.library_file_id WHERE t.album_id = sub.id AND lf.deleted_at IS NULL) AS available
FROM (
    SELECT al.*,
           a.name  AS artist_name,
           mi.slug AS artist_slug
    FROM (
        SELECT k.id
        FROM albums k
        ORDER BY k.sort_artist ASC, k.year ASC, k.sort_title ASC, k.id ASC
        LIMIT $1 OFFSET $2
    ) keys
    JOIN albums al ON al.id = keys.id
    JOIN artists     a  ON a.id  = al.artist_id
    JOIN media_item_cards mi ON mi.id = a.media_item_id
) sub
ORDER BY sub.sort_artist ASC, sub.year ASC, sub.sort_title ASC, sub.id ASC;

-- name: CountMusicAlbums :one
-- Bare count: albums exist only under music libraries (created solely by the
-- music matcher), so the artists/media_items/libraries joins filtered nothing
-- while costing 2.3k per-artist index probes.
SELECT count(*) FROM albums;

-- name: ListMusicTracks :many
-- Flat listing for the Songs tab. Carries everything the row needs:
-- title, duration, album title, artist name, slugs for navigation, and the
-- album cover for the thumbnail. album_slug + artist_slug together address
-- the album cover endpoint without an ID lookup.
--
-- Perf notes (do not "simplify"):
--  * The keys subquery selects the page by walking idx_tracks_catalog_order
--    (the trigger-maintained denormalized sort keys) as an index-only scan —
--    the virtual scroller random-accesses deep offsets (offset≈224k when the
--    scrollbar is dragged), and the previous cross-table ORDER BY hash-joined
--    and quicksorted all 280k tracks per deep page (464ms; this shape
--    measured 63ms worst-case, ~3ms shallow). Joins run on page rows only.
--  * No media-type filter needed: tracks exist only under music libraries
--    (created solely by the music matcher — see CountMusicTracks).
--  * The availability EXISTS stays outside the page derived table so it runs
--    per page row, not per candidate.
--  * The outer ORDER BY re-sorts only the page rows (cheap) and guarantees
--    output order; the expressions equal the denormalized keys by
--    construction (sort_artist = lower(artist name), etc).
--  * t.id tie-break keeps OFFSET page boundaries deterministic.
SELECT page.*,
       EXISTS (SELECT 1 FROM track_files tf JOIN library_files lf ON lf.id = tf.library_file_id WHERE tf.track_id = page.track_id AND lf.deleted_at IS NULL) AS available
FROM (
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
  FROM (
    SELECT k.id
    FROM tracks k
    ORDER BY k.sort_artist ASC, k.sort_album_year ASC, k.sort_album ASC,
             k.disc_number ASC, k.track_number ASC, k.id ASC
    LIMIT $1 OFFSET $2
  ) keys
  JOIN tracks t ON t.id = keys.id
  JOIN albums      al ON al.id = t.album_id
  JOIN artists     a  ON a.id  = al.artist_id
  JOIN media_item_cards mi ON mi.id = a.media_item_id
) page
ORDER BY lower(page.artist_name) ASC, page.album_year ASC, lower(page.album_title) ASC,
         page.disc_number ASC, page.track_number ASC, page.track_id ASC;

-- name: CountMusicTracks :one
-- Bare count — tracks exist only under music libraries; see CountMusicAlbums.
-- The joined form ran 50k per-album index probes (~200-370ms) on every Songs
-- page navigation.
SELECT count(*) FROM tracks;

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
-- Newest albums across every music library. Newest = highest album id since
-- albums get IDENTITY-generated IDs in insert order. The derived table pins
-- the plan to a backward albums_pkey scan (LIMIT before joins) AND keeps the
-- EXISTS below a cheap per-row probe — without it the planner hashes the
-- entire track_files x library_files join (measured 1.26s vs 1.9ms). Keep the
-- outer ORDER BY: plan-order preservation through the joins is not guaranteed.
SELECT al.*,
       a.name           AS artist_name,
       mi.slug          AS artist_slug,
       (SELECT count(*) FROM tracks t WHERE t.album_id = al.id) AS track_count,
       EXISTS (SELECT 1 FROM tracks t JOIN track_files tf ON tf.track_id = t.id JOIN library_files lf ON lf.id = tf.library_file_id WHERE t.album_id = al.id AND lf.deleted_at IS NULL) AS available,
       -- When the album's files actually landed (albums carry no created_at
       -- of their own) — feeds the "3d ago" chip on the Recently Added rail.
       (SELECT MIN(lf.created_at) FROM tracks t JOIN track_files tf ON tf.track_id = t.id JOIN library_files lf ON lf.id = tf.library_file_id WHERE t.album_id = al.id)::timestamptz AS added_at
FROM (
  SELECT * FROM albums ORDER BY id DESC LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off)
) al
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE mi.media_type = 'music'
ORDER BY al.id DESC;

-- name: ListRecentlyAddedArtists :many
-- Newest artists across every music library — uses discography_enriched_at
-- when present (signals the artist actually exists with metadata) else falls
-- back to the artist id. Nulls-last keeps fresh additions out of pole position
-- before their enrichment completes.
SELECT a.*,
       mi.slug         AS slug,
       mi.poster_path  AS poster_path,
       (SELECT count(*) FROM albums al WHERE al.artist_id = a.id) AS album_count,
       (SELECT count(*) FROM tracks t JOIN albums al ON al.id = t.album_id WHERE al.artist_id = a.id) AS track_count,
       EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = a.media_item_id AND lf.deleted_at IS NULL) AS available
FROM artists a
JOIN media_item_cards mi ON mi.id = a.media_item_id
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

-- name: UpdateTrackFileBoundaries :exec
-- Structural transition points (intro/outro/fade/silence, in ms) detected from
-- the RMS envelope. Written by ScanTrackLoudnessWorker in the same pass as
-- loudness, since it has already decoded the file. Feeds the client smart
-- crossfade.
UPDATE track_files
   SET intro_end_ms           = $2,
       outro_start_ms         = $3,
       fade_start_ms          = $4,
       silence_start_ms       = $5,
       boundaries_analyzed_at = now()
 WHERE id = $1;

-- name: ListTrackFilesPendingLoudness :many
-- Files in music libraries missing loudness OR boundary analysis (and not
-- soft-deleted). The kickoff pump sweeps this with an id cursor (after_id)
-- so one run visits each candidate exactly once — a file whose loudness job
-- failed permanently (lufs stays NULL) is passed over instead of being
-- re-enqueued in a loop. The worker only computes whichever piece is actually
-- missing, so re-listing already-loud tracks is cheap.
SELECT tf.id, tf.library_file_id, tf.track_id, lf.path
FROM track_files tf
JOIN library_files lf ON lf.id = tf.library_file_id
JOIN tracks t  ON t.id  = tf.track_id
JOIN albums al ON al.id = t.album_id
JOIN artists a ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
  AND lf.deleted_at IS NULL
  AND tf.id > sqlc.arg(after_id)::bigint
  AND (tf.integrated_lufs IS NULL OR tf.boundaries_analyzed_at IS NULL)
ORDER BY tf.id
LIMIT sqlc.arg(row_limit)::int;

-- name: GetLibraryFileFingerprint :one
SELECT *
FROM library_file_fingerprints
WHERE library_file_id = $1;

-- name: GetLibraryFileFingerprintLookup :one
SELECT *
FROM library_file_fingerprint_lookups
WHERE library_file_id = sqlc.arg(library_file_id)
  AND provider = sqlc.arg(provider);

-- name: UpsertLibraryFileFingerprintLookup :one
INSERT INTO library_file_fingerprint_lookups (
    library_file_id,
    provider,
    evidence_key,
    state,
    results,
    error_message,
    retry_after
) VALUES (
    sqlc.arg(library_file_id),
    sqlc.arg(provider),
    sqlc.arg(evidence_key),
    sqlc.arg(state),
    sqlc.arg(results),
    sqlc.arg(error_message),
    sqlc.narg(retry_after)
)
ON CONFLICT (library_file_id, provider) DO UPDATE
SET evidence_key  = EXCLUDED.evidence_key,
    state         = EXCLUDED.state,
    results       = EXCLUDED.results,
    error_message = EXCLUDED.error_message,
    observed_at   = now(),
    retry_after   = EXCLUDED.retry_after,
    updated_at    = now()
RETURNING *;

-- name: UpsertLibraryFileFingerprint :one
INSERT INTO library_file_fingerprints (
    library_file_id,
    algorithm,
    fingerprint,
    fingerprint_duration_secs,
    source_duration_secs,
    source_size,
    source_mtime
) VALUES (
    sqlc.arg(library_file_id),
    sqlc.arg(algorithm),
    sqlc.arg(fingerprint),
    sqlc.arg(fingerprint_duration_secs),
    sqlc.arg(source_duration_secs),
    sqlc.arg(source_size),
    sqlc.narg(source_mtime)
)
ON CONFLICT (library_file_id) DO UPDATE
SET algorithm                 = EXCLUDED.algorithm,
    fingerprint               = EXCLUDED.fingerprint,
    fingerprint_duration_secs = EXCLUDED.fingerprint_duration_secs,
    source_duration_secs      = EXCLUDED.source_duration_secs,
    source_size               = EXCLUDED.source_size,
    source_mtime              = EXCLUDED.source_mtime,
    fingerprinted_at          = now(),
    updated_at                = now()
RETURNING *;

-- name: ListMusicLibraryFilesPendingFingerprint :many
-- Includes matched and unmatched audio because fingerprint evidence belongs
-- to the physical file. Source size/mtime invalidates evidence when a path is
-- overwritten in place while retaining its library_files identity.
SELECT lf.id, lf.path
FROM library_files lf
JOIN libraries l ON l.id = lf.library_id
LEFT JOIN library_file_fingerprints fp ON fp.library_file_id = lf.id
WHERE l.media_type = 'music'
  AND lf.deleted_at IS NULL
  AND lf.id > sqlc.arg(after_id)::bigint
  AND lower(lf.path) ~ '\.(flac|mp3|m4a|aac|ogg|opus|wav|wma|ape|wv|alac|aiff|aif)$'
  AND (
      fp.library_file_id IS NULL
      OR fp.algorithm <> 1
      OR fp.source_size <> lf.size
      OR fp.source_mtime IS DISTINCT FROM lf.mtime
  )
ORDER BY lf.id
LIMIT sqlc.arg(row_limit)::int;

-- name: UpdateAlbumLoudness :exec
UPDATE albums
   SET integrated_lufs      = $2,
       true_peak_db         = $3,
       loudness_range_db    = $4,
       loudness_analyzed_at = now()
 WHERE id = $1;

-- name: ListAlbumsPendingLoudness :many
-- Albums whose track loudness is fully populated but their own album-level
-- loudness has not yet been measured. Cursor-swept (after_id) by the kickoff
-- pump's album phase, which only starts once the track sweep has drained —
-- at that point eligibility is stable, so a single monotonic pass is complete.
SELECT al.id, al.title
FROM albums al
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
JOIN libraries   l  ON l.id  = mi.library_id
WHERE l.media_type = 'music'
  AND al.loudness_analyzed_at IS NULL
  AND al.id > sqlc.arg(after_id)::bigint
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
LIMIT sqlc.arg(row_limit)::int;

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
-- The `!= ''` guard stops an empty MBID arg from matching every
-- empty-MBID row and fusing unrelated artists.
SELECT *
FROM artists
WHERE musicbrainz_id = sqlc.arg(mbid)
  AND musicbrainz_id != ''
  AND id != sqlc.arg(exclude_id)
LIMIT 1;

-- name: ReparentAlbums :exec
-- Move every album from src_id over to dst_id. Tracks follow via
-- albums.artist_id (track_id is keyed on album_id, not artist_id).
-- NOTE: blind move — only safe when no album collides on
-- uq_albums_artist_title_year. The artist merge no longer uses this; it walks
-- albums one at a time (ReparentAlbumToArtist for non-colliders, the album
-- merge queries below for colliders). Retained for callers that have already
-- proven the destination is collision-free.
UPDATE albums SET artist_id = sqlc.arg(dst_id) WHERE albums.artist_id = sqlc.arg(src_id);

-- name: ReparentAlbumToArtist :exec
-- Move ONE album to a new artist. Drops the album's slug when the destination
-- artist already has an album using it, sidestepping uq_albums_artist_slug;
-- the slug regenerates on the next refresh (that index is WHERE slug != '').
-- Used by the artist merge for albums that do NOT collide on
-- (lower(title), year) — colliders are folded by the album-merge queries below.
UPDATE albums a SET
    artist_id = sqlc.arg(dst_artist_id),
    slug = CASE WHEN EXISTS (
        SELECT 1 FROM albums b
        WHERE b.artist_id = sqlc.arg(dst_artist_id)
          AND b.slug = a.slug AND b.slug != '' AND b.id != a.id
    ) THEN '' ELSE a.slug END
WHERE a.id = sqlc.arg(album_id);

-- name: ReparentCollidingAlbumTrackFiles :exec
-- Album-merge step 1. For src_album tracks whose (disc, track_number) already
-- exists on dst_album, move their track_files onto the matching dst track so
-- the audio survives the merge. track_files are unique only on
-- library_file_id, so re-pointing track_id never collides.
UPDATE track_files tf
SET track_id = dst.id
FROM tracks src
JOIN tracks dst
    ON dst.album_id = sqlc.arg(dst_album_id)
   AND dst.disc_number = src.disc_number
   AND dst.track_number = src.track_number
WHERE tf.track_id = src.id
  AND src.album_id = sqlc.arg(src_album_id);

-- name: MergeCollidingAlbumTrackRatings :exec
-- Album-merge step 2a. Fold user_track_ratings off the colliding src tracks
-- onto their dst counterpart, keeping the higher rating. MUST run before the
-- src tracks are deleted, or CASCADE drops these user ratings.
INSERT INTO user_track_ratings (user_id, track_id, rating)
SELECT utr.user_id, dst.id, utr.rating
FROM user_track_ratings utr
JOIN tracks src ON src.id = utr.track_id AND src.album_id = sqlc.arg(src_album_id)
JOIN tracks dst
    ON dst.album_id = sqlc.arg(dst_album_id)
   AND dst.disc_number = src.disc_number
   AND dst.track_number = src.track_number
ON CONFLICT (user_id, track_id) DO UPDATE
SET rating = GREATEST(user_track_ratings.rating, EXCLUDED.rating),
    updated_at = now();

-- name: MergeCollidingAlbumTrackPlaylists :exec
-- Album-merge step 2b. Move playlist memberships off the colliding src tracks
-- onto their dst counterpart, keeping dst's slot when the track is already in
-- the playlist. MUST run before the src tracks are deleted.
INSERT INTO user_playlist_tracks (playlist_id, track_id, position, added_at)
SELECT pt.playlist_id, dst.id, pt.position, pt.added_at
FROM user_playlist_tracks pt
JOIN tracks src ON src.id = pt.track_id AND src.album_id = sqlc.arg(src_album_id)
JOIN tracks dst
    ON dst.album_id = sqlc.arg(dst_album_id)
   AND dst.disc_number = src.disc_number
   AND dst.track_number = src.track_number
ON CONFLICT (playlist_id, track_id) DO NOTHING;

-- name: ReparentCollidingAlbumTrackPlayEvents :exec
-- Album-merge step 2c. Move play history off the colliding src tracks onto
-- their dst counterpart. play_events has no per-track unique key, so a plain
-- UPDATE moves every event. MUST run before the src tracks are deleted.
UPDATE play_events pe
SET track_id = dst.id
FROM tracks src
JOIN tracks dst
    ON dst.album_id = sqlc.arg(dst_album_id)
   AND dst.disc_number = src.disc_number
   AND dst.track_number = src.track_number
WHERE pe.track_id = src.id AND src.album_id = sqlc.arg(src_album_id);

-- name: DeleteCollidingAlbumTracks :exec
-- Album-merge step 3. Drop the src_album tracks whose (disc, track_number)
-- already exists on dst_album. Their track_files (step 1) and user-scoped rows
-- — ratings, playlists, play history (steps 2a-2c) — have already moved; only
-- the derived track_facets remain and CASCADE clears them (they regenerate).
DELETE FROM tracks src
WHERE src.album_id = sqlc.arg(src_album_id)
  AND EXISTS (
      SELECT 1 FROM tracks dst
      WHERE dst.album_id = sqlc.arg(dst_album_id)
        AND dst.disc_number = src.disc_number
        AND dst.track_number = src.track_number
  );

-- name: ReparentAlbumTracks :exec
-- Album-merge step 4. Move the surviving (non-colliding) src_album tracks onto
-- dst_album. Safe after DeleteCollidingAlbumTracks: the remaining
-- (disc, track_number) tuples are now unique across the two albums.
UPDATE tracks SET album_id = sqlc.arg(dst_album_id)
WHERE album_id = sqlc.arg(src_album_id);

-- name: MergeAlbumRatings :exec
-- Album-merge step 5. Move user_album_ratings from src to dst, keeping the
-- higher rating on collision.
INSERT INTO user_album_ratings (user_id, album_id, rating)
SELECT user_id, sqlc.arg(dst_album_id), rating
FROM user_album_ratings
WHERE user_album_ratings.album_id = sqlc.arg(src_album_id)
ON CONFLICT (user_id, album_id) DO UPDATE
SET rating = GREATEST(user_album_ratings.rating, EXCLUDED.rating),
    updated_at = now();

-- name: MergeAlbumFavorites :exec
-- Album-merge step 6. Move "loved album" entries from src to dst, collapsing
-- dupes via the (user_id, entity_type, entity_id) unique key.
INSERT INTO user_favorites (user_id, entity_type, entity_id)
SELECT user_id, 'album', sqlc.arg(dst_album_id)
FROM user_favorites
WHERE entity_type = 'album' AND user_favorites.entity_id = sqlc.arg(src_album_id)
ON CONFLICT (user_id, entity_type, entity_id) DO NOTHING;

-- name: DeleteAlbumByID :exec
-- Album-merge step 7. Remove the emptied src album once its tracks, ratings,
-- and favorites have moved. CASCADE clears album_centroids and any remaining
-- album-scoped rows.
DELETE FROM albums WHERE id = sqlc.arg(album_id);

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
SELECT rank, provider, provider_rank, title, mbid, recording_entity_id, playcount, listeners, url
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
    asa.provider,
    asa.local_artist_id,
    COALESCE(local_mi.slug, '')::text AS local_slug,
    COALESCE(local_mi.id, 0)::bigint  AS local_media_item_id
FROM artist_similar_artists asa
LEFT JOIN artists      local_a  ON local_a.id  = asa.local_artist_id
LEFT JOIN media_item_cards local_mi ON local_mi.id = local_a.media_item_id
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

-- name: ListTracksByArtist :many
-- Whole-artist batch for the artist detail page — one query instead of one
-- ListTracksByAlbum per album. Ordered so the caller can group by album.
SELECT t.* FROM tracks t
JOIN albums al ON al.id = t.album_id
WHERE al.artist_id = $1
ORDER BY t.album_id ASC, t.disc_number ASC, t.track_number ASC;

-- name: ListTrackFilesByArtist :many
-- Whole-artist batch — see ListTracksByArtist. Quality-descending within each
-- track so callers can keep picking [0] as the primary after grouping.
SELECT tf.*
FROM track_files tf
JOIN library_files lf ON lf.id = tf.library_file_id
JOIN tracks t ON t.id = tf.track_id
JOIN albums al ON al.id = t.album_id
WHERE al.artist_id = $1 AND lf.deleted_at IS NULL
ORDER BY tf.track_id ASC, tf.quality_score DESC, tf.id ASC;
