-- +goose Up

-- One physical sidecar publication can be visible through duplicate or nested
-- library roots. Keep the publication state global by canonical path, then
-- attach every containing library separately for NFO baselines and cleanup.
--
-- Published and pending signatures deliberately coexist. During an authorized
-- refresh the old published bytes remain valid while the desired replacement
-- is staged/published; a crash at any point can therefore classify either
-- version without ever trusting a third, user-authored value.
CREATE TABLE public.generated_sidecar_publications (
    path                       text PRIMARY KEY CHECK (path <> ''),

    published_size             bigint,
    published_mtime            timestamp with time zone,
    published_sha256           bytea,
    published_at               timestamp with time zone,

    pending_intent_id          uuid,
    pending_size               bigint,
    pending_sha256             bytea,
    pending_staged_path        text,
    pending_previous_path      text,
    pending_lease_expires_at   timestamp with time zone,

    generated_at               timestamp with time zone NOT NULL DEFAULT now(),
    updated_at                 timestamp with time zone NOT NULL DEFAULT now(),
    verified_at                timestamp with time zone NOT NULL DEFAULT now(),

    CONSTRAINT generated_sidecar_publications_has_signature CHECK (
        published_sha256 IS NOT NULL OR pending_sha256 IS NOT NULL
    ),
    CONSTRAINT generated_sidecar_publications_published_complete CHECK (
        (published_size IS NULL AND published_mtime IS NULL AND
         published_sha256 IS NULL AND published_at IS NULL)
        OR
        (published_size IS NOT NULL AND published_size >= 0 AND
         published_mtime IS NOT NULL AND published_sha256 IS NOT NULL AND
         octet_length(published_sha256) = 32 AND published_at IS NOT NULL)
    ),
    CONSTRAINT generated_sidecar_publications_pending_complete CHECK (
        (pending_intent_id IS NULL AND pending_size IS NULL AND
         pending_sha256 IS NULL AND pending_staged_path IS NULL AND
         pending_previous_path IS NULL AND
         pending_lease_expires_at IS NULL)
        OR
        (pending_intent_id IS NOT NULL AND
         pending_size IS NOT NULL AND pending_size >= 0 AND
         pending_sha256 IS NOT NULL AND octet_length(pending_sha256) = 32 AND
         pending_staged_path IS NOT NULL AND pending_staged_path <> '' AND
         pending_previous_path IS NOT NULL AND pending_previous_path <> '' AND
         pending_lease_expires_at IS NOT NULL)
    )
);

CREATE TABLE public.library_generated_sidecars (
    library_id bigint NOT NULL REFERENCES public.libraries(id) ON DELETE CASCADE,
    path       text NOT NULL REFERENCES public.generated_sidecar_publications(path) ON DELETE CASCADE,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT library_generated_sidecars_pkey PRIMARY KEY (library_id, path)
);

CREATE INDEX generated_sidecar_publications_pending_lease_idx
    ON public.generated_sidecar_publications (pending_lease_expires_at, path)
    WHERE pending_intent_id IS NOT NULL;

CREATE INDEX generated_sidecar_publications_verified_idx
    ON public.generated_sidecar_publications (verified_at, path);

CREATE INDEX library_generated_sidecars_path_idx
    ON public.library_generated_sidecars (path);

-- +goose Down

DROP TABLE IF EXISTS public.library_generated_sidecars;
DROP TABLE IF EXISTS public.generated_sidecar_publications;
