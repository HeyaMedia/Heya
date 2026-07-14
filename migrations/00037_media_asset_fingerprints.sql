-- +goose Up

ALTER TABLE public.media_assets
    ADD COLUMN content_hash text NOT NULL DEFAULT '',
    ADD COLUMN visual_hash text NOT NULL DEFAULT '';

-- Collapse exact duplicate candidate URLs immediately. Season posters and
-- episode stills retain separate label scopes because one shared source image
-- can legitimately be assigned to several structural slots. Generic backdrop
-- language/provider labels are provenance only and do not create a new slot.
WITH ranked AS (
    SELECT
        media_assets.id,
        row_number() OVER (
            PARTITION BY
                media_assets.media_item_id,
                media_assets.asset_type,
                media_assets.remote_url,
                CASE
                    WHEN media_assets.asset_type = 'still'
                      OR media_assets.label ~ '^season-[0-9]+$'
                      OR media_assets.label ~ '^s[0-9]+e[0-9]+$'
                        THEN media_assets.label
                    ELSE ''
                END
            ORDER BY
                CASE media_assets.source WHEN 'custom' THEN 0 WHEN 'local' THEN 1 ELSE 2 END,
                (media_assets.width::bigint * media_assets.height::bigint) DESC,
                media_assets.file_size DESC,
                media_assets.sort_order,
                media_assets.id
        ) AS duplicate_rank
    FROM public.media_assets
    WHERE media_assets.remote_url <> ''
)
DELETE FROM public.media_assets AS asset
USING ranked
WHERE asset.id = ranked.id
  AND ranked.duplicate_rank > 1;

-- Deletions can leave backdrop gaps. Stage first to avoid the legacy unique
-- index while rewriting every collection to a deterministic 0..N order.
WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY media_item_id
               ORDER BY sort_order, id
           ) - 1 AS wanted_order
    FROM public.media_assets
    WHERE asset_type = 'backdrop'
)
UPDATE public.media_assets AS asset
SET sort_order = (-1000000000 + ranked.wanted_order)::integer
FROM ranked
WHERE asset.id = ranked.id;

UPDATE public.media_assets
SET sort_order = sort_order + 1000000000
WHERE asset_type = 'backdrop'
  AND sort_order < 0;

CREATE INDEX idx_media_assets_content_hash
    ON public.media_assets (media_item_id, asset_type, content_hash)
    WHERE content_hash <> '';

-- +goose Down

DROP INDEX IF EXISTS public.idx_media_assets_content_hash;

ALTER TABLE public.media_assets
    DROP COLUMN IF EXISTS visual_hash,
    DROP COLUMN IF EXISTS content_hash;
