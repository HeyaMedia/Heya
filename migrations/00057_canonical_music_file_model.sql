-- +goose Up
-- +goose StatementBegin

-- Freeze the affected graph while its IDs are redirected. Reads continue,
-- but a rating/play event/matcher write cannot land between the copy and the
-- stale-track delete and disappear through a cascade.
LOCK TABLE tracks, track_files, library_files, albums, artists, media_items,
    play_events, play_queue_items, external_listens, user_track_ratings,
    user_album_ratings, user_artist_ratings, user_playlist_tracks, track_facets, metadata_entity_bindings,
    metadata_projection_states
IN SHARE MODE;

-- Lyrics sidecars were the only useful data still stored on tracks. Resolve
-- them through the old physical path before removing that path. This is an
-- exact path-to-library_file mapping, not a probabilistic rematch.
UPDATE track_files tf
SET lyrics_path = t.lyrics_path
FROM tracks t
WHERE t.lyrics_path <> ''
  AND tf.library_file_id = t.library_file_id
  AND tf.lyrics_path = '';

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM tracks t
        WHERE t.lyrics_path <> ''
          AND NOT EXISTS (
              SELECT 1
              FROM track_files tf
              WHERE tf.library_file_id = t.library_file_id
                AND tf.lyrics_path = t.lyrics_path
          )
    ) THEN
        RAISE EXCEPTION 'cannot remove tracks.lyrics_path: unresolved lyric sidecars remain';
    END IF;
END $$;

-- Catch any fingerprint written by an older binary between migration 00056's
-- backfill and this contraction. Existing file-level rows remain authoritative.
INSERT INTO library_file_fingerprints (
    library_file_id,
    algorithm,
    fingerprint,
    fingerprint_duration_secs,
    source_duration_secs,
    source_size,
    source_mtime,
    fingerprinted_at
)
SELECT tf.library_file_id,
       tf.chromaprint_algorithm,
       tf.chromaprint,
       tf.chromaprint_duration_secs,
       GREATEST(tf.duration, tf.chromaprint_duration_secs, 1),
       lf.size,
       lf.mtime,
       tf.fingerprinted_at
FROM track_files tf
JOIN library_files lf ON lf.id = tf.library_file_id
WHERE tf.chromaprint IS NOT NULL
  AND tf.chromaprint <> ''
  AND tf.chromaprint_algorithm IS NOT NULL
  AND tf.chromaprint_duration_secs IS NOT NULL
  AND tf.fingerprinted_at IS NOT NULL
ON CONFLICT (library_file_id) DO NOTHING;

-- Rows which only own a file through tracks.library_file_id are duplicate
-- logical tracks left by the pre-track_files matcher. The same physical file
-- already has exactly one canonical track_files owner. Preserve every user or
-- analysis edge by redirecting it before deleting the stale identity.
CREATE TEMP TABLE stale_music_track_redirects ON COMMIT DROP AS
SELECT t.id AS stale_track_id,
       canonical.track_id AS canonical_track_id,
       t.album_id AS stale_album_id
FROM tracks t
JOIN track_files canonical ON canonical.library_file_id = t.library_file_id
WHERE t.library_file_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM track_files own WHERE own.track_id = t.id
  );

CREATE UNIQUE INDEX stale_music_track_redirects_track
    ON stale_music_track_redirects (stale_track_id);
CREATE INDEX stale_music_track_redirects_canonical
    ON stale_music_track_redirects (canonical_track_id);

CREATE TEMP TABLE stale_music_artist_media ON COMMIT DROP AS
SELECT DISTINCT a.artist_id, ar.media_item_id
FROM stale_music_track_redirects redirect
JOIN albums a ON a.id = redirect.stale_album_id
JOIN artists ar ON ar.id = a.artist_id;

UPDATE play_events event
SET track_id = redirect.canonical_track_id
FROM stale_music_track_redirects redirect
WHERE event.track_id = redirect.stale_track_id;

UPDATE play_queue_items item
SET track_id = redirect.canonical_track_id
FROM stale_music_track_redirects redirect
WHERE item.track_id = redirect.stale_track_id;

UPDATE external_listens listen
SET matched_track_id = redirect.canonical_track_id
FROM stale_music_track_redirects redirect
WHERE listen.matched_track_id = redirect.stale_track_id;

INSERT INTO user_track_ratings (user_id, track_id, rating, created_at, updated_at)
SELECT DISTINCT ON (rating.user_id, redirect.canonical_track_id)
       rating.user_id,
       redirect.canonical_track_id,
       rating.rating,
       rating.created_at,
       rating.updated_at
FROM user_track_ratings rating
JOIN stale_music_track_redirects redirect ON redirect.stale_track_id = rating.track_id
ORDER BY rating.user_id, redirect.canonical_track_id, rating.updated_at DESC
ON CONFLICT (user_id, track_id) DO UPDATE
SET rating = CASE
        WHEN EXCLUDED.updated_at > user_track_ratings.updated_at THEN EXCLUDED.rating
        ELSE user_track_ratings.rating
    END,
    created_at = LEAST(user_track_ratings.created_at, EXCLUDED.created_at),
    updated_at = GREATEST(user_track_ratings.updated_at, EXCLUDED.updated_at);

INSERT INTO user_playlist_tracks (playlist_id, track_id, position, added_at)
SELECT DISTINCT ON (entry.playlist_id, redirect.canonical_track_id)
       entry.playlist_id,
       redirect.canonical_track_id,
       entry.position,
       entry.added_at
FROM user_playlist_tracks entry
JOIN stale_music_track_redirects redirect ON redirect.stale_track_id = entry.track_id
ORDER BY entry.playlist_id, redirect.canonical_track_id, entry.position, entry.added_at
ON CONFLICT (playlist_id, track_id) DO UPDATE
SET position = LEAST(user_playlist_tracks.position, EXCLUDED.position),
    added_at = LEAST(user_playlist_tracks.added_at, EXCLUDED.added_at);

INSERT INTO track_facets (
    track_id, track_embedding, artist_embedding, release_embedding,
    text_embedding, bpm, bpm_confidence, key_root, key_mode, key_clarity,
    top_genres, mood_tags, waveform, analyzed_at, analyzer_version
)
SELECT DISTINCT ON (redirect.canonical_track_id)
       redirect.canonical_track_id,
       facet.track_embedding,
       facet.artist_embedding,
       facet.release_embedding,
       facet.text_embedding,
       facet.bpm,
       facet.bpm_confidence,
       facet.key_root,
       facet.key_mode,
       facet.key_clarity,
       facet.top_genres,
       facet.mood_tags,
       facet.waveform,
       facet.analyzed_at,
       facet.analyzer_version
FROM track_facets facet
JOIN stale_music_track_redirects redirect ON redirect.stale_track_id = facet.track_id
ORDER BY redirect.canonical_track_id, facet.analyzer_version DESC, facet.analyzed_at DESC
ON CONFLICT (track_id) DO UPDATE
SET track_embedding = EXCLUDED.track_embedding,
    artist_embedding = EXCLUDED.artist_embedding,
    release_embedding = EXCLUDED.release_embedding,
    text_embedding = EXCLUDED.text_embedding,
    bpm = EXCLUDED.bpm,
    bpm_confidence = EXCLUDED.bpm_confidence,
    key_root = EXCLUDED.key_root,
    key_mode = EXCLUDED.key_mode,
    key_clarity = EXCLUDED.key_clarity,
    top_genres = EXCLUDED.top_genres,
    mood_tags = EXCLUDED.mood_tags,
    waveform = EXCLUDED.waveform,
    analyzed_at = EXCLUDED.analyzed_at,
    analyzer_version = EXCLUDED.analyzer_version
WHERE (EXCLUDED.analyzer_version, EXCLUDED.analyzed_at)
    > (track_facets.analyzer_version, track_facets.analyzed_at);

INSERT INTO metadata_entity_bindings (
    local_kind, local_id, entity_id, entity_kind, schema_version,
    projection_version, bound_at, updated_at
)
SELECT DISTINCT ON (redirect.canonical_track_id)
       'track',
       redirect.canonical_track_id,
       binding.entity_id,
       binding.entity_kind,
       binding.schema_version,
       binding.projection_version,
       binding.bound_at,
       binding.updated_at
FROM metadata_entity_bindings binding
JOIN stale_music_track_redirects redirect ON redirect.stale_track_id = binding.local_id
WHERE binding.local_kind = 'track'
ORDER BY redirect.canonical_track_id, binding.projection_version DESC, binding.updated_at DESC
ON CONFLICT (local_kind, local_id) DO UPDATE
SET entity_id = EXCLUDED.entity_id,
    entity_kind = EXCLUDED.entity_kind,
    schema_version = EXCLUDED.schema_version,
    projection_version = EXCLUDED.projection_version,
    bound_at = EXCLUDED.bound_at,
    updated_at = EXCLUDED.updated_at
WHERE (EXCLUDED.projection_version, EXCLUDED.updated_at)
    > (metadata_entity_bindings.projection_version, metadata_entity_bindings.updated_at);

INSERT INTO metadata_projection_states (
    local_kind, local_id, scope, entity_id, entity_kind,
    projection_version, applied_at
)
SELECT DISTINCT ON (redirect.canonical_track_id, state.scope)
       'track',
       redirect.canonical_track_id,
       state.scope,
       state.entity_id,
       state.entity_kind,
       state.projection_version,
       state.applied_at
FROM metadata_projection_states state
JOIN stale_music_track_redirects redirect ON redirect.stale_track_id = state.local_id
JOIN metadata_entity_bindings canonical_binding
  ON canonical_binding.local_kind = 'track'
 AND canonical_binding.local_id = redirect.canonical_track_id
 AND canonical_binding.entity_id = state.entity_id
 AND canonical_binding.entity_kind = state.entity_kind
WHERE state.local_kind = 'track'
ORDER BY redirect.canonical_track_id, state.scope, state.projection_version DESC, state.applied_at DESC
ON CONFLICT (local_kind, local_id, scope) DO UPDATE
SET entity_id = EXCLUDED.entity_id,
    entity_kind = EXCLUDED.entity_kind,
    projection_version = EXCLUDED.projection_version,
    applied_at = EXCLUDED.applied_at
WHERE (EXCLUDED.projection_version, EXCLUDED.applied_at)
    > (metadata_projection_states.projection_version, metadata_projection_states.applied_at);

-- Generic metadata bindings have no FK to tracks, so remove their stale keys
-- explicitly. Typed user/analysis rows cascade after their canonical copies
-- above have landed.
DELETE FROM metadata_entity_bindings binding
USING stale_music_track_redirects redirect
WHERE binding.local_kind = 'track'
  AND binding.local_id = redirect.stale_track_id;

DELETE FROM tracks track
USING stale_music_track_redirects redirect
WHERE track.id = redirect.stale_track_id;

-- A rated duplicate album is retained as a trackless catalog row rather than
-- silently discarding user intent. Production has no such rows today, but the
-- guard makes the migration safe for other databases as well.
DELETE FROM metadata_entity_bindings binding
USING (SELECT DISTINCT stale_album_id FROM stale_music_track_redirects) touched
WHERE binding.local_kind = 'album'
  AND binding.local_id = touched.stale_album_id
  AND NOT EXISTS (SELECT 1 FROM tracks track WHERE track.album_id = touched.stale_album_id)
  AND NOT EXISTS (SELECT 1 FROM user_album_ratings rating WHERE rating.album_id = touched.stale_album_id);

DELETE FROM albums album
USING (SELECT DISTINCT stale_album_id FROM stale_music_track_redirects) touched
WHERE album.id = touched.stale_album_id
  AND NOT EXISTS (SELECT 1 FROM tracks track WHERE track.album_id = album.id)
  AND NOT EXISTS (SELECT 1 FROM user_album_ratings rating WHERE rating.album_id = album.id);

-- A stale-only artist is also an ingest duplicate. Delete its media_item only
-- when it has no albums and no live file; normal FK cascades remove the artist.
-- As with albums, a user rating keeps the now-fileless catalog identity.
DELETE FROM metadata_entity_bindings binding
USING stale_music_artist_media stale
WHERE (
        (binding.local_kind = 'artist' AND binding.local_id = stale.artist_id)
        OR (binding.local_kind = 'media_item' AND binding.local_id = stale.media_item_id)
      )
  AND NOT EXISTS (SELECT 1 FROM albums album WHERE album.artist_id = stale.artist_id)
  AND NOT EXISTS (SELECT 1 FROM user_artist_ratings rating WHERE rating.artist_id = stale.artist_id)
  AND NOT EXISTS (
      SELECT 1 FROM library_files file
      WHERE file.media_item_id = stale.media_item_id AND file.deleted_at IS NULL
  );

DELETE FROM media_items item
USING stale_music_artist_media stale
WHERE item.id = stale.media_item_id
  AND NOT EXISTS (
      SELECT 1 FROM artists artist
      JOIN albums album ON album.artist_id = artist.id
      WHERE artist.media_item_id = item.id
  )
  AND NOT EXISTS (
      SELECT 1 FROM user_artist_ratings rating WHERE rating.artist_id = stale.artist_id
  )
  AND NOT EXISTS (
      SELECT 1 FROM library_files file
      WHERE file.media_item_id = item.id AND file.deleted_at IS NULL
  );

DROP TABLE stale_music_artist_media;
DROP TABLE stale_music_track_redirects;

-- tracks is the logical recording; track_files is the sole ownership edge to
-- a physical library_file and the sole home of per-file sidecars.
ALTER TABLE tracks
    DROP COLUMN file_path,
    DROP COLUMN lyrics_path,
    DROP COLUMN library_file_id;

-- Fingerprints belong to the physical library file and must also exist before
-- a track is matched, so the compatibility mirror has no remaining purpose.
ALTER TABLE track_files
    DROP COLUMN chromaprint,
    DROP COLUMN chromaprint_algorithm,
    DROP COLUMN chromaprint_duration_secs,
    DROP COLUMN fingerprinted_at;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE tracks
    ADD COLUMN file_path text NOT NULL DEFAULT '',
    ADD COLUMN lyrics_path text NOT NULL DEFAULT '',
    ADD COLUMN library_file_id bigint REFERENCES library_files(id) ON DELETE CASCADE;

CREATE INDEX idx_tracks_library_file_id
    ON tracks (library_file_id)
    WHERE library_file_id IS NOT NULL;

WITH primary_files AS (
    SELECT DISTINCT ON (tf.track_id)
           tf.track_id, tf.library_file_id, lf.path, tf.lyrics_path
    FROM track_files tf
    JOIN library_files lf ON lf.id = tf.library_file_id
    WHERE lf.deleted_at IS NULL
    ORDER BY tf.track_id, tf.quality_score DESC, tf.id ASC
)
UPDATE tracks t
SET file_path = p.path,
    lyrics_path = p.lyrics_path,
    library_file_id = p.library_file_id
FROM primary_files p
WHERE p.track_id = t.id;

ALTER TABLE track_files
    ADD COLUMN chromaprint text,
    ADD COLUMN chromaprint_algorithm smallint,
    ADD COLUMN chromaprint_duration_secs integer,
    ADD COLUMN fingerprinted_at timestamp with time zone;

UPDATE track_files tf
SET chromaprint = fp.fingerprint,
    chromaprint_algorithm = fp.algorithm,
    chromaprint_duration_secs = fp.fingerprint_duration_secs,
    fingerprinted_at = fp.fingerprinted_at
FROM library_file_fingerprints fp
WHERE fp.library_file_id = tf.library_file_id;

-- +goose StatementEnd
