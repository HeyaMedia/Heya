-- +goose Up

ALTER TABLE public.media_items ADD COLUMN IF NOT EXISTS public_id uuid;

UPDATE public.media_items
   SET public_id = gen_random_uuid()
 WHERE public_id IS NULL;

ALTER TABLE public.media_items
  ALTER COLUMN public_id SET DEFAULT gen_random_uuid(),
  ALTER COLUMN public_id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_media_items_public_id
  ON public.media_items USING btree (public_id);

CREATE OR REPLACE VIEW public.media_item_cards AS
 SELECT e.id,
    e.library_id,
    e.media_type,
    COALESCE(p.title, ''::text) AS title,
    COALESCE(p.sort_title, ''::text) AS sort_title,
    COALESCE(p.year, ''::text) AS year,
    COALESCE(p.description, ''::text) AS description,
    COALESCE(p.poster_path, ''::text) AS poster_path,
    COALESCE(p.backdrop_path, ''::text) AS backdrop_path,
    COALESCE(ext.external_ids, '{}'::jsonb) AS external_ids,
    e.slug,
    COALESCE(p.homepage, ''::text) AS homepage,
    COALESCE(p.tagline, ''::text) AS tagline,
    COALESCE(p.original_title, ''::text) AS original_title,
    COALESCE(p.original_language, ''::text) AS original_language,
    COALESCE(p.status, ''::text) AS status,
    e.provider_kind,
    e.heya_slug,
    e.heya_enriched_at,
    e.metadata_refreshed_at,
    e.created_at,
    GREATEST(e.updated_at, COALESCE(p.updated_at, e.updated_at))::timestamp with time zone AS updated_at,
    p.search_vector,
    e.matched_at,
    e.enrichment_status,
    e.base_enriched_at,
    e.people_enriched_at,
    e.extras_enriched_at,
    e.images_enriched_at,
    e.structure_enriched_at,
    e.last_enrich_attempt_at,
    e.last_enrich_error,
    e.field_provenance,
    e.match_confidence,
    e.slug_locked,
    e.public_id
   FROM public.media_items e
     LEFT JOIN public.media_item_profiles p ON p.media_item_id = e.id
     LEFT JOIN LATERAL (
        SELECT jsonb_object_agg(ei.provider, ei.external_id ORDER BY ei.provider) AS external_ids
          FROM public.media_item_external_ids ei
         WHERE ei.media_item_id = e.id
     ) ext ON true;

-- +goose Down

CREATE OR REPLACE VIEW public.media_item_cards AS
 SELECT e.id,
    e.library_id,
    e.media_type,
    COALESCE(p.title, ''::text) AS title,
    COALESCE(p.sort_title, ''::text) AS sort_title,
    COALESCE(p.year, ''::text) AS year,
    COALESCE(p.description, ''::text) AS description,
    COALESCE(p.poster_path, ''::text) AS poster_path,
    COALESCE(p.backdrop_path, ''::text) AS backdrop_path,
    COALESCE(ext.external_ids, '{}'::jsonb) AS external_ids,
    e.slug,
    COALESCE(p.homepage, ''::text) AS homepage,
    COALESCE(p.tagline, ''::text) AS tagline,
    COALESCE(p.original_title, ''::text) AS original_title,
    COALESCE(p.original_language, ''::text) AS original_language,
    COALESCE(p.status, ''::text) AS status,
    e.provider_kind,
    e.heya_slug,
    e.heya_enriched_at,
    e.metadata_refreshed_at,
    e.created_at,
    GREATEST(e.updated_at, COALESCE(p.updated_at, e.updated_at))::timestamp with time zone AS updated_at,
    p.search_vector,
    e.matched_at,
    e.enrichment_status,
    e.base_enriched_at,
    e.people_enriched_at,
    e.extras_enriched_at,
    e.images_enriched_at,
    e.structure_enriched_at,
    e.last_enrich_attempt_at,
    e.last_enrich_error,
    e.field_provenance,
    e.match_confidence,
    e.slug_locked
   FROM public.media_items e
     LEFT JOIN public.media_item_profiles p ON p.media_item_id = e.id
     LEFT JOIN LATERAL (
        SELECT jsonb_object_agg(ei.provider, ei.external_id ORDER BY ei.provider) AS external_ids
          FROM public.media_item_external_ids ei
         WHERE ei.media_item_id = e.id
     ) ext ON true;

DROP INDEX IF EXISTS public.idx_media_items_public_id;

ALTER TABLE public.media_items DROP COLUMN IF EXISTS public_id;
