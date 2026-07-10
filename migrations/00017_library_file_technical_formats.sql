-- +goose Up
-- Keep the browse-time technical filters out of the large ffprobe JSON blob.
-- These helpers are also used by the single media_info writer, so newly
-- probed files and this one-time backfill always use identical rules.
-- +goose StatementBegin
CREATE FUNCTION public.media_video_formats(info jsonb) RETURNS text[]
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
  WITH streams AS (
    SELECT s
    FROM jsonb_array_elements(COALESCE(info->'streams', '[]'::jsonb)) AS s
    WHERE s->>'codec_type' = 'video'
  ), flags AS (
    SELECT
      EXISTS (SELECT 1 FROM streams) AS has_video,
      EXISTS (
        SELECT 1 FROM streams, jsonb_array_elements(COALESCE(s->'side_data_list', '[]'::jsonb)) sd
        WHERE lower(COALESCE(sd->>'side_data_type', '')) LIKE ANY (ARRAY['%dovi%', '%dolby vision%'])
           OR COALESCE((sd->>'dv_profile')::int, 0) > 0
      ) AS dolby_vision,
      EXISTS (
        SELECT 1 FROM streams
        WHERE lower(COALESCE(s->>'color_transfer', '')) IN ('smpte2084', 'smpte-st-2084')
      ) AS pq,
      EXISTS (
        SELECT 1 FROM streams
        WHERE lower(COALESCE(s->>'color_transfer', '')) IN ('arib-std-b67', 'hlg')
      ) AS hlg,
      lower(info::text) LIKE ANY (ARRAY['%hdr10+%', '%hdr10plus%', '%dynamic hdr plus%']) AS hdr10_plus
  )
  SELECT COALESCE(array_agg(format ORDER BY format), ARRAY[]::text[])
  FROM flags
  CROSS JOIN LATERAL unnest(ARRAY[
    CASE WHEN dolby_vision THEN 'dolby-vision' END,
    CASE WHEN pq OR hlg OR dolby_vision OR hdr10_plus THEN 'hdr' END,
    CASE WHEN pq THEN 'hdr10' END,
    CASE WHEN hdr10_plus THEN 'hdr10-plus' END,
    CASE WHEN hlg THEN 'hlg' END,
    CASE WHEN has_video AND NOT (pq OR hlg OR dolby_vision OR hdr10_plus) THEN 'sdr' END
  ]) format
  WHERE format IS NOT NULL;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION public.media_audio_formats(info jsonb) RETURNS text[]
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
  WITH audio AS (
    SELECT s,
           lower(COALESCE(s->>'codec_name', '')) AS codec,
           lower(COALESCE(s->>'profile', '')) AS profile,
           lower(COALESCE(s->>'codec_long_name', '')) AS long_name
    FROM jsonb_array_elements(COALESCE(info->'streams', '[]'::jsonb)) AS s
    WHERE s->>'codec_type' = 'audio'
  ), formats AS (
    SELECT CASE
      WHEN codec = 'aac' THEN 'aac'
      WHEN codec = 'flac' THEN 'flac'
      WHEN codec = 'opus' THEN 'opus'
      WHEN codec IN ('mp3', 'mp2') THEN 'mp3'
      WHEN codec LIKE 'pcm_%' THEN 'pcm'
    END AS format FROM audio
    UNION ALL SELECT 'dolby-audio' FROM audio WHERE codec IN ('ac3', 'eac3', 'truehd', 'mlp')
    UNION ALL SELECT 'dolby-digital' FROM audio WHERE codec = 'ac3'
    UNION ALL SELECT 'dolby-digital-plus' FROM audio WHERE codec = 'eac3'
    UNION ALL SELECT 'truehd' FROM audio WHERE codec IN ('truehd', 'mlp')
    UNION ALL SELECT 'dts' FROM audio WHERE codec LIKE 'dts%'
    UNION ALL SELECT 'dts-hd' FROM audio
      WHERE codec LIKE 'dts%' AND (profile LIKE '%dts-hd%' OR long_name LIKE '%dts-hd%')
    UNION ALL SELECT 'dolby-atmos' WHERE lower(info::text) LIKE '%atmos%'
    UNION ALL SELECT 'dts-x' WHERE lower(info::text) LIKE ANY (ARRAY['%dts:x%', '%dts-x%'])
  )
  SELECT COALESCE(array_agg(DISTINCT format ORDER BY format), ARRAY[]::text[])
  FROM formats WHERE format IS NOT NULL;
$$;
-- +goose StatementEnd

ALTER TABLE public.library_files
  ADD COLUMN video_formats text[] NOT NULL DEFAULT '{}',
  ADD COLUMN audio_formats text[] NOT NULL DEFAULT '{}';

UPDATE public.library_files
SET video_formats = public.media_video_formats(media_info),
    audio_formats = public.media_audio_formats(media_info)
WHERE media_info <> '{}'::jsonb;

-- +goose Down
ALTER TABLE public.library_files
  DROP COLUMN IF EXISTS video_formats,
  DROP COLUMN IF EXISTS audio_formats;
DROP FUNCTION IF EXISTS public.media_audio_formats(jsonb);
DROP FUNCTION IF EXISTS public.media_video_formats(jsonb);
