-- +goose Up

-- Canonical metadata identity is deliberately separate from Heya's local
-- relational read models. local_kind + local_id can point at a media item,
-- artist, album, track, person, author, TV season, or TV episode without
-- forcing every table to grow provider-specific columns.
CREATE TABLE public.metadata_entity_bindings (
    local_kind text NOT NULL,
    local_id bigint NOT NULL,
    entity_id uuid NOT NULL,
    entity_kind text NOT NULL,
    schema_version integer NOT NULL DEFAULT 1,
    projection_version bigint NOT NULL DEFAULT 0,
    bound_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT metadata_entity_bindings_pkey PRIMARY KEY (local_kind, local_id),
    CONSTRAINT metadata_entity_bindings_local_kind_check CHECK (local_kind IN (
        'media_item', 'artist', 'album', 'track', 'person', 'author',
        'tv_season', 'tv_episode'
    )),
    CONSTRAINT metadata_entity_bindings_entity_kind_check CHECK (entity_kind IN (
        'movie', 'tv_show', 'anime', 'artist', 'release_group', 'release',
        'recording', 'musical_work', 'book_work', 'book_edition', 'author',
        'person', 'manga', 'manga_volume', 'manga_edition', 'comic_volume',
        'comic_edition', 'season', 'episode'
    ))
);

CREATE INDEX metadata_entity_bindings_entity_idx
    ON public.metadata_entity_bindings (entity_id, entity_kind);

-- Preserve the canonical recording link supplied by the bounded artist
-- top-tracks resource even when the recording is not owned locally.
ALTER TABLE public.artist_top_tracks
    ADD COLUMN recording_entity_id uuid,
    ADD COLUMN provider text NOT NULL DEFAULT '',
    ADD COLUMN provider_rank integer NOT NULL DEFAULT 0;

-- Discovery and resolution are durable Heya workflows. request_key is a
-- stable hash of the canonicalized request/selected resolution, allowing a
-- timed-out or restarted scanner to resume rather than inventing a second
-- upstream workflow or local identity.
CREATE TABLE public.metadata_resolution_workflows (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    request_key text NOT NULL UNIQUE,
    identity_id bigint REFERENCES public.local_media_identities(id) ON DELETE SET NULL,
    kind text NOT NULL,
    query text NOT NULL DEFAULT '',
    hints jsonb NOT NULL DEFAULT '{}'::jsonb,
    selected_resolution jsonb NOT NULL DEFAULT '{}'::jsonb,
    discovery_id uuid,
    job_id bigint,
    entity_id uuid,
    state text NOT NULL DEFAULT 'pending',
    last_error text NOT NULL DEFAULT '',
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    completed_at timestamp with time zone,
    CONSTRAINT metadata_resolution_workflows_state_check CHECK (state IN (
        'pending', 'discovering', 'awaiting_selection', 'resolving',
        'completed', 'failed'
    ))
);

CREATE INDEX metadata_resolution_workflows_resume_idx
    ON public.metadata_resolution_workflows (state, updated_at)
    WHERE state NOT IN ('completed', 'failed');

-- Cursors are committed only after the corresponding local read-model update
-- succeeds. A named consumer leaves room for future independent projections.
CREATE TABLE public.metadata_change_consumers (
    consumer text PRIMARY KEY,
    next_cursor bigint NOT NULL DEFAULT 0,
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);

INSERT INTO public.metadata_change_consumers (consumer, next_cursor)
VALUES ('heya-read-models', 0)
ON CONFLICT (consumer) DO NOTHING;

-- V2 owns refresh cadence and exposes a gap-free change feed. Retire the
-- blind Heya-side age sweep so it cannot create a second refresh policy.
UPDATE public.scheduled_tasks
SET enabled = false,
    description = 'Retired: HeyaMetadata V2 stale-while-revalidate and its durable change cursor now drive metadata refreshes.',
    updated_at = now()
WHERE id = 'refresh_stale_items';

UPDATE public.scheduled_tasks
SET description = 'Fetch community intro, recap, outro, and credits markers directly from TheIntroDB, SkipMeDB, and AniSkip.',
    updated_at = now()
WHERE id = 'scan_media_segments';

-- Community skip segments are media-server behavior, not canonical metadata.
-- Cache each upstream independently so a healthy source is not hidden by a
-- failed one and misses can expire sooner than hits.
CREATE TABLE public.community_segment_cache (
    cache_key text NOT NULL,
    source text NOT NULL,
    candidates jsonb NOT NULL DEFAULT '[]'::jsonb,
    fetch_ok boolean NOT NULL DEFAULT false,
    fetched_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT community_segment_cache_pkey PRIMARY KEY (cache_key, source),
    CONSTRAINT community_segment_cache_source_check CHECK (source IN (
        'theintrodb', 'skipmedb', 'aniskip'
    ))
);

-- AniSkip is addressed by per-season MAL IDs while Heya's episodes are
-- generally TVDB-shaped. Persist the weekly Fribb mapping dump so restarts do
-- not turn every anime lookup into a multi-megabyte download.
CREATE TABLE public.community_segment_anime_map_cache (
    cache_id boolean PRIMARY KEY DEFAULT true CHECK (cache_id),
    entries jsonb NOT NULL DEFAULT '[]'::jsonb,
    fetched_at timestamp with time zone NOT NULL DEFAULT now()
);

-- +goose Down

UPDATE public.scheduled_tasks
SET enabled = true,
    description = 'Re-fetch metadata from heya.media for any media item past its library''s MetadataRefreshDays staleness window. Covers movies, TV, music, and books.',
    updated_at = now()
WHERE id = 'refresh_stale_items';

UPDATE public.scheduled_tasks
SET description = 'Community intro/credits skip markers from heya.media for movie and episode files',
    updated_at = now()
WHERE id = 'scan_media_segments';

DROP TABLE public.community_segment_anime_map_cache;
DROP TABLE public.community_segment_cache;
DROP TABLE public.metadata_change_consumers;
DROP TABLE public.metadata_resolution_workflows;
DROP TABLE public.metadata_entity_bindings;
ALTER TABLE public.artist_top_tracks
    DROP COLUMN provider_rank,
    DROP COLUMN provider,
    DROP COLUMN recording_entity_id;
