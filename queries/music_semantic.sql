-- name: UpsertMusicCatalogRecording :exec
INSERT INTO music_catalog_recordings (
    recording_entity_id, recording_mbid, title, artist_name, source_artist_id,
    provider, provider_rank, provider_url, playcount, listeners,
    genres, tags, moods, instrumentation, vocal_characteristics,
    recording_attributes, refreshed_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15,
    $16, now()
)
ON CONFLICT (recording_entity_id) DO UPDATE SET
    recording_mbid = CASE WHEN EXCLUDED.recording_mbid <> '' THEN EXCLUDED.recording_mbid ELSE music_catalog_recordings.recording_mbid END,
    title = CASE WHEN EXCLUDED.title <> '' THEN EXCLUDED.title ELSE music_catalog_recordings.title END,
    artist_name = CASE WHEN EXCLUDED.artist_name <> '' THEN EXCLUDED.artist_name ELSE music_catalog_recordings.artist_name END,
    source_artist_id = COALESCE(EXCLUDED.source_artist_id, music_catalog_recordings.source_artist_id),
    provider = CASE WHEN EXCLUDED.provider <> '' THEN EXCLUDED.provider ELSE music_catalog_recordings.provider END,
    provider_rank = CASE WHEN EXCLUDED.provider_rank > 0 THEN EXCLUDED.provider_rank ELSE music_catalog_recordings.provider_rank END,
    provider_url = CASE WHEN EXCLUDED.provider_url <> '' THEN EXCLUDED.provider_url ELSE music_catalog_recordings.provider_url END,
    playcount = GREATEST(EXCLUDED.playcount, music_catalog_recordings.playcount),
    listeners = GREATEST(EXCLUDED.listeners, music_catalog_recordings.listeners),
    genres = EXCLUDED.genres,
    tags = EXCLUDED.tags,
    moods = EXCLUDED.moods,
    instrumentation = EXCLUDED.instrumentation,
    vocal_characteristics = EXCLUDED.vocal_characteristics,
    recording_attributes = EXCLUDED.recording_attributes,
    refreshed_at = now();

-- name: ListMusicCatalogEmbeddingRows :many
SELECT recording_entity_id, genres, tags, moods, instrumentation,
       vocal_characteristics, recording_attributes
FROM music_catalog_recordings
ORDER BY recording_entity_id;

-- name: ListMusicCatalogHydrationCandidates :many
WITH candidates AS (
    SELECT top_track.recording_entity_id,
           top_track.artist_id AS source_artist_id,
           top_track.rank, top_track.provider, top_track.title,
           top_track.mbid, top_track.playcount, top_track.listeners,
           top_track.url
    FROM artist_top_tracks top_track
    WHERE top_track.recording_entity_id IS NOT NULL
    UNION ALL
    SELECT binding.entity_id,
           album.artist_id,
           0, 'library', track.title, track.recording_mbid, 0, 0, ''
    FROM metadata_entity_bindings binding
    JOIN tracks track ON binding.local_kind = 'track' AND binding.local_id = track.id
    JOIN albums album ON album.id = track.album_id
    WHERE binding.entity_kind = 'recording'
)
SELECT DISTINCT ON (candidate.recording_entity_id)
       candidate.recording_entity_id, candidate.source_artist_id,
       candidate.rank, candidate.provider, candidate.title, candidate.mbid,
       candidate.playcount, candidate.listeners, candidate.url
FROM candidates candidate
LEFT JOIN music_catalog_recordings catalog
  ON catalog.recording_entity_id = candidate.recording_entity_id
WHERE catalog.recording_entity_id IS NULL
ORDER BY candidate.recording_entity_id, candidate.rank ASC
LIMIT $1;

-- name: ListMusicCatalogArtistExpansionCandidates :many
WITH ranked AS (
    SELECT DISTINCT ON (relation.artist_id, relation.mbid)
           relation.artist_id AS source_artist_id,
           relation.mbid AS related_artist_mbid,
           relation.name AS related_artist_name,
           relation.rank
    FROM artist_similar_artists relation
    WHERE relation.mbid <> '' AND relation.local_artist_id IS NULL
    ORDER BY relation.artist_id, relation.mbid, relation.rank
)
SELECT ranked.source_artist_id, ranked.related_artist_mbid,
       ranked.related_artist_name
FROM ranked
LEFT JOIN music_catalog_artist_expansions expansion
  ON expansion.source_artist_id = ranked.source_artist_id
 AND expansion.related_artist_mbid = ranked.related_artist_mbid
WHERE expansion.source_artist_id IS NULL
   OR expansion.hydrated_at < now() - interval '30 days'
ORDER BY (expansion.source_artist_id IS NULL) DESC, ranked.rank, ranked.source_artist_id
LIMIT $1;

-- name: MarkMusicCatalogArtistExpansion :exec
INSERT INTO music_catalog_artist_expansions
  (source_artist_id, related_artist_mbid, related_artist_name, hydrated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (source_artist_id, related_artist_mbid) DO UPDATE SET
  related_artist_name = EXCLUDED.related_artist_name,
  hydrated_at = now();

-- name: CountMusicCatalogRecordings :one
SELECT count(*)::int FROM music_catalog_recordings
WHERE cardinality(genres) + cardinality(tags) + cardinality(moods) +
      cardinality(instrumentation) + cardinality(vocal_characteristics) +
      cardinality(recording_attributes) > 0;

-- name: CountEmbeddedMusicCatalogRecordings :one
SELECT count(*)::int
FROM music_recording_facets facet
JOIN music_catalog_recordings recording USING (recording_entity_id)
WHERE facet.text_embedding IS NOT NULL
  AND facet.embedder_version >= $1
  AND cardinality(recording.genres) + cardinality(recording.tags) + cardinality(recording.moods) +
      cardinality(recording.instrumentation) + cardinality(recording.vocal_characteristics) +
      cardinality(recording.recording_attributes) > 0;
