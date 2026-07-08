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

-- name: UpsertTrackFacetsStub :exec
-- Failure marker: a permanently-broken track (decode error, unreadable file)
-- gets a facets row stamped at the current analyzer version so the pending
-- sweep stops re-picking it every run; bumping AnalyzerVersion invalidates
-- stubs along with real rows. This can't reuse UpsertTrackFacets with
-- zero-value params — pgvector rejects 0-dimension vectors, so that write
-- always errored and broken tracks churned forever. Existing embeddings from
-- a previous successful version are deliberately kept (still useful for
-- similarity) — only the version/timestamp advance.
INSERT INTO track_facets (track_id, analyzer_version)
VALUES ($1, $2)
ON CONFLICT (track_id) DO UPDATE SET
    analyzed_at      = now(),
    analyzer_version = EXCLUDED.analyzer_version;

-- name: GetTrackFacets :one
SELECT * FROM track_facets WHERE track_id = $1;

-- name: GetTrackWaveform :one
SELECT waveform FROM track_facets WHERE track_id = $1;

-- name: NextTrackForAnalysis :one
-- Pick the next track that either has no facets row yet, or whose facets row
-- is older than the configured analyzer_version. Tracks must have a usable
-- primary file (file_path != '') so the analyzer can actually read audio.
-- Skip tracks longer than sqlc.arg(max_duration_seconds) — long-form content
-- (DJ sets, podcasts, lectures) blows the analysis budget for noisy facets.
-- Duration is checked against BOTH sources we have: tracks.duration (from
-- upstream metadata, often 0 for orphan files) and track_files.duration
-- (ffprobe at scan time, ground truth). If either source says "too long",
-- skip. duration=0 means "unknown" and passes — we only reject on positive
-- evidence the track is over the cap, so missing metadata never silently
-- kills a song-length track.
-- Deterministic order (id ASC) so the scheduler resumes predictably.
SELECT t.id, t.title, t.album_id, a.artist_id, t.file_path
FROM tracks t
JOIN albums a ON a.id = t.album_id
LEFT JOIN track_facets tf ON tf.track_id = t.id
WHERE t.file_path != ''
  AND t.duration <= sqlc.arg(max_duration_seconds)::int
  AND NOT EXISTS (
    SELECT 1 FROM track_files tfile
    WHERE tfile.track_id = t.id
      AND tfile.duration > sqlc.arg(max_duration_seconds)::int
  )
  AND (tf.track_id IS NULL OR tf.analyzer_version < sqlc.arg(analyzer_version)::int)
ORDER BY t.id ASC
LIMIT 1;

-- name: GetTrackForAnalysis :one
-- Resolve a specific track for the analyze_track_facets River worker.
-- Same shape as NextTrackForAnalysis plus the resolved artist name so
-- the progress label can read "Artist - Track" instead of a bare title.
-- No eligibility filter — the worker bails on empty file_path itself.
SELECT t.id, t.title, t.album_id, a.artist_id, ar.name AS artist_name, t.file_path
FROM tracks t
JOIN albums  a  ON a.id = t.album_id
JOIN artists ar ON ar.id = a.artist_id
WHERE t.id = $1;

-- name: ListPendingAnalysisTracks :many
-- Fan-out source for kickoff_sonic_analysis. Returns up to `limit_count`
-- track IDs (above the pump's after_id cursor) whose facets row is missing
-- or older than the requested analyzer_version. Mirrors
-- NextTrackForAnalysis' eligibility filter so the kickoff doesn't enqueue
-- jobs the worker would just skip.
SELECT t.id
FROM tracks t
LEFT JOIN track_facets tf ON tf.track_id = t.id
WHERE t.file_path != ''
  AND t.id > sqlc.arg(after_id)::bigint
  AND t.duration <= sqlc.arg(max_duration_seconds)::int
  AND NOT EXISTS (
    SELECT 1 FROM track_files tfile
    WHERE tfile.track_id = t.id
      AND tfile.duration > sqlc.arg(max_duration_seconds)::int
  )
  AND (tf.track_id IS NULL OR tf.analyzer_version < sqlc.arg(analyzer_version)::int)
ORDER BY t.id ASC
LIMIT sqlc.arg(limit_count)::int;

-- name: CountPendingAnalysis :one
-- Mirrors NextTrackForAnalysis' eligibility filter so the Tasks UI counter
-- agrees with what the scheduler will actually pick up.
SELECT count(*)::int FROM tracks t
LEFT JOIN track_facets tf ON tf.track_id = t.id
WHERE t.file_path != ''
  AND t.duration <= sqlc.arg(max_duration_seconds)::int
  AND NOT EXISTS (
    SELECT 1 FROM track_files tfile
    WHERE tfile.track_id = t.id
      AND tfile.duration > sqlc.arg(max_duration_seconds)::int
  )
  AND (tf.track_id IS NULL OR tf.analyzer_version < sqlc.arg(analyzer_version)::int);

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
  JOIN media_item_cards mi ON mi.id = ar.media_item_id
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

-- name: SimilarTracksByTrackRich :many
-- Rich KNN: same cosine ordering as SimilarTracksByTrack but joins
-- album + artist context so the caller gets a self-contained track row
-- (no follow-up lookups). Used by the Instant Radio endpoint.
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
       mi.slug           AS artist_slug,
       (tf.track_embedding <=> $1)::real AS distance
FROM track_facets tf
JOIN tracks      t  ON t.id = tf.track_id
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE tf.track_embedding IS NOT NULL
  AND NOT (tf.track_id = ANY(sqlc.arg(exclude_ids)::bigint[]))
ORDER BY tf.track_embedding <=> $1
LIMIT sqlc.arg(track_limit);

-- Recording identity for browse dedupe: (artist, lowercased title, ~15s
-- duration band). The same recording reappears across releases (original
-- single + each remix single + compilations) with near-identical duration;
-- a live cut, re-record, or different mix that shares the title differs in
-- length and stays a separate row. tracks.recording_mbid would be exact but
-- is populated on a few hundred rows out of 400k, so it can't carry this.
-- Duration-0 copies (file not probed yet) land in their own band until the
-- reprobe pump backfills them.

-- name: CountTracksByMood :one
-- Count tracks scoring above a threshold for one mood tag (e.g. 'mood_happy').
-- Powers the Browse > Moods tile counts. Counts distinct recordings (see
-- recording-identity note above), not track rows, so the tile agrees with
-- the deduped drilldown list.
SELECT count(DISTINCT (al.artist_id, lower(t.title), t.duration / 15))::bigint
FROM track_facets tf
JOIN tracks t  ON t.id = tf.track_id
JOIN albums al ON al.id = t.album_id
WHERE (tf.mood_tags->>sqlc.arg(mood_key)::text)::real > sqlc.arg(threshold)::real
  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id WHERE atf.track_id = t.id AND alf.deleted_at IS NULL);

-- name: ListTracksByMood :many
-- High-scoring tracks for one mood tag, paginated, with album+artist context.
-- Deduped to one row per recording (see recording-identity note above). The
-- surviving copy prefers a known duration, then the highest mood score, then
-- the earliest release.
SELECT * FROM (
    SELECT DISTINCT ON (a.id, lower(t.title), t.duration / 15)
           t.id              AS track_id,
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
           mi.slug           AS artist_slug,
           ((tf.mood_tags->>sqlc.arg(mood_key)::text)::real) AS score
    FROM track_facets tf
    JOIN tracks      t  ON t.id = tf.track_id
    JOIN albums      al ON al.id = t.album_id
    JOIN artists     a  ON a.id  = al.artist_id
    JOIN media_item_cards mi ON mi.id = a.media_item_id
    WHERE (tf.mood_tags->>sqlc.arg(mood_key)::text)::real > sqlc.arg(threshold)::real
      AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
    ORDER BY a.id, lower(t.title), t.duration / 15,
             (tf.mood_tags->>sqlc.arg(mood_key)::text)::real DESC,
             al.year ASC NULLS LAST, t.id ASC
) dedup
ORDER BY score DESC, track_id ASC
LIMIT sqlc.arg(track_limit) OFFSET sqlc.arg(track_offset);

-- name: ListGenreBuckets :many
-- Distinct top-level genres from track_facets.top_genres (array of {name,score})
-- with a track count per genre. Score filter weeds out the long tail of
-- low-confidence labels. We deliberately inline (elem->>'name') / (elem->>'score')
-- twice instead of pushing them through a derived table — sqlc's planner can't
-- resolve LATERAL-aliased columns and would refuse to generate this query.
SELECT (elem->>'name')::text     AS genre_name,
       count(DISTINCT (al.artist_id, lower(t.title), t.duration / 15))::bigint AS track_count
FROM track_facets tf
JOIN tracks t  ON t.id = tf.track_id
JOIN albums al ON al.id = t.album_id
CROSS JOIN LATERAL jsonb_array_elements(tf.top_genres) AS elem
WHERE (elem->>'score')::real >= sqlc.arg(min_score)::real
  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
GROUP BY (elem->>'name')
HAVING count(DISTINCT (al.artist_id, lower(t.title), t.duration / 15)) >= sqlc.arg(min_tracks)::bigint
ORDER BY track_count DESC, (elem->>'name') ASC
LIMIT sqlc.arg(bucket_limit);

-- name: ListTracksByGenre :many
-- Deduped to one row per recording (see recording-identity note above). The
-- surviving copy prefers the highest genre score, then the earliest release.
SELECT * FROM (
    SELECT DISTINCT ON (a.id, lower(t.title), t.duration / 15)
           t.id              AS track_id,
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
           mi.slug           AS artist_slug,
           ((elem->>'score')::real) AS score
    FROM track_facets tf
    JOIN tracks      t  ON t.id = tf.track_id
    JOIN albums      al ON al.id = t.album_id
    JOIN artists     a  ON a.id  = al.artist_id
    JOIN media_item_cards mi ON mi.id = a.media_item_id
    CROSS JOIN LATERAL jsonb_array_elements(tf.top_genres) AS elem
    WHERE (elem->>'name') = sqlc.arg(genre_name)::text
      AND (elem->>'score')::real >= sqlc.arg(min_score)::real
      AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
    ORDER BY a.id, lower(t.title), t.duration / 15,
             (elem->>'score')::real DESC,
             al.year ASC NULLS LAST, t.id ASC
) dedup
ORDER BY score DESC, track_id ASC
LIMIT sqlc.arg(track_limit) OFFSET sqlc.arg(track_offset);

-- name: CountTracksByTempoBand :one
-- Count tracks whose BPM falls in [min, max). Half-open so adjacent bands
-- partition cleanly with no double-counting. Distinct-recording counting for
-- the same reason as CountTracksByMood.
SELECT count(DISTINCT (al.artist_id, lower(t.title), t.duration / 15))::bigint
FROM track_facets tf
JOIN tracks t  ON t.id = tf.track_id
JOIN albums al ON al.id = t.album_id
WHERE tf.bpm IS NOT NULL
  AND tf.bpm >= sqlc.arg(min_bpm)::real
  AND tf.bpm <  sqlc.arg(max_bpm)::real
  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id WHERE atf.track_id = t.id AND alf.deleted_at IS NULL);

-- name: ListTracksByTempoBand :many
-- Deduped to one row per recording (see recording-identity note above). The
-- surviving copy prefers the earliest release.
SELECT * FROM (
    SELECT DISTINCT ON (a.id, lower(t.title), t.duration / 15)
           t.id              AS track_id,
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
           mi.slug           AS artist_slug,
           tf.bpm            AS bpm
    FROM track_facets tf
    JOIN tracks      t  ON t.id = tf.track_id
    JOIN albums      al ON al.id = t.album_id
    JOIN artists     a  ON a.id  = al.artist_id
    JOIN media_item_cards mi ON mi.id = a.media_item_id
    WHERE tf.bpm IS NOT NULL
      AND tf.bpm >= sqlc.arg(min_bpm)::real
      AND tf.bpm <  sqlc.arg(max_bpm)::real
      AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
    ORDER BY a.id, lower(t.title), t.duration / 15,
             al.year ASC NULLS LAST, t.id ASC
) dedup
ORDER BY bpm ASC, track_id ASC
LIMIT sqlc.arg(track_limit) OFFSET sqlc.arg(track_offset);

-- name: MixToTracks :many
-- DJ-style "mix to next track" picker. Constrains the result to tracks that
-- mix smoothly with the seed:
--
--   - BPM within sqlc.arg(bpm_min)..bpm_max (caller picks the tolerance)
--   - Key matches one of the Camelot-compatible (root, mode) pairs passed in
--     `key_codes` as composite ints: code = root*2 + mode. The caller computes
--     these from sonicanalysis.Key.CompatibleKeys().
--   - Excludes the seed and any caller-supplied skip IDs.
--
-- Result ordered by cosine ASC on the seed's track_embedding, so the
-- harmonically-compatible track that's also closest in feel comes first.
-- Returns the rich track row shape (album + artist context + slugs).
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
       mi.slug           AS artist_slug,
       tf.bpm            AS bpm,
       tf.key_root       AS key_root,
       tf.key_mode       AS key_mode,
       (tf.track_embedding <=> sqlc.arg(track_embedding))::real AS distance
FROM track_facets tf
JOIN tracks      t  ON t.id = tf.track_id
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE tf.track_embedding IS NOT NULL
  AND tf.bpm IS NOT NULL
  AND tf.bpm BETWEEN sqlc.arg(bpm_min)::real AND sqlc.arg(bpm_max)::real
  AND tf.key_root IS NOT NULL
  AND tf.key_mode IS NOT NULL
  AND (tf.key_root::int * 2 + tf.key_mode::int) = ANY(sqlc.arg(key_codes)::int[])
  AND NOT (tf.track_id = ANY(sqlc.arg(exclude_ids)::bigint[]))
  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
ORDER BY tf.track_embedding <=> sqlc.arg(track_embedding)
LIMIT sqlc.arg(track_limit);

-- name: PickTrackWithFacetsByArtistSlug :one
-- Picks any track for the given artist that already has facets analyzed.
-- Used as the radio seed when the user starts a station from an artist —
-- "pick something from this artist's catalog to anchor the KNN".
SELECT t.id AS track_id
FROM tracks t
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
JOIN track_facets tf ON tf.track_id = t.id
WHERE mi.slug = $1
  AND tf.track_embedding IS NOT NULL
ORDER BY random()
LIMIT 1;

-- name: PickTrackWithFacetsByArtistID :one
SELECT t.id AS track_id
FROM tracks t
JOIN albums      al ON al.id = t.album_id
JOIN track_facets tf ON tf.track_id = t.id
WHERE al.artist_id = $1
  AND tf.track_embedding IS NOT NULL
ORDER BY random()
LIMIT 1;

-- name: PickTrackWithFacetsByAlbumID :one
SELECT t.id AS track_id
FROM tracks t
JOIN track_facets tf ON tf.track_id = t.id
WHERE t.album_id = $1
  AND tf.track_embedding IS NOT NULL
ORDER BY random()
LIMIT 1;

-- name: SimilarTracksByTextRich :many
-- Rich CLAP text→audio search. Returns the same row shape as
-- SimilarTracksByTrackRich so the FE consumes search results and KNN
-- expansions with one component.
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
       mi.slug           AS artist_slug,
       (tf.text_embedding <=> $1)::real AS distance
FROM track_facets tf
JOIN tracks      t  ON t.id = tf.track_id
JOIN albums      al ON al.id = t.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE tf.text_embedding IS NOT NULL
ORDER BY tf.text_embedding <=> $1
LIMIT sqlc.arg(track_limit);

-- name: SimilarArtists :many
SELECT ar.id, ar.name, ar.media_item_id, mi.slug AS media_slug,
       (ac.sonic_centroid <=> $1)::real AS distance
FROM artist_centroids ac
JOIN artists ar ON ar.id = ac.artist_id
JOIN media_item_cards mi ON mi.id = ar.media_item_id
WHERE ac.artist_id != $2
  AND ac.sonic_centroid IS NOT NULL
ORDER BY ac.sonic_centroid <=> $1
LIMIT $3;

-- name: SimilarAlbums :many
-- artist_slug + album.slug together address the cover endpoint and the album
-- detail page; carrying both saves the FE from joining against an artist row.
SELECT al.id, al.title, al.artist_id, al.slug AS album_slug,
       a.name           AS artist_name,
       mi.slug          AS artist_slug,
       al.cover_path    AS album_cover_path,
       al.year          AS album_year,
       (alc.sonic_centroid <=> $1)::real AS distance
FROM album_centroids alc
JOIN albums      al ON al.id = alc.album_id
JOIN artists     a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE alc.album_id != $2
  AND alc.sonic_centroid IS NOT NULL
ORDER BY alc.sonic_centroid <=> $1
LIMIT $3;

-- name: GetArtistCentroid :one
SELECT * FROM artist_centroids WHERE artist_id = $1;

-- name: GetAlbumCentroid :one
SELECT * FROM album_centroids WHERE album_id = $1;
