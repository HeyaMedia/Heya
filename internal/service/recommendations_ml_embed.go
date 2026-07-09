package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/karbowiak/heya/internal/textembed"
	"github.com/pgvector/pgvector-go"
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

// loadVideoEmbedDocs builds the embed doc for video items. When onlyStale is set
// it returns only items missing a current-version embedding (the incremental case).
func (a *App) loadVideoEmbedDocs(ctx context.Context, onlyStale bool) ([]embedDocRow, error) {
	type meta struct {
		title, desc      string
		genres, kw, cast []string
	}
	items := map[int64]*meta{}
	var order []int64

	itemSQL := `SELECT mi.id, mi.title, coalesce(mi.description,'')
		FROM media_item_cards mi WHERE mi.media_type IN ('movie','tv','anime')`
	if onlyStale {
		itemSQL += ` AND NOT EXISTS (SELECT 1 FROM media_item_facets f
			WHERE f.media_item_id = mi.id AND f.embedder_version >= ` + strconv.Itoa(textembed.Version) + `)`
	}
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
	if err := appendName(
		`SELECT mk.media_item_id, k.name FROM media_keywords mk JOIN keywords k ON k.id = mk.keyword_id`,
		func(m *meta) *[]string { return &m.kw }); err != nil {
		return nil, err
	}
	if err := appendName(
		`SELECT media_item_id, name FROM (
			SELECT mc.media_item_id, p.name, row_number() OVER (PARTITION BY mc.media_item_id ORDER BY mc.display_order) rn
			FROM media_cast mc JOIN people p ON p.id = mc.person_id) s WHERE rn <= 6`,
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
func (a *App) loadEpisodeEmbedDocs(ctx context.Context, onlyStale bool) ([]embedDocRow, error) {
	q := `SELECT e.id, mi.title, se.season_number, e.episode_number, e.title, e.overview
		FROM tv_episodes e
		JOIN tv_seasons se ON se.id = e.season_id
		JOIN tv_series ts ON ts.id = se.series_id
		JOIN media_item_cards mi ON mi.id = ts.media_item_id
		WHERE e.overview <> ''`
	if onlyStale {
		q += ` AND NOT EXISTS (SELECT 1 FROM episode_facets f
			WHERE f.episode_id = e.id AND f.embedder_version >= ` + strconv.Itoa(textembed.Version) + `)`
	}
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

// BackfillVideoEmbeddings embeds every video item AND episode that lacks a
// current-version embedding (or everything when force), upserting
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
	docs, err := a.loadVideoEmbedDocs(ctx, !force)
	if err != nil {
		return 0, 0, fmt.Errorf("load docs: %w", err)
	}
	upsert := func(table, keyCol string, d embedDocRow) error {
		vec, err := emb.Embed(d.doc)
		if err != nil {
			skipped++ // tokenizer edge case — skip this doc, keep going
			return nil
		}
		if _, err := a.db.Exec(ctx,
			`INSERT INTO `+table+` (`+keyCol+`, text_embedding, embedder_version, embedded_at)
			 VALUES ($1, $2, $3, now())
			 ON CONFLICT (`+keyCol+`) DO UPDATE
			   SET text_embedding = EXCLUDED.text_embedding, embedder_version = EXCLUDED.embedder_version, embedded_at = now()`,
			d.id, pgvector.NewVector(vec), textembed.Version); err != nil {
			return fmt.Errorf("upsert %s %d: %w", table, d.id, err)
		}
		embedded++
		return nil
	}
	for _, d := range docs {
		if ctx.Err() != nil {
			return embedded, skipped, ctx.Err()
		}
		if err := upsert("media_item_facets", "media_item_id", d); err != nil {
			return embedded, skipped, err
		}
	}
	epDocs, err := a.loadEpisodeEmbedDocs(ctx, !force)
	if err != nil {
		return embedded, skipped, fmt.Errorf("load episode docs: %w", err)
	}
	for _, d := range epDocs {
		if ctx.Err() != nil {
			return embedded, skipped, ctx.Err()
		}
		if err := upsert("episode_facets", "episode_id", d); err != nil {
			return embedded, skipped, err
		}
	}
	return embedded, skipped, nil
}

// EmbeddedVideoCount reports how many video items carry a current-version
// embedding, and the total candidate count — for the settings-page progress.
func (a *App) EmbeddedVideoCount(ctx context.Context) (embedded, total int) {
	_ = a.db.QueryRow(ctx,
		`SELECT count(*) FROM media_item_facets WHERE embedder_version >= $1`, textembed.Version).Scan(&embedded)
	_ = a.db.QueryRow(ctx,
		`SELECT count(*) FROM media_item_cards WHERE media_type IN ('movie','tv','anime')`).Scan(&total)
	return
}

// EmbeddedEpisodeCount is the episode-level twin of EmbeddedVideoCount —
// only episodes with a non-empty overview count as candidates.
func (a *App) EmbeddedEpisodeCount(ctx context.Context) (embedded, total int) {
	_ = a.db.QueryRow(ctx,
		`SELECT count(*) FROM episode_facets WHERE embedder_version >= $1`, textembed.Version).Scan(&embedded)
	_ = a.db.QueryRow(ctx,
		`SELECT count(*) FROM tv_episodes WHERE overview <> ''`).Scan(&total)
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
		`SELECT media_item_id, text_embedding FROM media_item_facets WHERE media_item_id = ANY($1) AND text_embedding IS NOT NULL`, seedIDs)
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
		`SELECT media_item_id, (text_embedding <=> $1)::float8 FROM media_item_facets WHERE text_embedding IS NOT NULL`,
		pgvector.NewVector(centroid))
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
		  AND EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL)
		  AND ($2 = '' OR mi.media_type::text = $2 OR ($2 = 'tv' AND mi.media_type = 'anime'))
		ORDER BY f.text_embedding <=> $1
		LIMIT $3`,
		pgvector.NewVector(qv), facets.Type, int32(limit)*3) // over-fetch for post-filtering
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
		if err := a.semanticEpisodeMerge(ctx, qv, facets, int32(limit)*3, byID, &order); err != nil {
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
		  AND EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL)
		  AND ($2 = '' OR mi.media_type::text = $2 OR ($2 = 'tv' AND mi.media_type = 'anime'))
		ORDER BY f.text_embedding <=> $1
		LIMIT $3`,
		pgvector.NewVector(qv), facets.Type, fetch)
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
