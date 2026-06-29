-- +goose NO TRANSACTION

-- Episode stills are a distinct concept from backdrops. They used to be stored
-- on the parent show's media_item as asset_type='backdrop' with an 'sXXeYY'
-- label, which polluted the show's real backdrop set and the metadata editor's
-- backdrop view. Give episodes their own asset_type and reclassify the existing
-- rows in place (preserving the few that actually downloaded — episode stills
-- 404 on heya.media ~99% of the time and can't be re-fetched).
--
-- ALTER TYPE ... ADD VALUE cannot run inside a transaction, and a freshly added
-- enum value can't be used in the same transaction it's added — hence
-- NO TRANSACTION, which makes goose run each statement autocommitted.

-- +goose Up
ALTER TYPE asset_type ADD VALUE IF NOT EXISTS 'still';

-- Labels are built with fmt.Sprintf("s%02de%02d", …), so each number is *at
-- least* two digits but more for seasons/episodes >= 100 (e.g. 's01e100' for
-- long-running or anime series). Match one-or-more digits, not exactly two, or
-- those stills stay mis-typed as 'backdrop'.
UPDATE media_assets
   SET asset_type = 'still'
 WHERE asset_type = 'backdrop'
   AND label ~ '^s[0-9]+e[0-9]+$';

-- +goose Down
-- Enum values cannot be dropped; leaving 'still' in place is harmless. Just
-- fold the episode stills back into the backdrop set.
UPDATE media_assets
   SET asset_type = 'backdrop'
 WHERE asset_type = 'still'
   AND label ~ '^s[0-9]+e[0-9]+$';
