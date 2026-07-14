-- +goose Up

-- Poster/logo/banner/etc. have one primary slot. Older scans could insert a
-- local sidecar and a HeyaMetadata image at the same sort order, leaving the
-- selected image dependent on PostgreSQL row order. Keep local data when it
-- exists, otherwise retain the explicitly first remote row.
WITH ranked AS (
    SELECT
        media_assets.id,
        row_number() OVER (
            PARTITION BY media_assets.media_item_id, media_assets.asset_type
            ORDER BY
                CASE
                    WHEN media_assets.source = 'local'
                         AND COALESCE((libraries.settings->>'use_local_data')::boolean, true)
                        THEN 0
                    WHEN media_assets.source <> 'local' THEN 1
                    ELSE 2
                END,
                media_assets.sort_order,
                media_assets.id
        ) AS rank
    FROM public.media_assets
    JOIN public.media_items ON media_items.id = media_assets.media_item_id
    JOIN public.libraries ON libraries.id = media_items.library_id
    WHERE media_assets.label = ''
      AND media_assets.asset_type IN ('poster', 'logo', 'art', 'banner', 'thumb', 'disc', 'clearart')
)
DELETE FROM public.media_assets AS asset
USING ranked
WHERE asset.id = ranked.id
  AND ranked.rank > 1;

UPDATE public.media_assets
SET sort_order = 0
WHERE label = ''
  AND asset_type IN ('poster', 'logo', 'art', 'banner', 'thumb', 'disc', 'clearart')
  AND sort_order <> 0;

CREATE UNIQUE INDEX idx_media_assets_single_primary
    ON public.media_assets (media_item_id, asset_type)
    WHERE label = ''
      AND asset_type IN ('poster', 'logo', 'art', 'banner', 'thumb', 'disc', 'clearart');

-- +goose Down

DROP INDEX IF EXISTS public.idx_media_assets_single_primary;
