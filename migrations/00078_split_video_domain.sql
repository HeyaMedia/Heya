-- +goose Up
-- Split the shared 'video' domain into 'movie' and 'tv'. Movies and TV have
-- different format libraries and tuned scores (Radarr vs Sonarr worlds) —
-- one shared domain forced name collisions (sonarr's x265 lost to radarr's)
-- and let a TV profile score movie formats. Existing rows follow their
-- import source; hand-made and seed rows land on 'movie'.
ALTER TABLE manager_quality_profiles DROP CONSTRAINT manager_quality_profiles_name_key;
ALTER TABLE manager_quality_profiles ADD CONSTRAINT manager_quality_profiles_domain_name_key UNIQUE (domain, name);

UPDATE manager_quality_profiles SET domain = 'tv' WHERE domain = 'video' AND source = 'sonarr';
UPDATE manager_quality_profiles SET domain = 'movie' WHERE domain = 'video';
UPDATE manager_custom_formats SET domain = 'tv' WHERE domain = 'video' AND source = 'sonarr';
UPDATE manager_custom_formats SET domain = 'movie' WHERE domain = 'video';

-- +goose Down
UPDATE manager_custom_formats SET domain = 'video' WHERE domain IN ('movie', 'tv');
UPDATE manager_quality_profiles SET domain = 'video' WHERE domain IN ('movie', 'tv');
ALTER TABLE manager_quality_profiles DROP CONSTRAINT manager_quality_profiles_domain_name_key;
ALTER TABLE manager_quality_profiles ADD CONSTRAINT manager_quality_profiles_name_key UNIQUE (name);
