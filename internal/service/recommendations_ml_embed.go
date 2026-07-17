package service

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/musicsemantic"
	"github.com/karbowiak/heya/internal/textembed"
	"github.com/pgvector/pgvector-go"
	"github.com/rs/zerolog/log"
)

// This file owns the ML write + read paths: composing the per-item embed doc,
// backfilling media_item_facets, and natural-language semantic search. All of
// it no-ops cleanly when the engine is disabled (recEmbedderInstance → nil).

// ErrMLDisabled is returned by ML-only paths when the engine is off / not ready.
var ErrMLDisabled = fmt.Errorf("recommendation ML engine is not enabled or the model is still downloading")

// embedDoc composes the text embedded per item: title + genres + keywords + top
// cast + overview. Must stay stable — bump textembed.Version to re-embed on change.
func embedDoc(title string, genres, keywords, cast []string, desc string) string {
	var b strings.Builder
	b.WriteString(title)
	if len(genres) > 0 {
		b.WriteString(". Genres: " + strings.Join(genres, ", "))
	}
	if len(keywords) > 0 {
		b.WriteString(". Themes: " + strings.Join(capN(keywords, 12), ", "))
	}
	if len(cast) > 0 {
		b.WriteString(". Starring: " + strings.Join(capN(cast, 6), ", "))
	}
	if desc != "" {
		if len(desc) > 500 {
			desc = desc[:500]
		}
		b.WriteString(". " + desc)
	}
	return b.String()
}

func capN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

type embedDocRow struct {
	id  int64
	doc string
}

type musicEmbedDocRow struct {
	id  uuid.UUID
	doc string
}

func (a *App) loadMusicEmbedDocs(ctx context.Context) ([]musicEmbedDocRow, error) {
	rows, err := sqlc.New(a.db).ListMusicCatalogEmbeddingRows(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]musicEmbedDocRow, 0, len(rows))
	for _, row := range rows {
		doc := musicsemantic.Document(musicsemantic.Facets{
			Genres: row.Genres, Tags: row.Tags, Moods: row.Moods,
			Instrumentation:      row.Instrumentation,
			VocalCharacteristics: row.VocalCharacteristics,
			RecordingAttributes:  row.RecordingAttributes,
		})
		if doc != "" {
			out = append(out, musicEmbedDocRow{id: row.RecordingEntityID, doc: doc})
		}
	}
	return out, nil
}

// loadVideoEmbedDocs builds the embed doc for every video item. Staleness is
// decided by the caller via doc hashes, not here — a doc whose source metadata
// changed must recompose to be detected.
func (a *App) loadVideoEmbedDocs(ctx context.Context) ([]embedDocRow, error) {
	type meta struct {
		title, desc      string
		genres, kw, cast []string
	}
	items := map[int64]*meta{}
	var order []int64

	itemSQL := `SELECT mi.id, mi.title, coalesce(mi.description,'')
		FROM media_item_cards mi WHERE mi.media_type IN ('movie','tv','anime')`
	rows, err := a.db.Query(ctx, itemSQL)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		m := &meta{}
		var id int64
		if err := rows.Scan(&id, &m.title, &m.desc); err != nil {
			rows.Close()
			return nil, err
		}
		items[id] = m
		order = append(order, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}

	// genres (text[]) from movies + tv_series
	for _, tbl := range []string{"movies", "tv_series"} {
		gr, err := a.db.Query(ctx, `SELECT media_item_id, genres FROM `+tbl+` WHERE genres IS NOT NULL`)
		if err != nil {
			return nil, err
		}
		for gr.Next() {
			var id int64
			var gs []string
			if err := gr.Scan(&id, &gs); err != nil {
				gr.Close()
				return nil, err
			}
			if m := items[id]; m != nil {
				m.genres = append(m.genres, gs...)
			}
		}
		gr.Close()
		if err := gr.Err(); err != nil {
			return nil, err
		}
	}
	appendName := func(sql string, pick func(*meta) *[]string) error {
		r, err := a.db.Query(ctx, sql)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var id int64
			var name string
			if err := r.Scan(&id, &name); err != nil {
				return err
			}
			if m := items[id]; m != nil {
				*pick(m) = append(*pick(m), name)
			}
		}
		return r.Err()
	}
	// Deterministic ordering matters: the doc text is hashed for staleness
	// detection, so row-order jitter would read as a metadata change and
	// churn re-embeds.
	if err := appendName(
		`SELECT mk.media_item_id, k.name FROM media_keywords mk JOIN keywords k ON k.id = mk.keyword_id
		 ORDER BY mk.media_item_id, k.name`,
		func(m *meta) *[]string { return &m.kw }); err != nil {
		return nil, err
	}
	if err := appendName(
		`SELECT media_item_id, name FROM (
			SELECT mc.media_item_id, p.name, row_number() OVER (PARTITION BY mc.media_item_id ORDER BY mc.display_order, p.name) rn
			FROM media_cast mc JOIN people p ON p.id = mc.person_id) s WHERE rn <= 6
		 ORDER BY media_item_id, rn`,
		func(m *meta) *[]string { return &m.cast }); err != nil {
		return nil, err
	}

	out := make([]embedDocRow, 0, len(order))
	for _, id := range order {
		m := items[id]
		out = append(out, embedDocRow{id: id, doc: embedDoc(m.title, m.genres, m.kw, m.cast, m.desc)})
	}
	return out, nil
}

// loadEpisodeEmbedDocs builds one doc per TV/anime episode that has an
// overview: "Series S02E05 — Episode Title. <overview>". The series title
// anchors the episode text to its show, so a probe naming the show still
// matches; the overview carries the plot-specific text the series blurb omits.
// Staleness is decided by the caller via doc hashes.
func (a *App) loadEpisodeEmbedDocs(ctx context.Context) ([]embedDocRow, error) {
	q := `SELECT e.id, mi.title, se.season_number, e.episode_number, e.title, e.overview
		FROM tv_episodes e
		JOIN tv_seasons se ON se.id = e.season_id
		JOIN tv_series ts ON ts.id = se.series_id
		JOIN media_item_cards mi ON mi.id = ts.media_item_id
		WHERE e.overview <> ''`
	rows, err := a.db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []embedDocRow
	for rows.Next() {
		var id int64
		var series, epTitle, overview string
		var season, epnum int
		if err := rows.Scan(&id, &series, &season, &epnum, &epTitle, &overview); err != nil {
			return nil, err
		}
		if len(overview) > 500 {
			overview = overview[:500]
		}
		doc := fmt.Sprintf("%s S%02dE%02d", series, season, epnum)
		if epTitle != "" {
			doc += " — " + epTitle
		}
		doc += ". " + overview
		out = append(out, embedDocRow{id: id, doc: doc})
	}
	return out, rows.Err()
}

// backfillEmbeddingsForTask is the scheduled-task entry point
// (kickoff_embed_recommendations): a disabled or still-downloading engine is
// a clean no-op there, not a failure.
func (a *App) backfillEmbeddingsForTask(ctx context.Context, force bool) (int, int, error) {
	embedded, skipped, err := a.BackfillVideoEmbeddings(ctx, force)
	if errors.Is(err, ErrMLDisabled) {
		return 0, 0, nil
	}
	return embedded, skipped, err
}

// embedDocHash fingerprints the exact text a facet row embedded, so the
// incremental backfill can detect source-metadata changes (refresh,
// re-identify, edited overviews) without an embedder_version bump.
func embedDocHash(doc string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(doc))
	return strconv.FormatUint(h.Sum64(), 16)
}

type facetState struct {
	version int32
	hash    string
}

// loadFacetStates reads (embedder_version, doc_hash) for every row of a facet
// table, keyed by its id column — the staleness baseline for the backfill.
func (a *App) loadFacetStates(ctx context.Context, table, keyCol string) (map[int64]facetState, error) {
	rows, err := a.db.Query(ctx, `SELECT `+keyCol+`, embedder_version, doc_hash FROM `+table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]facetState{}
	for rows.Next() {
		var id int64
		var st facetState
		if err := rows.Scan(&id, &st.version, &st.hash); err != nil {
			return nil, err
		}
		out[id] = st
	}
	return out, rows.Err()
}

// BackfillVideoEmbeddings embeds every video item, episode, and canonical music
// recording whose embedding
// is missing or stale — wrong embedder_version, or a doc_hash mismatch after
// the source metadata changed (or everything when force) — upserting
// media_item_facets / episode_facets. Returns the count embedded and skipped
// (tokenizer failures). Requires the engine enabled.
func (a *App) BackfillVideoEmbeddings(ctx context.Context, force bool) (embedded, skipped int, err error) {
	emb, err := a.recEmbedderInstance(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("load embedder: %w", err)
	}
	if emb == nil {
		return 0, 0, ErrMLDisabled
	}
	// Upgrade/backstop path for libraries enriched before the semantic music
	// catalog existed. Artist refresh handles new data; each embedding sweep
	// hydrates a bounded batch of already-known canonical recording IDs.
	if a.matcher != nil {
		if hydrated, hydrateErr := a.matcher.HydrateMissingMusicSemanticCatalog(ctx, 500); hydrateErr != nil {
			log.Warn().Err(hydrateErr).Msg("recommendations: bootstrap music semantic catalog failed")
		} else if hydrated > 0 {
			log.Info().Int("recordings", hydrated).Msg("recommendations: bootstrapped music semantic catalog")
		}
	}
	process := func(table, keyCol string, docs []embedDocRow) error {
		states, err := a.loadFacetStates(ctx, table, keyCol)
		if err != nil {
			return fmt.Errorf("load %s states: %w", table, err)
		}
		for _, d := range docs {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			hash := embedDocHash(d.doc)
			if st, ok := states[d.id]; !force && ok && st.version >= textembed.Version && st.hash == hash {
				continue // current version, unchanged source text
			}
			vec, err := emb.Embed(d.doc)
			if err != nil {
				skipped++ // tokenizer edge case — skip this doc, keep going
				continue
			}
			if _, err := a.db.Exec(ctx,
				`INSERT INTO `+table+` (`+keyCol+`, text_embedding, embedder_version, doc_hash, embedded_at)
				 VALUES ($1, $2, $3, $4, now())
				 ON CONFLICT (`+keyCol+`) DO UPDATE
				   SET text_embedding = EXCLUDED.text_embedding, embedder_version = EXCLUDED.embedder_version,
				       doc_hash = EXCLUDED.doc_hash, embedded_at = now()`,
				d.id, pgvector.NewVector(vec), textembed.Version, hash); err != nil {
				return fmt.Errorf("upsert %s %d: %w", table, d.id, err)
			}
			embedded++
		}
		return nil
	}
	processMusic := func(docs []musicEmbedDocRow) error {
		rows, err := a.db.Query(ctx, `SELECT recording_entity_id, embedder_version, doc_hash FROM music_recording_facets`)
		if err != nil {
			return fmt.Errorf("load music recording facet states: %w", err)
		}
		states := map[uuid.UUID]facetState{}
		for rows.Next() {
			var id uuid.UUID
			var state facetState
			if err := rows.Scan(&id, &state.version, &state.hash); err != nil {
				rows.Close()
				return err
			}
			states[id] = state
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, d := range docs {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			hash := embedDocHash(d.doc)
			if state, ok := states[d.id]; !force && ok && state.version >= textembed.Version && state.hash == hash {
				continue
			}
			vec, err := emb.Embed(d.doc)
			if err != nil {
				skipped++
				continue
			}
			if _, err := a.db.Exec(ctx, `
				INSERT INTO music_recording_facets (recording_entity_id, text_embedding, embedder_version, doc_hash, embedded_at)
				VALUES ($1, $2, $3, $4, now())
				ON CONFLICT (recording_entity_id) DO UPDATE SET
				  text_embedding = EXCLUDED.text_embedding,
				  embedder_version = EXCLUDED.embedder_version,
				  doc_hash = EXCLUDED.doc_hash,
				  embedded_at = now()`, d.id, pgvector.NewVector(vec), textembed.Version, hash); err != nil {
				return fmt.Errorf("upsert music_recording_facets %s: %w", d.id, err)
			}
			embedded++
		}
		return nil
	}

	docs, err := a.loadVideoEmbedDocs(ctx)
	if err != nil {
		return embedded, skipped, fmt.Errorf("load docs: %w", err)
	}
	if err := process("media_item_facets", "media_item_id", docs); err != nil {
		return embedded, skipped, err
	}
	epDocs, err := a.loadEpisodeEmbedDocs(ctx)
	if err != nil {
		return embedded, skipped, fmt.Errorf("load episode docs: %w", err)
	}
	if err := process("episode_facets", "episode_id", epDocs); err != nil {
		return embedded, skipped, err
	}
	musicDocs, err := a.loadMusicEmbedDocs(ctx)
	if err != nil {
		return embedded, skipped, fmt.Errorf("load music docs: %w", err)
	}
	if err := processMusic(musicDocs); err != nil {
		return embedded, skipped, err
	}

	// Prune facets whose source left the candidate set (episode overview
	// cleared, item media_type changed — deletes cascade on their own).
	// Without this the orphaned vector keeps matching semantic searches
	// with text that no longer exists anywhere.
	if err := a.pruneOrphanedFacets(ctx); err != nil {
		return embedded, skipped, fmt.Errorf("prune orphaned facets: %w", err)
	}
	return embedded, skipped, nil
}

// pruneOrphanedFacets deletes embedding rows that no longer have a doc: rows
// re-enter via the normal backfill if their source ever qualifies again.
func (a *App) pruneOrphanedFacets(ctx context.Context) error {
	itemTag, err := a.db.Exec(ctx, `
		DELETE FROM media_item_facets f
		WHERE NOT EXISTS (
			SELECT 1 FROM media_item_cards mi
			WHERE mi.id = f.media_item_id AND mi.media_type IN ('movie','tv','anime'))`)
	if err != nil {
		return err
	}
	epTag, err := a.db.Exec(ctx, `
		DELETE FROM episode_facets f
		WHERE NOT EXISTS (
			SELECT 1 FROM tv_episodes e
			WHERE e.id = f.episode_id AND e.overview <> '')`)
	if err != nil {
		return err
	}
	musicTag, err := a.db.Exec(ctx, `
		DELETE FROM music_recording_facets f
		WHERE NOT EXISTS (
			SELECT 1 FROM music_catalog_recordings r
			WHERE r.recording_entity_id = f.recording_entity_id
			  AND cardinality(r.genres) + cardinality(r.tags) + cardinality(r.moods) +
			      cardinality(r.instrumentation) + cardinality(r.vocal_characteristics) +
			      cardinality(r.recording_attributes) > 0)`)
	if err != nil {
		return err
	}
	if n := itemTag.RowsAffected() + epTag.RowsAffected() + musicTag.RowsAffected(); n > 0 {
		log.Info().Int64("pruned", n).Msg("recommendations: pruned orphaned embedding facets")
	}
	return nil
}

// EmbeddedVideoCount reports how many video items carry a current-version
// embedding, and the total candidate count — for the settings-page progress.
// embedded joins against the candidate set (not raw facet rows) so it can
// never exceed total even while orphaned facets await the sweep's prune.
func (a *App) EmbeddedVideoCount(ctx context.Context) (embedded, total int) {
	_ = a.db.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE f.media_item_id IS NOT NULL)
		FROM media_item_cards mi
		LEFT JOIN media_item_facets f ON f.media_item_id = mi.id AND f.embedder_version >= $1
		WHERE mi.media_type IN ('movie','tv','anime')`, textembed.Version).Scan(&total, &embedded)
	return
}

// EmbeddedEpisodeCount is the episode-level twin of EmbeddedVideoCount —
// only episodes with a non-empty overview count as candidates.
func (a *App) EmbeddedEpisodeCount(ctx context.Context) (embedded, total int) {
	_ = a.db.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE f.episode_id IS NOT NULL)
		FROM tv_episodes e
		LEFT JOIN episode_facets f ON f.episode_id = e.id AND f.embedder_version >= $1
		WHERE e.overview <> ''`, textembed.Version).Scan(&total, &embedded)
	return
}

// EmbeddedMusicCount reports focused metadata embedding coverage for canonical
// recordings, including external top tracks with no local file.
func (a *App) EmbeddedMusicCount(ctx context.Context) (embedded, total int) {
	q := sqlc.New(a.db)
	totalCount, _ := q.CountMusicCatalogRecordings(ctx)
	embeddedCount, _ := q.CountEmbeddedMusicCatalogRecordings(ctx, int32(textembed.Version))
	total = int(totalCount)
	embedded = int(embeddedCount)
	return
}

// fyEmbedScores returns a per-candidate embedding similarity (cosine to the
// user's taste centroid) for the For You blend, or nil when the ML engine is off
// / has no usable seed embeddings — the non-ML blend then proceeds unchanged.
func (a *App) fyEmbedScores(ctx context.Context, seedW map[int64]float64) map[int64]float64 {
	if !a.RecommendationsMLEnabled(ctx) || len(seedW) == 0 {
		return nil
	}
	seedIDs := make([]int64, 0, len(seedW))
	for id := range seedW {
		seedIDs = append(seedIDs, id)
	}
	rows, err := a.db.Query(ctx,
		`SELECT media_item_id, text_embedding FROM media_item_facets
		 WHERE media_item_id = ANY($1) AND text_embedding IS NOT NULL AND embedder_version >= $2`,
		seedIDs, int32(textembed.Version))
	if err != nil {
		return nil
	}
	centroid := make([]float32, textembed.Dim)
	var tot float64
	for rows.Next() {
		var id int64
		var v pgvector.Vector
		if err := rows.Scan(&id, &v); err != nil {
			rows.Close()
			return nil
		}
		s := v.Slice()
		if len(s) != textembed.Dim {
			continue
		}
		w := float32(seedW[id])
		for i := range centroid {
			centroid[i] += w * s[i]
		}
		tot += seedW[id]
	}
	rows.Close()
	if tot == 0 {
		return nil
	}
	l2normVec(centroid)

	// Cosine distance for every embedded item, computed in the DB.
	dr, err := a.db.Query(ctx,
		`SELECT media_item_id, (text_embedding <=> $1)::float8 FROM media_item_facets
		 WHERE text_embedding IS NOT NULL AND embedder_version >= $2`,
		pgvector.NewVector(centroid), int32(textembed.Version))
	if err != nil {
		return nil
	}
	defer dr.Close()
	out := map[int64]float64{}
	for dr.Next() {
		var id int64
		var dist float64
		if err := dr.Scan(&id, &dist); err != nil {
			return nil
		}
		out[id] = 1 - dist // cosine similarity
	}
	return out
}

func l2normVec(v []float32) {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	if s == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(s))
	for i := range v {
		v[i] *= inv
	}
}

// SemanticSearch embeds a natural-language query and returns the nearest library
// items by cosine distance. Facets (type/genre/keyword/min_rating) still apply.
// Two sources feed the ranking: series/movie docs (media_item_facets) and
// per-episode overview docs (episode_facets, resolved up to their series) —
// a series whose single episode nails the ask surfaces even when its
// spoiler-safe blurb says nothing. Episode-driven hits carry a "Matched
// S02E05 …" reason as evidence.
func (a *App) SemanticSearch(ctx context.Context, query string, facets ForYouFacets) ([]ForYouItem, error) {
	emb, err := a.recEmbedderInstance(ctx)
	if err != nil {
		return nil, fmt.Errorf("load embedder: %w", err)
	}
	if emb == nil {
		return nil, ErrMLDisabled
	}
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	qv, err := emb.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	limit := facets.Limit
	if limit <= 0 {
		limit = 40
	}
	if limit > 100 {
		limit = 100
	}

	// KNN over the HNSW cosine index, gated to available titles + the facets.
	rows, err := a.db.Query(ctx, `
		SELECT mi.id, mi.public_id, mi.library_id, mi.title, mi.slug, coalesce(mi.year,''), mi.media_type::text,
		       coalesce(m.rating, ts.rating, 0)::float8,
		       (f.text_embedding <=> $1)::float8 AS dist
		FROM media_item_facets f
		JOIN media_item_cards mi ON mi.id = f.media_item_id
		LEFT JOIN movies m     ON m.media_item_id  = mi.id
		LEFT JOIN tv_series ts ON ts.media_item_id = mi.id
		WHERE f.text_embedding IS NOT NULL
		  AND f.embedder_version >= $4
		  AND EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL)
		  AND ($2 = '' OR mi.media_type::text = $2 OR ($2 = 'tv' AND mi.media_type = 'anime'))
		ORDER BY f.text_embedding <=> $1
		LIMIT $3`,
		pgvector.NewVector(qv), facets.Type, limit*3, int32(textembed.Version)) // over-fetch for post-filtering
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := map[int64]ForYouItem{}
	var order []int64
	for rows.Next() {
		var it ForYouItem
		var dist float64
		var publicID uuid.UUID
		if err := rows.Scan(&it.ID, &publicID, &it.libraryID, &it.Title, &it.Slug, &it.Year, &it.MediaType, &it.Rating, &dist); err != nil {
			return nil, err
		}
		it.PublicID = publicID.String()
		if facets.MinRating > 0 && it.Rating < facets.MinRating {
			continue
		}
		it.Available = true
		it.Score = round3(1 - dist) // cosine similarity
		byID[it.ID] = it
		order = append(order, it.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Episode-level pass — movies have no episodes, skip the query entirely.
	if facets.Type != "movie" {
		if err := a.semanticEpisodeMerge(ctx, qv, facets, limit*3, byID, &order); err != nil {
			return nil, err
		}
	}

	items := make([]ForYouItem, 0, limit)
	for _, id := range order {
		items = append(items, byID[id])
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Score > items[j].Score })
	if len(items) > int(limit) {
		items = items[:limit]
	}
	a.fyLocalizeTitles(ctx, items)
	return items, nil
}

// semanticEpisodeMerge KNNs the query over episode_facets and folds the hits
// into the series-level candidate set: a series enters (or improves its score)
// via its best-matching episode, tagged with which episode matched.
func (a *App) semanticEpisodeMerge(ctx context.Context, qv []float32, facets ForYouFacets, fetch int32, byID map[int64]ForYouItem, order *[]int64) error {
	rows, err := a.db.Query(ctx, `
		SELECT mi.id, mi.public_id, mi.library_id, mi.title, mi.slug, coalesce(mi.year,''), mi.media_type::text,
		       coalesce(ts.rating, 0)::float8,
		       e.id, se.season_number, e.episode_number, e.title,
		       (f.text_embedding <=> $1)::float8 AS dist
		FROM episode_facets f
		JOIN tv_episodes e ON e.id = f.episode_id
		JOIN tv_seasons se ON se.id = e.season_id
		JOIN tv_series ts ON ts.id = se.series_id
		JOIN media_item_cards mi ON mi.id = ts.media_item_id
		WHERE f.text_embedding IS NOT NULL
		  AND f.embedder_version >= $4
		  AND EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL)
		  AND ($2 = '' OR mi.media_type::text = $2 OR ($2 = 'tv' AND mi.media_type = 'anime'))
		ORDER BY f.text_embedding <=> $1
		LIMIT $3`,
		pgvector.NewVector(qv), facets.Type, fetch, int32(textembed.Version))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var it ForYouItem
		var dist float64
		var publicID uuid.UUID
		var episodeID int64
		var season, epnum int
		var epTitle string
		if err := rows.Scan(&it.ID, &publicID, &it.libraryID, &it.Title, &it.Slug, &it.Year, &it.MediaType, &it.Rating,
			&episodeID, &season, &epnum, &epTitle, &dist); err != nil {
			return err
		}
		if facets.MinRating > 0 && it.Rating < facets.MinRating {
			continue
		}
		score := round3(1 - dist)
		if prev, ok := byID[it.ID]; ok {
			if score > prev.Score {
				prev.Score = score
				prev.Reason = matchedEpisodeReason(season, epnum, epTitle)
				prev.matchedEpisodeID = episodeID
				byID[it.ID] = prev
			}
			continue
		}
		it.PublicID = publicID.String()
		it.Available = true
		it.Score = score
		it.Reason = matchedEpisodeReason(season, epnum, epTitle)
		it.matchedEpisodeID = episodeID
		byID[it.ID] = it
		*order = append(*order, it.ID)
	}
	return rows.Err()
}

func matchedEpisodeReason(season, epnum int, title string) string {
	r := fmt.Sprintf("Matched S%02dE%02d", season, epnum)
	if title != "" {
		r += " — “" + title + "”"
	}
	return r
}
