-- +goose Up

-- Expand artists, albums, and tracks to capture every field heya.media's
-- ArtistDocBody exposes — the local-first / remote-fill story only works if
-- "remote-fill" has somewhere to land. Generated client at
-- clients/heyamedia/client.gen.go is the source of truth for the shapes.

-- Artists ---------------------------------------------------------------
ALTER TABLE artists
    -- Last.fm popularity signals (also useful for sort + autocomplete weight).
    ADD COLUMN listeners        BIGINT  NOT NULL DEFAULT 0,
    ADD COLUMN playcount        BIGINT  NOT NULL DEFAULT 0,
    ADD COLUMN popularity       INTEGER NOT NULL DEFAULT 0,
    -- Wikipedia / official-site annotation (separate from `biography` which
    -- comes from Discogs/Apple); MB lists this as "annotation".
    ADD COLUMN annotation       TEXT    NOT NULL DEFAULT '',
    -- ArtistURL list ([{type, url}]) — typed as jsonb because the elements
    -- have a stable two-field schema and we surface them as link chips.
    ADD COLUMN urls             JSONB   NOT NULL DEFAULT '[]'::jsonb,
    -- {language → wikipedia URL}. Stable key shape but sparse — jsonb is
    -- cheaper than a side table for read-only metadata.
    ADD COLUMN wikipedia_links  JSONB   NOT NULL DEFAULT '{}'::jsonb,
    -- {provider → profile-url} from last.fm/musicbrainz/etc.
    ADD COLUMN profiles         JSONB   NOT NULL DEFAULT '{}'::jsonb,
    -- All known aliases (locale variants, romanizations, alt-script forms).
    ADD COLUMN aliases          TEXT[]  NOT NULL DEFAULT '{}',
    -- ArtistMember list — band relationships. jsonb because nested + queried
    -- only as a render-time fan-out, not filtered/sorted.
    ADD COLUMN groups           JSONB   NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN members          JSONB   NOT NULL DEFAULT '[]'::jsonb,
    -- Artist type ("Person" / "Group" / etc.) from MusicBrainz.
    ADD COLUMN artist_type      TEXT    NOT NULL DEFAULT '',
    -- Lifecycle dates — useful for the artist detail page, also for
    -- range-style searches ("80s artists").
    ADD COLUMN begin_date       TEXT    NOT NULL DEFAULT '',
    ADD COLUMN begin_year       INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN end_date         TEXT    NOT NULL DEFAULT '',
    ADD COLUMN ended            BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN deathday         TEXT    NOT NULL DEFAULT '',
    ADD COLUMN birthplace       TEXT    NOT NULL DEFAULT '',
    -- Genre tags (free-form strings, distinct from MB's structured genres).
    -- MusicBrainz exposes both `genres` (curated) and `tags` (folksonomy);
    -- the matcher already merges them into a single set on the artist row
    -- so a flat array suffices.
    ADD COLUMN tags             TEXT[]  NOT NULL DEFAULT '{}';

-- Top tracks: a small N (typically 10-50) per artist, rendered as a
-- "popular" rail on the artist page. Separate table because we'll order +
-- limit + join to local tracks-by-mbid for "play locally if we have it".
CREATE TABLE artist_top_tracks (
    id          BIGINT  GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    artist_id   BIGINT  NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    rank        INTEGER NOT NULL,         -- 0-based; heya.media returns sorted
    title       TEXT    NOT NULL,
    mbid        TEXT    NOT NULL DEFAULT '',
    playcount   BIGINT  NOT NULL DEFAULT 0,
    listeners   BIGINT  NOT NULL DEFAULT 0,
    url         TEXT    NOT NULL DEFAULT '',
    UNIQUE (artist_id, rank)
);
CREATE INDEX idx_artist_top_tracks_artist ON artist_top_tracks (artist_id, rank);
CREATE INDEX idx_artist_top_tracks_mbid ON artist_top_tracks (mbid) WHERE mbid != '';

-- Similar artists from Last.fm / ListenBrainz. local_artist_id is filled
-- when the similar artist exists in our library — enables a "click to open"
-- link on the artist page (vs an external-only "Listen on Last.fm" badge).
CREATE TABLE artist_similar_artists (
    id              BIGINT  GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    artist_id       BIGINT  NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    rank            INTEGER NOT NULL,
    name            TEXT    NOT NULL,
    mbid            TEXT    NOT NULL DEFAULT '',
    match_score     NUMERIC(6, 4) NOT NULL DEFAULT 0,
    url             TEXT    NOT NULL DEFAULT '',
    local_artist_id BIGINT  REFERENCES artists(id) ON DELETE SET NULL,
    UNIQUE (artist_id, rank)
);
CREATE INDEX idx_artist_similar_artist ON artist_similar_artists (artist_id, rank);

-- Albums ----------------------------------------------------------------
ALTER TABLE albums
    ADD COLUMN catalog_no       TEXT    NOT NULL DEFAULT '',
    ADD COLUMN explicit         BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN original_title   TEXT    NOT NULL DEFAULT '',
    -- MB's secondary release-group types (e.g. ["compilation", "live"] on
    -- top of the primary "album").
    ADD COLUMN secondary_types  TEXT[]  NOT NULL DEFAULT '{}',
    -- Discogs styles — finer-grained genres ("Italo-Disco", "UK Garage").
    ADD COLUMN styles           TEXT[]  NOT NULL DEFAULT '{}',
    -- Folksonomy tags (distinct from `genres` which is the curated set).
    -- Skipping the tags column on `albums` — `genres` already covers what
    -- the UI uses, and the rate-limited Last.fm tag fetch isn't worth a
    -- second array column. Promote later if needed.
    ADD COLUMN language         TEXT    NOT NULL DEFAULT '',
    -- Sum of track durations from upstream (we compute our own from
    -- ffprobe'd track_files, but having upstream's number lets us spot
    -- missing tracks).
    ADD COLUMN duration_seconds INTEGER NOT NULL DEFAULT 0,
    -- ISRCs for the entire release — distinct from per-track ISRC.
    ADD COLUMN isrcs            TEXT[]  NOT NULL DEFAULT '{}',
    ADD COLUMN rating           NUMERIC(4, 2) NOT NULL DEFAULT 0,
    ADD COLUMN popularity       INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN listeners        BIGINT  NOT NULL DEFAULT 0,
    ADD COLUMN playcount        BIGINT  NOT NULL DEFAULT 0,
    -- External IDs map (apple, deezer, mb_release, mb_release_group, …).
    -- Mirrors what `media_items.external_ids` does for the top-level item.
    ADD COLUMN external_ids     JSONB   NOT NULL DEFAULT '{}'::jsonb,
    -- ArtistCredit list — the parsed "Various Artists / feat. X" payload
    -- for compilations and split releases. Each entry is {name, mbid, slug,
    -- join_phrase}.
    ADD COLUMN artist_credits   JSONB   NOT NULL DEFAULT '[]'::jsonb;

-- Tracks ----------------------------------------------------------------
ALTER TABLE tracks
    -- Per-track external IDs ({apple, deezer, mb_track, ...}). The mb_track
    -- and recording_mbid are critical for LRCLIB lyrics lookups (the most
    -- reliable resolver key beyond a fuzzy title match).
    ADD COLUMN external_ids   JSONB   NOT NULL DEFAULT '{}'::jsonb,
    -- Per-track ISRC — globally unique recording identifier, used by some
    -- lyrics providers and streaming-services cross-linking.
    ADD COLUMN isrc           TEXT    NOT NULL DEFAULT '',
    ADD COLUMN recording_mbid TEXT    NOT NULL DEFAULT '',
    -- 30-second preview URL (typically iTunes/Deezer CDN). Used by the
    -- planned hover-to-preview behaviour on track rows.
    ADD COLUMN preview_url    TEXT    NOT NULL DEFAULT '',
    ADD COLUMN explicit       BOOLEAN NOT NULL DEFAULT false,
    -- Per-track ArtistCredit list — covers "Title (feat. X)" cleanly.
    ADD COLUMN artist_credits JSONB   NOT NULL DEFAULT '[]'::jsonb;

CREATE INDEX idx_tracks_isrc           ON tracks (isrc)           WHERE isrc != '';
CREATE INDEX idx_tracks_recording_mbid ON tracks (recording_mbid) WHERE recording_mbid != '';
CREATE INDEX idx_albums_external_ids   ON albums USING GIN (external_ids);
CREATE INDEX idx_tracks_external_ids   ON tracks USING GIN (external_ids);

-- +goose Down

DROP INDEX IF EXISTS idx_tracks_external_ids;
DROP INDEX IF EXISTS idx_albums_external_ids;
DROP INDEX IF EXISTS idx_tracks_recording_mbid;
DROP INDEX IF EXISTS idx_tracks_isrc;

ALTER TABLE tracks
    DROP COLUMN artist_credits,
    DROP COLUMN explicit,
    DROP COLUMN preview_url,
    DROP COLUMN recording_mbid,
    DROP COLUMN isrc,
    DROP COLUMN external_ids;

ALTER TABLE albums
    DROP COLUMN artist_credits,
    DROP COLUMN external_ids,
    DROP COLUMN playcount,
    DROP COLUMN listeners,
    DROP COLUMN popularity,
    DROP COLUMN rating,
    DROP COLUMN isrcs,
    DROP COLUMN duration_seconds,
    DROP COLUMN language,
    DROP COLUMN styles,
    DROP COLUMN secondary_types,
    DROP COLUMN original_title,
    DROP COLUMN explicit,
    DROP COLUMN catalog_no;

DROP TABLE artist_similar_artists;
DROP TABLE artist_top_tracks;

ALTER TABLE artists
    DROP COLUMN tags,
    DROP COLUMN birthplace,
    DROP COLUMN deathday,
    DROP COLUMN ended,
    DROP COLUMN end_date,
    DROP COLUMN begin_year,
    DROP COLUMN begin_date,
    DROP COLUMN artist_type,
    DROP COLUMN members,
    DROP COLUMN groups,
    DROP COLUMN aliases,
    DROP COLUMN profiles,
    DROP COLUMN wikipedia_links,
    DROP COLUMN urls,
    DROP COLUMN annotation,
    DROP COLUMN popularity,
    DROP COLUMN playcount,
    DROP COLUMN listeners;
