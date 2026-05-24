-- name: UpsertTrackFacets :exec
-- Atomic write of all per-track facets after a successful Analyze() call.
-- analyzed_at + analyzer_version are set to (now(), :current_version) so the
-- "next track to analyze" query naturally skips this row until the version
-- bumps. Loudness is NOT written here — it lives in track_files (canonical
-- pipeline) and is joined on read.
INSERT INTO track_facets (
    track_id,
    track_embedding, artist_embedding, release_embedding, text_embedding,
    bpm, bpm_confidence,
    key_root, key_mode, key_clarity,
    top_genres, mood_tags,
    waveform,
    analyzer_version
) VALUES (
    $1,
    $2, $3, $4, $5,
    $6, $7,
    $8, $9, $10,
    $11, $12,
    $13,
    $14
)
ON CONFLICT (track_id) DO UPDATE SET
    track_embedding   = EXCLUDED.track_embedding,
    artist_embedding  = EXCLUDED.artist_embedding,
    release_embedding = EXCLUDED.release_embedding,
    text_embedding    = EXCLUDED.text_embedding,
    bpm               = EXCLUDED.bpm,
    bpm_confidence    = EXCLUDED.bpm_confidence,
    key_root          = EXCLUDED.key_root,
    key_mode          = EXCLUDED.key_mode,
    key_clarity       = EXCLUDED.key_clarity,
    top_genres        = EXCLUDED.top_genres,
    mood_tags         = EXCLUDED.mood_tags,
    waveform          = EXCLUDED.waveform,
    analyzed_at       = now(),
    analyzer_version  = EXCLUDED.analyzer_version;

-- name: GetTrackFacets :one
SELECT * FROM track_facets WHERE track_id = $1;

-- name: GetTrackWaveform :one
SELECT waveform FROM track_facets WHERE track_id = $1;

-- name: NextTrackForAnalysis :one
-- Pick the next track that either has no facets row yet, or whose facets row
-- is older than the configured analyzer_version. Tracks must have a usable
-- primary file (file_path != '') so the analyzer can actually read audio.
-- Deterministic order (id ASC) so the scheduler resumes predictably.
SELECT t.id, t.title, t.album_id, a.artist_id, t.file_path
FROM tracks t
JOIN albums a ON a.id = t.album_id
LEFT JOIN track_facets tf ON tf.track_id = t.id
WHERE t.file_path != ''
  AND (tf.track_id IS NULL OR tf.analyzer_version < $1)
ORDER BY t.id ASC
LIMIT 1;

-- name: CountPendingAnalysis :one
SELECT count(*)::int FROM tracks t
LEFT JOIN track_facets tf ON tf.track_id = t.id
WHERE t.file_path != ''
  AND (tf.track_id IS NULL OR tf.analyzer_version < $1);

-- name: CountAnalyzedTracks :one
SELECT count(*)::int FROM track_facets WHERE analyzer_version >= $1;

-- name: ResetTrackFacetsVersion :exec
-- Force re-analysis library-wide by lowering analyzer_version to 0. The
-- scheduler will then re-process every track on its next pass. Used by
-- `heya analyze reset`.
UPDATE track_facets SET analyzer_version = 0;

-- name: ResetTrackFacetsVersionForLibrary :exec
-- Same as above but limited to a single library.
UPDATE track_facets tf
   SET analyzer_version = 0
  FROM tracks t
  JOIN albums a ON a.id = t.album_id
  JOIN artists ar ON ar.id = a.artist_id
  JOIN media_items mi ON mi.id = ar.media_item_id
 WHERE tf.track_id = t.id AND mi.library_id = $1;

-- name: RefreshArtistCentroid :exec
-- Recompute the centroid for one artist as the mean of its tracks'
-- artist_embedding (sonic) and text_embedding (CLAP). Skips tracks whose
-- analyzer hasn't populated the vectors yet. UPSERTs the result.
INSERT INTO artist_centroids (artist_id, sonic_centroid, text_centroid, track_count, updated_at)
SELECT
    ar.id,
    AVG(tf.artist_embedding)::vector(512),
    AVG(tf.text_embedding)::vector(512),
    count(*)::int,
    now()
FROM artists ar
JOIN albums  a  ON a.artist_id  = ar.id
JOIN tracks  t  ON t.album_id   = a.id
JOIN track_facets tf ON tf.track_id = t.id
WHERE ar.id = $1
  AND tf.artist_embedding IS NOT NULL
  AND tf.text_embedding   IS NOT NULL
GROUP BY ar.id
ON CONFLICT (artist_id) DO UPDATE SET
    sonic_centroid = EXCLUDED.sonic_centroid,
    text_centroid  = EXCLUDED.text_centroid,
    track_count    = EXCLUDED.track_count,
    updated_at     = now();

-- name: RefreshAlbumCentroid :exec
INSERT INTO album_centroids (album_id, sonic_centroid, text_centroid, track_count, updated_at)
SELECT
    a.id,
    AVG(tf.release_embedding)::vector(512),
    AVG(tf.text_embedding)::vector(512),
    count(*)::int,
    now()
FROM albums a
JOIN tracks t ON t.album_id = a.id
JOIN track_facets tf ON tf.track_id = t.id
WHERE a.id = $1
  AND tf.release_embedding IS NOT NULL
  AND tf.text_embedding    IS NOT NULL
GROUP BY a.id
ON CONFLICT (album_id) DO UPDATE SET
    sonic_centroid = EXCLUDED.sonic_centroid,
    text_centroid  = EXCLUDED.text_centroid,
    track_count    = EXCLUDED.track_count,
    updated_at     = now();

-- name: SimilarTracksByTrack :many
-- Top-N tracks closest to the seed track by cosine on track_embedding.
-- Excludes the seed itself. Caller passes the seed's vector after fetching
-- it; this avoids an extra subquery + lets the planner use the HNSW index
-- on the literal parameter.
SELECT t.id, t.title, t.album_id, a.artist_id, t.file_path,
       (tf.track_embedding <=> $1)::real AS distance
FROM track_facets tf
JOIN tracks t ON t.id = tf.track_id
JOIN albums a ON a.id = t.album_id
WHERE tf.track_id != $2
  AND tf.track_embedding IS NOT NULL
ORDER BY tf.track_embedding <=> $1
LIMIT $3;

-- name: SimilarTracksByText :many
-- Top-N tracks closest to a CLAP text embedding (caller computes the text
-- vector via TextSearcher). Searches in the audio↔text shared CLAP space.
SELECT t.id, t.title, t.album_id, a.artist_id, t.file_path,
       (tf.text_embedding <=> $1)::real AS distance
FROM track_facets tf
JOIN tracks t ON t.id = tf.track_id
JOIN albums a ON a.id = t.album_id
WHERE tf.text_embedding IS NOT NULL
ORDER BY tf.text_embedding <=> $1
LIMIT $2;

-- name: SimilarArtists :many
SELECT ar.id, ar.name, ar.media_item_id, mi.slug AS media_slug,
       (ac.sonic_centroid <=> $1)::real AS distance
FROM artist_centroids ac
JOIN artists ar ON ar.id = ac.artist_id
JOIN media_items mi ON mi.id = ar.media_item_id
WHERE ac.artist_id != $2
  AND ac.sonic_centroid IS NOT NULL
ORDER BY ac.sonic_centroid <=> $1
LIMIT $3;

-- name: SimilarAlbums :many
SELECT al.id, al.title, al.artist_id, al.slug,
       (alc.sonic_centroid <=> $1)::real AS distance
FROM album_centroids alc
JOIN albums al ON al.id = alc.album_id
WHERE alc.album_id != $2
  AND alc.sonic_centroid IS NOT NULL
ORDER BY alc.sonic_centroid <=> $1
LIMIT $3;

-- name: GetArtistCentroid :one
SELECT * FROM artist_centroids WHERE artist_id = $1;

-- name: GetAlbumCentroid :one
SELECT * FROM album_centroids WHERE album_id = $1;
