-- +goose Up

-- Structural transition boundaries per audio file, in milliseconds from the
-- start. Detected from the RMS energy envelope by the loudness worker (which
-- already decodes each file). The player's smart crossfade reads these to start
-- a transition where the track is naturally dying — at its fade/outro — instead
-- of a fixed N seconds before the end.
--
-- All nullable: a file that hasn't been analyzed yet (boundaries_analyzed_at IS
-- NULL) simply falls back to the timed crossfade on the client.
ALTER TABLE track_files
    ADD COLUMN intro_end_ms           INTEGER,
    ADD COLUMN outro_start_ms         INTEGER,
    ADD COLUMN fade_start_ms          INTEGER,
    ADD COLUMN silence_start_ms       INTEGER,
    ADD COLUMN boundaries_analyzed_at TIMESTAMPTZ;

-- +goose Down

ALTER TABLE track_files
    DROP COLUMN intro_end_ms,
    DROP COLUMN outro_start_ms,
    DROP COLUMN fade_start_ms,
    DROP COLUMN silence_start_ms,
    DROP COLUMN boundaries_analyzed_at;
