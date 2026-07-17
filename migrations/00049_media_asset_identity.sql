-- +goose Up

-- A backdrop's position is presentation state, not identity. Older scanner
-- paths included sort_order in the only uniqueness constraint, so every scan
-- could append another row for the same local file or upstream URL. Collapse
-- those exact identities before installing constraints that prevent the rows
-- from returning.
--
-- Season posters and episode stills retain their structural label scope: the
-- same source image may legitimately be assigned to more than one slot.
WITH ranked AS (
    SELECT
        media_assets.id,
        row_number() OVER duplicate AS duplicate_rank
    FROM public.media_assets
    WHERE media_assets.local_path <> ''
    WINDOW duplicate AS (
        PARTITION BY
            media_assets.media_item_id,
            media_assets.asset_type,
            media_assets.local_path,
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
    )
)
DELETE FROM public.media_assets AS asset
USING ranked
WHERE asset.id = ranked.id
  AND ranked.duplicate_rank > 1;

-- A materialized row wins over a pending copy of the same URL. Pending losers
-- have no filesystem state and can be removed immediately.
WITH ranked AS (
    SELECT
        media_assets.id,
        row_number() OVER duplicate AS duplicate_rank
    FROM public.media_assets
    WHERE media_assets.remote_url <> ''
    WINDOW duplicate AS (
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
            CASE WHEN media_assets.local_path <> '' THEN 0 ELSE 1 END,
            CASE media_assets.source WHEN 'custom' THEN 0 WHEN 'local' THEN 1 ELSE 2 END,
            (media_assets.width::bigint * media_assets.height::bigint) DESC,
            media_assets.file_size DESC,
            media_assets.sort_order,
            media_assets.id
    )
)
DELETE FROM public.media_assets AS asset
USING ranked
WHERE asset.id = ranked.id
  AND ranked.duplicate_rank > 1
  AND asset.local_path = '';

-- When an old release managed to materialize the same URL to two different
-- cache filenames, retain both rows for the startup fingerprint backfill
-- instead of orphaning a file from SQL. Clearing the duplicate identity lets
-- the constraint below be installed; perceptual reconciliation then collapses
-- the rows and reference-checks the managed file before removing it.
WITH ranked AS (
    SELECT
        media_assets.id,
        row_number() OVER duplicate AS duplicate_rank
    FROM public.media_assets
    WHERE media_assets.remote_url <> ''
    WINDOW duplicate AS (
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
            CASE WHEN media_assets.local_path <> '' THEN 0 ELSE 1 END,
            CASE media_assets.source WHEN 'custom' THEN 0 WHEN 'local' THEN 1 ELSE 2 END,
            (media_assets.width::bigint * media_assets.height::bigint) DESC,
            media_assets.file_size DESC,
            media_assets.sort_order,
            media_assets.id
    )
)
UPDATE public.media_assets AS asset
SET remote_url = ''
FROM ranked
WHERE asset.id = ranked.id
  AND ranked.duplicate_rank > 1;

-- Close backdrop gaps in the same collision-free staging range used by the
-- runtime deduplicator.
WITH ordered AS (
    SELECT
        media_assets.id,
        row_number() OVER (
            PARTITION BY media_assets.media_item_id
            ORDER BY media_assets.sort_order, media_assets.id
        ) - 1 AS wanted_order
    FROM public.media_assets
    WHERE media_assets.asset_type = 'backdrop'
)
UPDATE public.media_assets AS asset
SET sort_order = (-1000000000 + ordered.wanted_order)::integer
FROM ordered
WHERE asset.id = ordered.id;

UPDATE public.media_assets
SET sort_order = sort_order + 1000000000
WHERE asset_type = 'backdrop'
  AND sort_order < 0;

CREATE UNIQUE INDEX idx_media_assets_local_identity
    ON public.media_assets (
        media_item_id,
        asset_type,
        local_path,
        (CASE
            WHEN asset_type = 'still'
              OR label ~ '^season-[0-9]+$'
              OR label ~ '^s[0-9]+e[0-9]+$'
                THEN label
            ELSE ''
        END)
    )
    WHERE local_path <> '';

CREATE UNIQUE INDEX idx_media_assets_remote_identity
    ON public.media_assets (
        media_item_id,
        asset_type,
        remote_url,
        (CASE
            WHEN asset_type = 'still'
              OR label ~ '^season-[0-9]+$'
              OR label ~ '^s[0-9]+e[0-9]+$'
                THEN label
            ELSE ''
        END)
    )
    WHERE remote_url <> '';

-- +goose Down

DROP INDEX IF EXISTS public.idx_media_assets_remote_identity;
DROP INDEX IF EXISTS public.idx_media_assets_local_identity;
