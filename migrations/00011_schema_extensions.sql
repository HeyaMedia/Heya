-- +goose Up

-- Movies: add cast/crew, budget, language, production details
ALTER TABLE movies ADD COLUMN original_title TEXT NOT NULL DEFAULT '';
ALTER TABLE movies ADD COLUMN original_language TEXT NOT NULL DEFAULT '';
ALTER TABLE movies ADD COLUMN budget BIGINT NOT NULL DEFAULT 0;
ALTER TABLE movies ADD COLUMN revenue BIGINT NOT NULL DEFAULT 0;
ALTER TABLE movies ADD COLUMN popularity NUMERIC(10,3) NOT NULL DEFAULT 0;
ALTER TABLE movies ADD COLUMN vote_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE movies ADD COLUMN production_companies TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE movies ADD COLUMN cast_data JSONB NOT NULL DEFAULT '[]';
ALTER TABLE movies ADD COLUMN crew_data JSONB NOT NULL DEFAULT '[]';

-- TV series: add network, creators, episode counts
ALTER TABLE tv_series ADD COLUMN original_name TEXT NOT NULL DEFAULT '';
ALTER TABLE tv_series ADD COLUMN original_language TEXT NOT NULL DEFAULT '';
ALTER TABLE tv_series ADD COLUMN networks TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE tv_series ADD COLUMN created_by TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE tv_series ADD COLUMN number_of_seasons INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tv_series ADD COLUMN number_of_episodes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tv_series ADD COLUMN popularity NUMERIC(10,3) NOT NULL DEFAULT 0;
ALTER TABLE tv_series ADD COLUMN vote_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tv_series ADD COLUMN cast_data JSONB NOT NULL DEFAULT '[]';

-- Albums: add label, country, barcode, totals
ALTER TABLE albums ADD COLUMN label TEXT NOT NULL DEFAULT '';
ALTER TABLE albums ADD COLUMN country TEXT NOT NULL DEFAULT '';
ALTER TABLE albums ADD COLUMN barcode TEXT NOT NULL DEFAULT '';
ALTER TABLE albums ADD COLUMN total_tracks INTEGER NOT NULL DEFAULT 0;
ALTER TABLE albums ADD COLUMN total_discs INTEGER NOT NULL DEFAULT 0;
ALTER TABLE albums ADD COLUMN tags TEXT[] NOT NULL DEFAULT '{}';

-- Books: add subjects, language, series info, description
ALTER TABLE books ADD COLUMN subjects TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE books ADD COLUMN language TEXT NOT NULL DEFAULT '';
ALTER TABLE books ADD COLUMN series_name TEXT NOT NULL DEFAULT '';
ALTER TABLE books ADD COLUMN series_number INTEGER NOT NULL DEFAULT 0;
ALTER TABLE books ADD COLUMN format TEXT NOT NULL DEFAULT '';
ALTER TABLE books ADD COLUMN description TEXT NOT NULL DEFAULT '';

-- Authors: add dates
ALTER TABLE authors ADD COLUMN birth_date TEXT NOT NULL DEFAULT '';
ALTER TABLE authors ADD COLUMN death_date TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE authors DROP COLUMN death_date;
ALTER TABLE authors DROP COLUMN birth_date;

ALTER TABLE books DROP COLUMN description;
ALTER TABLE books DROP COLUMN format;
ALTER TABLE books DROP COLUMN series_number;
ALTER TABLE books DROP COLUMN series_name;
ALTER TABLE books DROP COLUMN language;
ALTER TABLE books DROP COLUMN subjects;

ALTER TABLE albums DROP COLUMN tags;
ALTER TABLE albums DROP COLUMN total_discs;
ALTER TABLE albums DROP COLUMN total_tracks;
ALTER TABLE albums DROP COLUMN barcode;
ALTER TABLE albums DROP COLUMN country;
ALTER TABLE albums DROP COLUMN label;

ALTER TABLE tv_series DROP COLUMN cast_data;
ALTER TABLE tv_series DROP COLUMN vote_count;
ALTER TABLE tv_series DROP COLUMN popularity;
ALTER TABLE tv_series DROP COLUMN number_of_episodes;
ALTER TABLE tv_series DROP COLUMN number_of_seasons;
ALTER TABLE tv_series DROP COLUMN created_by;
ALTER TABLE tv_series DROP COLUMN networks;
ALTER TABLE tv_series DROP COLUMN original_language;
ALTER TABLE tv_series DROP COLUMN original_name;

ALTER TABLE movies DROP COLUMN crew_data;
ALTER TABLE movies DROP COLUMN cast_data;
ALTER TABLE movies DROP COLUMN production_companies;
ALTER TABLE movies DROP COLUMN vote_count;
ALTER TABLE movies DROP COLUMN popularity;
ALTER TABLE movies DROP COLUMN revenue;
ALTER TABLE movies DROP COLUMN budget;
ALTER TABLE movies DROP COLUMN original_language;
ALTER TABLE movies DROP COLUMN original_title;
