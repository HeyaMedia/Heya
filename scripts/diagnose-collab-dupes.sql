-- diagnose-collab-dupes.sql
--
-- One-shot cleanup helper for the music-enrichment dedup fix (v0.1.3).
-- Phase 0 stops NEW false merges; it can't undo merges that already happened
-- before the fix shipped. Run these read-only queries against the affected DB
-- (psql "$DATABASE_URL" -f scripts/diagnose-collab-dupes.sql), then re-enrich
-- the rows they surface:
--
--   heya media refresh <slug>        # per-artist, re-runs enrichment
--   heya library scan <library_id>   # whole library
--
-- The marker regex matches the collaboration separators the precision gate
-- uses ( & / feat / ft / featuring / vs / versus / + ), space-padded so it
-- never fires on substrings ("AT&T", "Daft Punk").

\echo '== Query A: surviving collaboration artists =='
\echo '(name still reads as a collaboration — re-enrich to confirm the gate keeps'
\echo ' them distinct now; these are safe to refresh)'
SELECT a.id AS artist_id, mi.slug, a.name, mi.library_id,
       a.discography_enriched_at
FROM artists a
JOIN media_items mi ON mi.id = a.media_item_id
WHERE a.name ~* '( & | feat\.? | ft\.? | featuring | vs\.? | versus | \+ )'
ORDER BY mi.library_id, a.name;

\echo ''
\echo '== Query B: artists assembled from >1 top-level folder (review) =='
\echo '(structural tell of a merge that fused two folders into one artist row.'
\echo ' Catches BOTH correct merges (HANABIE / 花冷え。) and wrong ones (a duo'
\echo ' folded into a member) — eyeball the folder list to tell them apart.'
\echo ' Assumes a /<root>/<artist>/<album>/<file> layout; artist folder is the'
\echo ' 4th path segment — adjust split_part index if your music root differs.)'
SELECT a.id AS artist_id, mi.slug, a.name,
       count(DISTINCT split_part(lf.path, '/', 4)) AS folders,
       array_agg(DISTINCT split_part(lf.path, '/', 4)) AS folder_names
FROM artists a
JOIN media_items mi  ON mi.id = a.media_item_id
JOIN albums al       ON al.artist_id = a.id
JOIN tracks t        ON t.album_id = al.id
JOIN track_files tf  ON tf.track_id = t.id
JOIN library_files lf ON lf.id = tf.library_file_id
GROUP BY a.id, mi.slug, a.name
HAVING count(DISTINCT split_part(lf.path, '/', 4)) > 1
ORDER BY folders DESC, a.name;
