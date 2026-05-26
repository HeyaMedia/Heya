-- +goose Up

-- person_external_credits caches the upstream cast/crew/known-for credit
-- list from the Heya metadata API. Used to power the "Known For" tab on a
-- person page — titles the actor/director has worked on that we don't have
-- in the local library. The `matched_media_item_id` link (resolved by a
-- LEFT JOIN at query time via external_ids overlap) lets the FE dim or
-- dedupe rows that the user already owns.
--
-- One table with a `kind` discriminator keeps the schema small; an actor
-- with 200 cast credits and 5 crew credits writes 205 rows here. The unique
-- key tolerates duplicates from multiple providers reporting the same role
-- by including character + job in the key.
CREATE TABLE person_external_credits (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    person_id     BIGINT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    kind          TEXT   NOT NULL CHECK (kind IN ('cast', 'crew', 'known_for')),
    media_kind    TEXT   NOT NULL DEFAULT '',     -- "movie" | "tv" | ""
    title         TEXT   NOT NULL DEFAULT '',
    year          INTEGER NOT NULL DEFAULT 0,
    character     TEXT   NOT NULL DEFAULT '',
    job           TEXT   NOT NULL DEFAULT '',
    department    TEXT   NOT NULL DEFAULT '',
    episode_count INTEGER NOT NULL DEFAULT 0,
    display_order INTEGER NOT NULL DEFAULT 0,
    slug          TEXT   NOT NULL DEFAULT '',
    poster_url    TEXT   NOT NULL DEFAULT '',
    external_ids  JSONB  NOT NULL DEFAULT '{}',
    source        TEXT   NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (person_id, kind, title, year, character, job)
);

CREATE INDEX idx_person_external_credits_person ON person_external_credits (person_id, kind, display_order);
CREATE INDEX idx_person_external_credits_external_ids ON person_external_credits USING GIN (external_ids);

-- +goose Down
DROP TABLE IF EXISTS person_external_credits;
