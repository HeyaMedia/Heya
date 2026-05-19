-- +goose Up

-- Asset types: poster, backdrop, banner, clearart, clearlogo, fanart, landscape,
-- logo, folder, thumb, season_poster, subtitle, lyrics, nfo
CREATE TYPE asset_type AS ENUM (
    'poster', 'backdrop', 'banner', 'clearart', 'clearlogo',
    'fanart', 'landscape', 'logo', 'folder', 'thumb',
    'season_poster', 'subtitle', 'lyrics', 'nfo'
);

-- Extra types follow Plex conventions
CREATE TYPE extra_type AS ENUM (
    'trailer', 'teaser', 'behind_the_scenes', 'deleted_scene',
    'featurette', 'interview', 'scene', 'short', 'other'
);

CREATE TABLE media_assets (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id   BIGINT      NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    asset_type      asset_type  NOT NULL,
    source          TEXT        NOT NULL DEFAULT 'local',
    local_path      TEXT        NOT NULL DEFAULT '',
    remote_url      TEXT        NOT NULL DEFAULT '',
    language        TEXT        NOT NULL DEFAULT '',
    label           TEXT        NOT NULL DEFAULT '',
    sort_order      INTEGER     NOT NULL DEFAULT 0,
    width           INTEGER     NOT NULL DEFAULT 0,
    height          INTEGER     NOT NULL DEFAULT 0,
    file_size       BIGINT      NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_media_assets_media_item ON media_assets (media_item_id);
CREATE INDEX idx_media_assets_type ON media_assets (media_item_id, asset_type);

CREATE TABLE media_extras (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id   BIGINT      NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    extra_type      extra_type  NOT NULL,
    title           TEXT        NOT NULL DEFAULT '',
    file_path       TEXT        NOT NULL DEFAULT '',
    duration_ms     INTEGER     NOT NULL DEFAULT 0,
    file_size       BIGINT      NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_media_extras_media_item ON media_extras (media_item_id);

-- Soft deletes for library files
ALTER TABLE library_files ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX idx_library_files_deleted ON library_files (deleted_at) WHERE deleted_at IS NOT NULL;

-- FFProbe data stored per library file
ALTER TABLE library_files ADD COLUMN media_info JSONB NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE library_files DROP COLUMN media_info;
ALTER TABLE library_files DROP COLUMN deleted_at;
DROP TABLE media_extras;
DROP TABLE media_assets;
DROP TYPE extra_type;
DROP TYPE asset_type;
