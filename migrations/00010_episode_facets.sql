-- +goose Up

-- Text-embedding facets for TV/anime EPISODES — same engine and model as
-- media_item_facets, but one embedding per episode overview. Episode overviews
-- carry the plot-specific text the spoiler-safe series blurb omits (character
-- names, arcs, twists), so semantic search can match "the one where X happens"
-- asks; hits resolve up to their series via tv_seasons/tv_series.
CREATE TABLE IF NOT EXISTS public.episode_facets (
    episode_id bigint NOT NULL,
    text_embedding vector(1024),
    embedder_version integer DEFAULT 1 NOT NULL,
    embedded_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT episode_facets_pkey PRIMARY KEY (episode_id),
    CONSTRAINT episode_facets_episode_id_fkey FOREIGN KEY (episode_id)
        REFERENCES public.tv_episodes(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS episode_facets_text_emb_hnsw
    ON public.episode_facets USING hnsw (text_embedding vector_cosine_ops);

-- +goose Down

DROP INDEX IF EXISTS public.episode_facets_text_emb_hnsw;
DROP TABLE IF EXISTS public.episode_facets;
