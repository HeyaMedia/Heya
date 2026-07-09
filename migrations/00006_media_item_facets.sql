-- +goose Up

-- Text-embedding facets for movies/TV — the optional ML recommendation engine
-- (HEYA_RECOMMENDATIONS_ML_ENABLED, off by default). One BGE-large-en embedding
-- (1024-dim, cosine) per item's metadata doc; mirrors track_facets for music.
-- embedder_version is a code constant bumped to force a global re-embed after a
-- model/doc change, exactly like sonic's analyzer_version.
CREATE TABLE IF NOT EXISTS public.media_item_facets (
    media_item_id bigint NOT NULL,
    text_embedding vector(1024),
    embedder_version integer DEFAULT 1 NOT NULL,
    embedded_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT media_item_facets_pkey PRIMARY KEY (media_item_id),
    CONSTRAINT media_item_facets_media_item_id_fkey FOREIGN KEY (media_item_id)
        REFERENCES public.media_items(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS media_item_facets_text_emb_hnsw
    ON public.media_item_facets USING hnsw (text_embedding vector_cosine_ops);

-- +goose Down

DROP INDEX IF EXISTS public.media_item_facets_text_emb_hnsw;
DROP TABLE IF EXISTS public.media_item_facets;
