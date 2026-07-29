-- +goose Up
-- Heya Media Manager, phase 1: connection + policy config. Indexers speak
-- Torznab/Newznab (a 'prowlarr' row is an app connection whose sync
-- materializes per-indexer torznab child rows), download clients start with
-- SABnzbd, and quality profiles hold the ranked ladder the decision engine
-- will consume. Monitoring hangs off media_items — the manager is a lens over
-- the server's catalog, not a second one.

CREATE TABLE manager_indexers (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    name text NOT NULL,
    -- 'prowlarr' (app connection), 'torznab', 'newznab'
    kind text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    base_url text NOT NULL,
    api_key text DEFAULT ''::text NOT NULL,
    -- 'usenet' | 'torrent' | '' (prowlarr app rows carry no protocol)
    protocol text DEFAULT ''::text NOT NULL,
    -- lower = preferred when the same release appears on several indexers
    priority integer DEFAULT 25 NOT NULL,
    -- torznab category ids to search; empty = defaults per media domain
    categories integer[] DEFAULT '{}'::integer[] NOT NULL,
    -- '' for hand-added rows, 'prowlarr' for rows materialized by app sync
    source text DEFAULT ''::text NOT NULL,
    -- the prowlarr-side indexer id a synced row mirrors
    source_ref text DEFAULT ''::text NOT NULL,
    parent_id bigint,
    settings jsonb DEFAULT '{}'::jsonb NOT NULL,
    last_test_at timestamp with time zone,
    last_test_ok boolean DEFAULT false NOT NULL,
    last_test_error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT manager_indexers_pkey PRIMARY KEY (id),
    CONSTRAINT manager_indexers_parent_fkey FOREIGN KEY (parent_id)
        REFERENCES manager_indexers(id) ON DELETE CASCADE
);

-- Sync upsert identity for materialized child rows.
CREATE UNIQUE INDEX manager_indexers_parent_ref_idx
    ON manager_indexers (parent_id, source_ref)
    WHERE parent_id IS NOT NULL;

CREATE TABLE manager_download_clients (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    name text NOT NULL,
    -- 'sabnzbd' first; 'qbittorrent'/'nzbget'/'blackhole' later
    kind text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    protocol text NOT NULL,
    base_url text NOT NULL,
    api_key text DEFAULT ''::text NOT NULL,
    username text DEFAULT ''::text NOT NULL,
    password text DEFAULT ''::text NOT NULL,
    category text DEFAULT 'heya'::text NOT NULL,
    priority integer DEFAULT 1 NOT NULL,
    -- [{"remote": "/storage", "local": "/Volumes/Storage"}, …]: rewrites the
    -- client's reported paths into paths this process can stat. Identity when
    -- empty (prod shares the client's filesystem view).
    path_mappings jsonb DEFAULT '[]'::jsonb NOT NULL,
    settings jsonb DEFAULT '{}'::jsonb NOT NULL,
    last_test_at timestamp with time zone,
    last_test_ok boolean DEFAULT false NOT NULL,
    last_test_error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT manager_download_clients_pkey PRIMARY KEY (id)
);

CREATE TABLE manager_quality_profiles (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    name text NOT NULL,
    -- 'video' | 'music' | 'book' — which quality vocabulary the ladder uses
    domain text DEFAULT 'video'::text NOT NULL,
    -- ranked ladder, index 0 = best: [{"quality": "remux-2160p", "allowed": true}, …]
    items jsonb DEFAULT '[]'::jsonb NOT NULL,
    -- quality key where searching stops once met
    cutoff text DEFAULT ''::text NOT NULL,
    upgrades_enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT manager_quality_profiles_pkey PRIMARY KEY (id),
    CONSTRAINT manager_quality_profiles_name_key UNIQUE (name)
);

ALTER TABLE media_items
    ADD COLUMN monitored boolean DEFAULT false NOT NULL,
    ADD COLUMN quality_profile_id bigint;

ALTER TABLE media_items
    ADD CONSTRAINT media_items_quality_profile_fkey FOREIGN KEY (quality_profile_id)
        REFERENCES manager_quality_profiles(id) ON DELETE SET NULL;

-- Wanted scans iterate monitored items only; keep the common case cheap.
CREATE INDEX media_items_monitored_idx ON media_items (library_id) WHERE monitored;

-- Starter profiles matching the parser's quality vocabulary. Editable/deletable
-- like any user-created row — they just save the first-run ceremony.
INSERT INTO manager_quality_profiles (name, domain, items, cutoff) VALUES
(
    'HD-1080p', 'video',
    '[{"quality": "remux-1080p", "allowed": false},
      {"quality": "bluray-1080p", "allowed": true},
      {"quality": "webdl-1080p", "allowed": true},
      {"quality": "webrip-1080p", "allowed": true},
      {"quality": "hdtv-1080p", "allowed": true},
      {"quality": "bluray-720p", "allowed": false},
      {"quality": "webdl-720p", "allowed": false}]'::jsonb,
    'webdl-1080p'
),
(
    'UHD-2160p', 'video',
    '[{"quality": "remux-2160p", "allowed": true},
      {"quality": "bluray-2160p", "allowed": true},
      {"quality": "webdl-2160p", "allowed": true},
      {"quality": "webrip-2160p", "allowed": false},
      {"quality": "bluray-1080p", "allowed": true},
      {"quality": "webdl-1080p", "allowed": true}]'::jsonb,
    'remux-2160p'
),
(
    'Lossless', 'music',
    '[{"quality": "flac-24", "allowed": true},
      {"quality": "flac", "allowed": true},
      {"quality": "mp3-320", "allowed": true},
      {"quality": "mp3-v0", "allowed": false},
      {"quality": "mp3-256", "allowed": false}]'::jsonb,
    'flac'
),
(
    'EPUB', 'book',
    '[{"quality": "epub", "allowed": true},
      {"quality": "azw3", "allowed": true},
      {"quality": "mobi", "allowed": false},
      {"quality": "pdf", "allowed": false}]'::jsonb,
    'epub'
);

-- +goose Down
ALTER TABLE media_items DROP CONSTRAINT IF EXISTS media_items_quality_profile_fkey;
DROP INDEX IF EXISTS media_items_monitored_idx;
ALTER TABLE media_items
    DROP COLUMN IF EXISTS monitored,
    DROP COLUMN IF EXISTS quality_profile_id;
DROP TABLE IF EXISTS manager_quality_profiles;
DROP TABLE IF EXISTS manager_download_clients;
DROP TABLE IF EXISTS manager_indexers;
