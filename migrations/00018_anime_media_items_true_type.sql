-- +goose Up
-- Canonicalize anime media_items to their TRUE type. Anime is a distinct
-- library media_type on purpose (so anime can be handled distinctly); the
-- media_item must carry 'anime', not be flattened to 'tv'. Reads that mean
-- "TV-shaped content" use mediatype.IsTVLike / media_type IN ('tv','anime')
-- rather than a bare 'tv'. The legacy matcher path stored anime items as 'tv';
-- this repairs those rows to 'anime' so the type is consistent with the library.
-- Library scoping means no identity collision: an anime library holds no native
-- 'tv' rows to clash with.
UPDATE public.media_items
SET media_type = 'anime'
WHERE media_type = 'tv'
  AND library_id IN (SELECT id FROM public.libraries WHERE media_type = 'anime');

-- +goose Down
UPDATE public.media_items
SET media_type = 'tv'
WHERE media_type = 'anime'
  AND library_id IN (SELECT id FROM public.libraries WHERE media_type = 'anime');
