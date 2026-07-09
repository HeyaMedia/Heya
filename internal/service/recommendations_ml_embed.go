package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

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

// BackfillVideoEmbeddings embeds every video item that lacks a current-version
// embedding (or all items when force) and upserts media_item_facets. Returns the
// count embedded and skipped (tokenizer failures). Requires the engine enabled.
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
	for _, d := range docs {
		if ctx.Err() != nil {
			return embedded, skipped, ctx.Err()
		}
		vec, err := emb.Embed(d.doc)
		if err != nil {
			skipped++ // tokenizer edge case — skip this item, keep going
			continue
		}
		if _, err := a.db.Exec(ctx,
			`INSERT INTO media_item_facets (media_item_id, text_embedding, embedder_version, embedded_at)
			 VALUES ($1, $2, $3, now())
			 ON CONFLICT (media_item_id) DO UPDATE
			   SET text_embedding = EXCLUDED.text_embedding, embedder_version = EXCLUDED.embedder_version, embedded_at = now()`,
			d.id, pgvector.NewVector(vec), textembed.Version); err != nil {
			return embedded, skipped, fmt.Errorf("upsert %d: %w", d.id, err)
		}
		embedded++
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
		SELECT mi.id, mi.library_id, mi.title, mi.slug, coalesce(mi.year,''), mi.media_type::text,
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

	items := make([]ForYouItem, 0, limit)
	for rows.Next() {
		var it ForYouItem
		var dist float64
		if err := rows.Scan(&it.ID, &it.libraryID, &it.Title, &it.Slug, &it.Year, &it.MediaType, &it.Rating, &dist); err != nil {
			return nil, err
		}
		if facets.MinRating > 0 && it.Rating < facets.MinRating {
			continue
		}
		it.Available = true
		it.Score = round3(1 - dist) // cosine similarity
		if len(items) < int(limit) {
			items = append(items, it)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	a.fyLocalizeTitles(ctx, items)
	return items, nil
}
