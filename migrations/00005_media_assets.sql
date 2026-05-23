-- +goose Up

CREATE TYPE asset_type AS ENUM (
    'poster', 'backdrop', 'logo', 'art', 'banner', 'thumb', 'disc', 'clearart',
    'subtitle', 'lyrics', 'nfo'
);

CREATE TYPE extra_type AS ENUM (
    'trailer', 'teaser', 'behind_the_scenes', 'deleted_scene',
    'featurette', 'interview', 'scene', 'short', 'other'
);

CREATE TABLE media_assets (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id BIGINT      NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    asset_type    asset_type  NOT NULL,
    source        TEXT        NOT NULL DEFAULT 'local',
    local_path    TEXT        NOT NULL DEFAULT '',
    remote_url    TEXT        NOT NULL DEFAULT '',
    language      TEXT        NOT NULL DEFAULT '',
    label         TEXT        NOT NULL DEFAULT '',
    sort_order    INTEGER     NOT NULL DEFAULT 0,
    width         INTEGER     NOT NULL DEFAULT 0,
    height        INTEGER     NOT NULL DEFAULT 0,
    file_size     BIGINT      NOT NULL DEFAULT 0,
    score         NUMERIC(8,3) NOT NULL DEFAULT 0,
    likes         INTEGER     NOT NULL DEFAULT 0,
    aspect        TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_media_assets_media_item ON media_assets (media_item_id);
CREATE INDEX idx_media_assets_type ON media_assets (media_item_id, asset_type);
CREATE UNIQUE INDEX idx_media_assets_unique ON media_assets (media_item_id, asset_type, sort_order, local_path);

CREATE TABLE media_extras (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id  BIGINT      NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    extra_type     extra_type  NOT NULL,
    title          TEXT        NOT NULL DEFAULT '',
    file_path      TEXT        NOT NULL DEFAULT '',
    duration_ms    INTEGER     NOT NULL DEFAULT 0,
    file_size      BIGINT      NOT NULL DEFAULT 0,
    thumbnail_path TEXT        NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_media_extras_media_item ON media_extras (media_item_id);
CREATE UNIQUE INDEX idx_media_extras_unique ON media_extras (media_item_id, extra_type, title);

-- +goose Down
DROP TABLE media_extras;
DROP TABLE media_assets;
DROP TYPE extra_type;
DROP TYPE asset_type;
