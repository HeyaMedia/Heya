-- +goose Up

-- Browse > Tempo counts/drilldown (CountTracksByTempoBand[s],
-- ListTracksByTempoBand) all filter "bpm IS NOT NULL AND bpm >= x AND
-- bpm < y" over the full track_facets table (~100k rows; the only existing
-- indexes are the two HNSW vector indexes + analyzer_version). A partial
-- btree scoped to analyzed tracks lets every band predicate range-scan
-- instead of seq-scanning.
CREATE INDEX idx_track_facets_bpm ON public.track_facets USING btree (bpm)
    WHERE bpm IS NOT NULL;

-- Browse > Moods counts/drilldown (CountTracksByMood[s], ListTracksByMood)
-- filter "(mood_tags->>'<key>')::real > threshold" for one of the 9 fixed
-- keys in moodOrder (internal/service/music_browse.go). One partial
-- expression index per key: the WHERE clause scopes each index to rows that
-- actually carry that key (mood_tags is sparse — populated per analyzer
-- version), and the indexed expression matches the query's own cast so
-- Postgres can range-scan against the threshold instead of unpacking jsonb
-- on every row. The mood key travels as a bind parameter, but Postgres
-- re-plans bind-parameterized queries against their actual argument values
-- for the first 5 executions of a prepared statement, and keeps using that
-- specialized (index) plan indefinitely once it sees it's far cheaper than
-- the generic (seq-scan) one — which it reliably will be here.
CREATE INDEX idx_track_facets_mood_danceability ON public.track_facets USING btree (((mood_tags ->> 'danceability')::real))
    WHERE mood_tags ? 'danceability';
CREATE INDEX idx_track_facets_mood_voice ON public.track_facets USING btree (((mood_tags ->> 'voice')::real))
    WHERE mood_tags ? 'voice';
CREATE INDEX idx_track_facets_mood_happy ON public.track_facets USING btree (((mood_tags ->> 'mood_happy')::real))
    WHERE mood_tags ? 'mood_happy';
CREATE INDEX idx_track_facets_mood_sad ON public.track_facets USING btree (((mood_tags ->> 'mood_sad')::real))
    WHERE mood_tags ? 'mood_sad';
CREATE INDEX idx_track_facets_mood_aggressive ON public.track_facets USING btree (((mood_tags ->> 'mood_aggressive')::real))
    WHERE mood_tags ? 'mood_aggressive';
CREATE INDEX idx_track_facets_mood_relaxed ON public.track_facets USING btree (((mood_tags ->> 'mood_relaxed')::real))
    WHERE mood_tags ? 'mood_relaxed';
CREATE INDEX idx_track_facets_mood_party ON public.track_facets USING btree (((mood_tags ->> 'mood_party')::real))
    WHERE mood_tags ? 'mood_party';
CREATE INDEX idx_track_facets_mood_electronic ON public.track_facets USING btree (((mood_tags ->> 'mood_electronic')::real))
    WHERE mood_tags ? 'mood_electronic';
CREATE INDEX idx_track_facets_mood_acoustic ON public.track_facets USING btree (((mood_tags ->> 'mood_acoustic')::real))
    WHERE mood_tags ? 'mood_acoustic';

-- Browse > Genres drilldown (ListTracksByGenre / CountTracksByGenre /
-- TopArtistsByGenres) deliberately gets no index here. The predicate unnests
-- tf.top_genres via `CROSS JOIN LATERAL jsonb_array_elements(...)` and then
-- filters each element's name/score *after* unpacking — there's no top-level
-- expression or containment clause on the tf.top_genres column itself for an
-- index to attach to. A GIN(top_genres jsonb_path_ops) index would only help
-- if the query first narrowed rows with a redundant
-- `top_genres @> '[{"name": "..."}]'` clause ahead of the LATERAL unnest; it
-- doesn't, so adding one here would just be a write-amplifying index nothing
-- ever plans against. Leaving this as a seq/bitmap scan over track_facets is
-- the honest choice until that query shape changes.

-- +goose Down

DROP INDEX IF EXISTS public.idx_track_facets_mood_acoustic;
DROP INDEX IF EXISTS public.idx_track_facets_mood_electronic;
DROP INDEX IF EXISTS public.idx_track_facets_mood_party;
DROP INDEX IF EXISTS public.idx_track_facets_mood_relaxed;
DROP INDEX IF EXISTS public.idx_track_facets_mood_aggressive;
DROP INDEX IF EXISTS public.idx_track_facets_mood_sad;
DROP INDEX IF EXISTS public.idx_track_facets_mood_happy;
DROP INDEX IF EXISTS public.idx_track_facets_mood_voice;
DROP INDEX IF EXISTS public.idx_track_facets_mood_danceability;
DROP INDEX IF EXISTS public.idx_track_facets_bpm;
