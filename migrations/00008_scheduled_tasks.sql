-- +goose Up

CREATE TABLE scheduled_tasks (
    id                        TEXT PRIMARY KEY,
    display_name              TEXT    NOT NULL,
    description               TEXT    NOT NULL DEFAULT '',
    category                  TEXT    NOT NULL DEFAULT 'media',
    enabled                   BOOLEAN NOT NULL DEFAULT false,
    interval_hours            INT     NOT NULL DEFAULT 24,
    daily_start_time          TEXT    NOT NULL DEFAULT '02:00',
    daily_end_time            TEXT    NOT NULL DEFAULT '06:00',
    max_runtime_minutes       INT     NOT NULL DEFAULT 120,
    last_run_at               TIMESTAMPTZ,
    last_run_result           TEXT    NOT NULL DEFAULT '',
    last_run_duration_sec     INT     NOT NULL DEFAULT 0,
    last_run_items_processed  INT     NOT NULL DEFAULT 0,
    last_run_items_total      INT     NOT NULL DEFAULT 0,
    next_run_at               TIMESTAMPTZ,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO scheduled_tasks (id, display_name, description, category, enabled) VALUES
    ('generate_trickplay', 'Generate Trickplay Sprites', 'Create timeline preview thumbnails for video files in libraries with trickplay enabled', 'media', false),
    ('generate_thumbnails', 'Generate Missing Thumbnails', 'Extract thumbnail frames for extras and episodes without artwork', 'media', false),
    ('scan_libraries', 'Scan Libraries', 'Scan all library paths for new, changed, or deleted media files', 'library', true),
    ('refresh_metadata', 'Refresh Metadata', 'Re-fetch metadata for items older than each library''s configured refresh period', 'library', true);

-- +goose Down
DROP TABLE scheduled_tasks;
