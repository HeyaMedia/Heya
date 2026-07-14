-- +goose Up

-- Backdrops are the one ordered image collection. Older scanner paths could
-- leave both the remote and a later local backdrop at sort 0. Normalize every
-- collection deterministically, preferring local art when the library is set
-- to use it, so row order and the editor's Primary badge agree. First stage
-- every row in a negative range so the legacy uniqueness index cannot reject
-- an in-place swap when multiple remote assets have an empty local_path.
WITH ranked AS (
    SELECT
        media_assets.id,
        row_number() OVER (
            PARTITION BY media_assets.media_item_id
            ORDER BY
                CASE
                    WHEN media_assets.source = 'local'
                         AND COALESCE((libraries.settings->>'use_local_data')::boolean, true)
                        THEN 0
                    ELSE 1
                END,
                media_assets.sort_order,
                media_assets.id
        ) - 1 AS wanted_order
    FROM public.media_assets
    JOIN public.media_items ON media_items.id = media_assets.media_item_id
    JOIN public.libraries ON libraries.id = media_items.library_id
    WHERE media_assets.asset_type = 'backdrop'
)
UPDATE public.media_assets AS asset
SET sort_order = (-1000000000 + ranked.wanted_order)::integer
FROM ranked
WHERE asset.id = ranked.id
  AND asset.sort_order IS DISTINCT FROM (-1000000000 + ranked.wanted_order)::integer;

UPDATE public.media_assets
SET sort_order = sort_order + 1000000000
WHERE asset_type = 'backdrop'
  AND sort_order < 0;

-- +goose Down

-- Ordering normalization is intentionally not reversed.
SELECT 1;
