-- +goose Up

-- Older installs may have persisted the former default through the Workers
-- settings UI. Move only that exact legacy value; deliberate custom values
-- and environment overrides remain untouched.
UPDATE public.system_settings
SET value = '4'::jsonb,
    updated_at = now()
WHERE key IN (
    'jobs.workers.search_metadata',
    'jobs.workers.search_metadata_poll',
    'jobs.workers.fetch_metadata',
    'jobs.workers.fetch_metadata_poll'
)
  AND value = '50'::jsonb;

-- +goose Down

-- Intentionally irreversible: restoring a high-concurrency value during a
-- rollback would recreate the database pressure this migration removes.
SELECT 1;
