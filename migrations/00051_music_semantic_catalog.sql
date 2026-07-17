-- +goose Up

-- Canonical recordings known through owned release tracks or bounded provider
-- top-track lists. Rows may have no local file: that is the point of keeping a
-- recommendation catalog separate from the playable library model.
CREATE TABLE public.music_catalog_recordings (
    recording_entity_id uuid PRIMARY KEY,
    recording_mbid text NOT NULL DEFAULT '',
    title text NOT NULL DEFAULT '',
    artist_name text NOT NULL DEFAULT '',
    source_artist_id bigint REFERENCES public.artists(id) ON DELETE SET NULL,
    provider text NOT NULL DEFAULT '',
    provider_rank integer NOT NULL DEFAULT 0,
    provider_url text NOT NULL DEFAULT '',
    playcount bigint NOT NULL DEFAULT 0,
    listeners bigint NOT NULL DEFAULT 0,
    genres text[] NOT NULL DEFAULT '{}',
    tags text[] NOT NULL DEFAULT '{}',
    moods text[] NOT NULL DEFAULT '{}',
    instrumentation text[] NOT NULL DEFAULT '{}',
    vocal_characteristics text[] NOT NULL DEFAULT '{}',
    recording_attributes text[] NOT NULL DEFAULT '{}',
    refreshed_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX music_catalog_recordings_source_artist_idx
    ON public.music_catalog_recordings (source_artist_id, provider_rank);
CREATE INDEX music_catalog_recordings_mbid_idx
    ON public.music_catalog_recordings (recording_mbid)
    WHERE recording_mbid <> '';

-- BGE-M3 semantic vector for the focused musical-character document. This is
-- intentionally independent from track_facets: those vectors describe owned
-- audio, while this table also covers recordings absent from the library.
CREATE TABLE public.music_recording_facets (
    recording_entity_id uuid PRIMARY KEY REFERENCES public.music_catalog_recordings(recording_entity_id) ON DELETE CASCADE,
    text_embedding vector(1024),
    embedder_version integer NOT NULL DEFAULT 1,
    doc_hash text NOT NULL DEFAULT '',
    embedded_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX music_recording_facets_text_emb_hnsw
    ON public.music_recording_facets USING hnsw (text_embedding vector_cosine_ops);

-- +goose Down

DROP INDEX IF EXISTS public.music_recording_facets_text_emb_hnsw;
DROP TABLE IF EXISTS public.music_recording_facets;
DROP INDEX IF EXISTS public.music_catalog_recordings_mbid_idx;
DROP INDEX IF EXISTS public.music_catalog_recordings_source_artist_idx;
DROP TABLE IF EXISTS public.music_catalog_recordings;
