-- +goose Up

-- Chromaprints are evidence about physical audio files, not matched tracks.
-- Keeping the durable copy on library_files lets an unmatched scanner entity
-- use AcoustID before tracks/albums/artists have been materialized.
CREATE TABLE public.library_file_fingerprints (
    library_file_id          bigint PRIMARY KEY REFERENCES public.library_files(id) ON DELETE CASCADE,
    algorithm                smallint NOT NULL,
    fingerprint              text NOT NULL,
    fingerprint_duration_secs integer NOT NULL,
    source_duration_secs     integer NOT NULL,
    source_size              bigint NOT NULL,
    source_mtime             timestamp with time zone,
    fingerprinted_at         timestamp with time zone NOT NULL DEFAULT now(),
    updated_at               timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT library_file_fingerprints_nonempty CHECK (fingerprint <> ''),
    CONSTRAINT library_file_fingerprints_durations CHECK (
        fingerprint_duration_secs > 0 AND source_duration_secs > 0
    )
);

-- Provider-neutral cached lookups derived from a file fingerprint. AcoustID is
-- the first provider; a future HeyaMetadata/local-corpus matcher can coexist
-- under a different provider without changing the scanner contract.
CREATE TABLE public.library_file_fingerprint_lookups (
    library_file_id bigint NOT NULL REFERENCES public.library_files(id) ON DELETE CASCADE,
    provider        text NOT NULL,
    evidence_key    text NOT NULL,
    state           text NOT NULL,
    results         jsonb NOT NULL DEFAULT '[]'::jsonb,
    error_message   text NOT NULL DEFAULT '',
    observed_at     timestamp with time zone NOT NULL DEFAULT now(),
    retry_after     timestamp with time zone,
    updated_at      timestamp with time zone NOT NULL DEFAULT now(),
    PRIMARY KEY (library_file_id, provider),
    CONSTRAINT library_file_fingerprint_lookups_state
        CHECK (state IN ('matched', 'no_match', 'failed'))
);

-- Preserve the hundreds of thousands of fingerprints already generated on
-- matched track_files. Future workers write both locations during the
-- compatibility period; consumers should read this file-level table.
INSERT INTO public.library_file_fingerprints (
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
FROM public.track_files tf
JOIN public.library_files lf ON lf.id = tf.library_file_id
WHERE tf.chromaprint IS NOT NULL
  AND tf.chromaprint <> ''
  AND tf.chromaprint_algorithm IS NOT NULL
  AND tf.chromaprint_duration_secs IS NOT NULL
  AND tf.fingerprinted_at IS NOT NULL
ON CONFLICT (library_file_id) DO NOTHING;

-- +goose Down

DROP TABLE IF EXISTS public.library_file_fingerprint_lookups;
DROP TABLE IF EXISTS public.library_file_fingerprints;
