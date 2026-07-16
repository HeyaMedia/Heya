-- +goose Up
-- Remaining slices of the heya.media 2026-07 provider expansion.

-- Performance credits per track (MusicBrainz artist-relationships via the
-- recording document): [{role, attributes[], artist_name, artist_mbid,
-- artist_entity_id}]. Roles/attributes are snake_case; humanize on display.
ALTER TABLE tracks ADD COLUMN credits jsonb NOT NULL DEFAULT '[]';

-- TheAudioDB ships an editorial writeup per music video.
ALTER TABLE media_videos ADD COLUMN description text NOT NULL DEFAULT '';

-- TheAudioDB follower count (ledger cell next to Last.fm listeners).
ALTER TABLE artists ADD COLUMN followers bigint NOT NULL DEFAULT 0;

-- Issued-release facts (the matched edition's release document):
-- per-country release events [{date, country}] and the writing script.
ALTER TABLE albums ADD COLUMN release_events jsonb NOT NULL DEFAULT '[]';
ALTER TABLE albums ADD COLUMN script text NOT NULL DEFAULT '';

-- Extra artwork classes (audiodb album renders: back/cdart/spine/case/
-- flat/face; artist clearart/cutout ride media_assets via new asset types).
-- Albums have no media_item, so their gallery lives on the row as
-- [{type, url}] remote references served through the existing image proxy.
ALTER TABLE albums ADD COLUMN artwork jsonb NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE albums DROP COLUMN artwork;
ALTER TABLE albums DROP COLUMN script;
ALTER TABLE albums DROP COLUMN release_events;
ALTER TABLE artists DROP COLUMN followers;
ALTER TABLE media_videos DROP COLUMN description;
ALTER TABLE tracks DROP COLUMN credits;
