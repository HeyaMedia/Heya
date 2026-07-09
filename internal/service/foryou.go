package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// ForYou is the personalized, non-ML recommendation engine. It scores unwatched
// library titles with two blended engines and returns a ranked, steerable list:
//
//	content — TF-IDF cosine over genres / keywords / top-cast / director
//	graph   — spreading activation over the TMDB media_recommendations edges
//
// plus a mild external-rating prior, MMR diversity, and a human reason per pick.
// It reads only existing tables — no schema changes, no ML. The embedding engine
// (Phase 2) plugs in as a third scorer behind a config flag; this stays the
// always-available baseline.

// ---- tuning knobs (mirror the validated cmd/recexp prototype) ----
const (
	fyHeart   = 3.0 // hearting a movie/series — the strongest signal
	fyMovie   = 1.0 // completing a movie
	fyEpBase  = 1.0 // base weight for a series with ≥1 completed episode
	fyEpScale = 0.5 // + fyEpScale*ln(1+episodes) — reward binge commitment
	fyEpCap   = 2.5 // stays BELOW fyHeart: a fully-watched-but-unhearted show never outranks a heart

	fyFWGenre    = 0.7 // genres are coarse
	fyFWKeyword  = 1.0 // keywords are the richest signal
	fyFWCast     = 0.6
	fyFWDirector = 1.1 // director is a strong auteur signal
	fyTopCast    = 10  // only top-billed cast count as tags

	fyAlpha = 0.60 // content engine weight
	fyBeta  = 0.35 // graph engine weight
	fyGamma = 0.05 // external-rating prior
	fyDelta = 0.30 // embedding-similarity term — added only when the ML engine is on

	// Broad "pseudo-genre" tags (e.g. "based on manga") appear in nearly every
	// anime a fan likes, so their profile weight grows with seed COUNT and drowns
	// specific keywords. sqrt(sum) makes accumulation sub-linear; the cap stops a
	// single broad tag from carrying a candidate alone.
	fyProfileCap = 3.2

	fyMMRLambda = 0.70 // MMR: relevance vs diversity
	fyMMRPool   = 200  // re-rank only the top-N by blended score

	fyCatalogTTL = 5 * time.Minute
)

// ForYouFacets steers the engine. Empty fields impose no constraint; set ones
// hard-filter the candidate pool, and the engine ranks by taste WITHIN it
// ("I'm on a horror binge" → Genre:"Horror").
type ForYouFacets struct {
	Type      string  // "", "movie", "tv" (incl. anime), "anime"
	Genre     string  // exact genre tag
	Keyword   string  // exact keyword/tag
	MinRating float64 // external-rating floor
	Limit     int32   // result size (default 20, max 100)
	Mode      string  // reserved for future ranking strategies
}

// ForYouItem is one ranked recommendation — the same shape as a discovery-rail
// tile (id for poster lookup, slug+media_type for the URL) plus a score and a
// human-readable reason.
type ForYouItem struct {
	ID        int64   `json:"id"`
	PublicID  string  `json:"public_id,omitempty"`
	Title     string  `json:"title"`
	Slug      string  `json:"slug"`
	Year      string  `json:"year,omitempty"`
	MediaType string  `json:"media_type"`
	Rating    float64 `json:"rating,omitempty"`
	Reason    string  `json:"reason,omitempty"`
	Score     float64 `json:"score"`
	Available bool    `json:"available"`

	libraryID int64 // carried for batched title localization; never serialized
	// matchedEpisodeID is set when the item surfaced via an episode-embedding
	// hit (semantic search) — the AI re-ranker fetches that episode's overview
	// as grading evidence. Never serialized.
	matchedEpisodeID int64
}

// AcquireItem is a highly-recommended title the user does NOT own — the seed for
// a future "add to library" flow. External-only, so keyed by TMDB id.
type AcquireItem struct {
	TmdbID    string  `json:"tmdb_id"`
	Title     string  `json:"title"`
	MediaType string  `json:"media_type"`
	Score     float64 `json:"score"`
}

// ForYouResult is the whole ranked answer. HasSignal is false for a cold-start
// account (no hearts/watches) — the engine then falls back to a quality ranking.
type ForYouResult struct {
	Items     []ForYouItem  `json:"items"`
	Acquire   []AcquireItem `json:"acquire,omitempty"`
	HasSignal bool          `json:"has_signal"`
}

// ForYou builds the ranked recommendation list for a user, steered by facets.
func (a *App) ForYou(ctx context.Context, userID int64, facets ForYouFacets) (ForYouResult, error) {
	limit := facets.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	idx, err := a.fyCatalogIndex(ctx)
	if err != nil {
		return ForYouResult{}, fmt.Errorf("build catalog index: %w", err)
	}

	seedW, _, err := a.fyLoadSeeds(ctx, userID) // seed reasons feed the CLI, not the API result
	if err != nil {
		return ForYouResult{}, fmt.Errorf("load seeds: %w", err)
	}
	hasSignal := len(seedW) > 0

	// taste profile: sub-linear + capped so broad tags don't dominate.
	profileRaw := map[string]float64{}
	for id, w := range seedW {
		for k := range idx.tags[id] {
			profileRaw[k] += w
		}
	}
	profile := make(map[string]float64, len(profileRaw))
	for k, raw := range profileRaw {
		e := math.Sqrt(raw)
		if e > fyProfileCap {
			e = fyProfileCap
		}
		profile[k] = e
	}

	graphScore, graphTopSeed, acquire, err := a.fyLoadGraph(ctx, seedW, idx, facets)
	if err != nil {
		return ForYouResult{}, fmt.Errorf("load graph: %w", err)
	}

	// Optional 3rd signal: embedding similarity to the taste centroid. nil when
	// the ML engine is off — the blend then uses content + graph + quality only.
	embedScores := a.fyEmbedScores(ctx, seedW)

	// score candidates (available, unwatched, not a seed, facet-matched)
	var cands []*fyScored
	for id, it := range idx.items {
		if !it.available || seedW[id] != 0 || !fyFacetMatch(facets, it, idx.tags[id]) {
			continue
		}
		var dot, norm2 float64
		bestTag, bestContrib := "", 0.0
		for k := range idx.tags[id] {
			w := idx.vw[k]
			norm2 += w * w
			if p := profile[k]; p > 0 {
				c := p * w * w
				dot += c
				if c > bestContrib {
					bestContrib, bestTag = c, k
				}
			}
		}
		content := 0.0
		if norm2 > 0 {
			content = dot / math.Sqrt(norm2)
		}
		cands = append(cands, &fyScored{it: it, content: content, graph: graphScore[id], embed: embedScores[id], contentTopTag: bestTag})
	}

	// normalize each engine to [0,1], then blend
	var maxC, maxG, maxE float64
	for _, s := range cands {
		maxC = math.Max(maxC, s.content)
		maxG = math.Max(maxG, s.graph)
		maxE = math.Max(maxE, s.embed)
	}
	for _, s := range cands {
		cN, gN, eN := 0.0, 0.0, 0.0
		if maxC > 0 {
			cN = s.content / maxC
		}
		if maxG > 0 {
			gN = s.graph / maxG
		}
		if maxE > 0 {
			eN = s.embed / maxE
		}
		s.content, s.graph, s.embed = cN, gN, eN
		s.final = fyAlpha*cN + fyBeta*gN + fyGamma*(s.it.rating/10)
		if embedScores != nil {
			s.final += fyDelta * eN
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].final > cands[j].final })

	picked := a.fyDiversify(cands, idx, graphTopSeed, int(limit))

	// resolve person names for any cast/director reasons in the final set
	names := a.fyResolveNames(ctx, picked)

	items := make([]ForYouItem, 0, len(picked))
	for _, s := range picked {
		items = append(items, ForYouItem{
			ID: s.it.id, PublicID: s.it.publicID, Title: s.it.title, Slug: s.it.slug, Year: s.it.year,
			MediaType: s.it.mediaType, Rating: s.it.rating, Available: true,
			Score:     round3(s.final),
			Reason:    a.fyReason(s, idx, graphTopSeed, names, hasSignal),
			libraryID: s.it.libraryID,
		})
	}
	a.fyLocalizeTitles(ctx, items)

	if len(acquire) > int(limit) {
		acquire = acquire[:limit]
	}
	return ForYouResult{Items: items, Acquire: acquire, HasSignal: hasSignal}, nil
}

// fyScored is a candidate mid-scoring; content/graph hold normalized [0,1] scores.
type fyScored struct {
	it                    *fyItem
	content, graph, embed float64
	contentTopTag         string
	final                 float64
}

// ---- catalog index (static, cached in-process) -----------------------------

type fyItem struct {
	id        int64
	publicID  string
	libraryID int64
	title     string
	slug      string
	year      string
	mediaType string
	rating    float64
	available bool
}

// fyIndex is the catalog's content fingerprint: per-item tag sets and per-tag
// weights (fieldWeight × idf). It depends only on the library (not the user), so
// it's memoized with a short TTL — this is NOT the Phase-2 per-user cache.
type fyIndex struct {
	items   map[int64]*fyItem
	tags    map[int64]map[string]struct{}
	vw      map[string]float64
	builtAt time.Time
}

var (
	fyIdxMu    sync.Mutex
	fyIdxCache *fyIndex
)

func (a *App) fyCatalogIndex(ctx context.Context) (*fyIndex, error) {
	fyIdxMu.Lock()
	defer fyIdxMu.Unlock()
	if fyIdxCache != nil && time.Since(fyIdxCache.builtAt) < fyCatalogTTL {
		return fyIdxCache, nil
	}
	idx, err := a.fyBuildIndex(ctx)
	if err != nil {
		return nil, err
	}
	fyIdxCache = idx
	return idx, nil
}

func (a *App) fyBuildIndex(ctx context.Context) (*fyIndex, error) {
	idx := &fyIndex{
		items:   map[int64]*fyItem{},
		tags:    map[int64]map[string]struct{}{},
		vw:      map[string]float64{},
		builtAt: time.Now(),
	}

	rows, err := a.db.Query(ctx, `
		SELECT mi.id, mi.public_id, mi.library_id, mi.title, mi.slug, coalesce(mi.year,''), mi.media_type::text,
		       coalesce(m.rating, ts.rating, 0)::float8,
		       EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL) AS available
		FROM media_item_cards mi
		LEFT JOIN movies m     ON m.media_item_id  = mi.id
		LEFT JOIN tv_series ts ON ts.media_item_id = mi.id
		WHERE mi.media_type IN ('movie','tv','anime')`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		it := &fyItem{}
		var publicID uuid.UUID
		if err := rows.Scan(&it.id, &publicID, &it.libraryID, &it.title, &it.slug, &it.year, &it.mediaType, &it.rating, &it.available); err != nil {
			rows.Close()
			return nil, err
		}
		it.publicID = publicID.String()
		idx.items[it.id] = it
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	addTag := func(id int64, key string) {
		if _, ok := idx.items[id]; !ok {
			return
		}
		if idx.tags[id] == nil {
			idx.tags[id] = map[string]struct{}{}
		}
		idx.tags[id][key] = struct{}{}
	}

	// genres (movies + tv_series, both text[])
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
			for _, g := range gs {
				addTag(id, "g:"+g)
			}
		}
		gr.Close()
		if err := gr.Err(); err != nil {
			return nil, err
		}
	}

	if err := a.fyScanPairs(ctx,
		`SELECT mk.media_item_id, k.name FROM media_keywords mk JOIN keywords k ON k.id = mk.keyword_id`,
		func(id int64, v string) { addTag(id, "k:"+v) }); err != nil {
		return nil, err
	}
	if err := a.fyScanIDs(ctx,
		`SELECT media_item_id, person_id FROM (
			SELECT media_item_id, person_id, row_number() OVER (PARTITION BY media_item_id ORDER BY display_order) rn
			FROM media_cast) s WHERE rn <= `+strconv.Itoa(fyTopCast),
		func(id, pid int64) { addTag(id, "c:"+strconv.FormatInt(pid, 10)) }); err != nil {
		return nil, err
	}
	if err := a.fyScanIDs(ctx,
		`SELECT DISTINCT media_item_id, person_id FROM media_crew WHERE department = 'Directing'`,
		func(id, pid int64) { addTag(id, "d:"+strconv.FormatInt(pid, 10)) }); err != nil {
		return nil, err
	}

	// document frequency → tag vector weight (fieldWeight × idf)
	df := map[string]int{}
	for _, ts := range idx.tags {
		for k := range ts {
			df[k]++
		}
	}
	n := float64(len(idx.items))
	for k, d := range df {
		idf := math.Log(n / float64(1+d))
		if idf < 0.01 {
			idf = 0.01
		}
		idx.vw[k] = fyFieldWeight(k) * idf
	}
	return idx, nil
}

// ---- per-user signals ------------------------------------------------------

func (a *App) fyLoadSeeds(ctx context.Context, userID int64) (map[int64]float64, map[int64]string, error) {
	seedW := map[int64]float64{}
	seedReason := map[int64]string{}
	bump := func(id int64, w float64, why string) {
		seedW[id] += w
		if seedReason[id] == "" {
			seedReason[id] = why
		}
	}

	// hearts (favorites). Only media_item level today; season/episode hearts would
	// roll up to their series here once the FE emits them.
	fr, err := a.db.Query(ctx, `SELECT entity_type, entity_id FROM user_favorites WHERE user_id=$1`, userID)
	if err != nil {
		return nil, nil, err
	}
	for fr.Next() {
		var et string
		var eid int64
		if err := fr.Scan(&et, &eid); err != nil {
			fr.Close()
			return nil, nil, err
		}
		if et == "media_item" {
			bump(eid, fyHeart, "hearted")
		}
	}
	fr.Close()
	if err := fr.Err(); err != nil {
		return nil, nil, err
	}

	// completed movies
	if err := a.fyScanOne(ctx,
		`SELECT entity_id FROM user_watch_progress WHERE user_id=$1 AND entity_type='movie' AND completed`,
		func(id int64) { bump(id, fyMovie, "watched") }, userID); err != nil {
		return nil, nil, err
	}

	// completed episodes rolled up to their series
	er, err := a.db.Query(ctx, `
		SELECT ts.media_item_id, count(*)::int AS eps
		FROM user_watch_progress wp
		JOIN tv_episodes te  ON te.id = wp.entity_id
		JOIN tv_seasons  tse ON tse.id = te.season_id
		JOIN tv_series   ts  ON ts.id = tse.series_id
		WHERE wp.user_id=$1 AND wp.entity_type='episode' AND wp.completed
		GROUP BY ts.media_item_id`, userID)
	if err != nil {
		return nil, nil, err
	}
	for er.Next() {
		var sid int64
		var eps int
		if err := er.Scan(&sid, &eps); err != nil {
			er.Close()
			return nil, nil, err
		}
		w := fyEpBase + fyEpScale*math.Log(1+float64(eps))
		if w > fyEpCap {
			w = fyEpCap
		}
		bump(sid, w, fmt.Sprintf("watched %d episodes", eps))
	}
	er.Close()
	if err := er.Err(); err != nil {
		return nil, nil, err
	}
	return seedW, seedReason, nil
}

// fyLoadGraph spreads activation over media_recommendations edges originating at
// the user's seeds. Each seed distributes a fixed budget across its rec-list
// (÷√outdegree) so a recommendation-dense seed can't flood the row. Unresolved
// (not-owned) targets become "acquire" suggestions.
func (a *App) fyLoadGraph(ctx context.Context, seedW map[int64]float64, idx *fyIndex, facets ForYouFacets) (map[int64]float64, map[int64]int64, []AcquireItem, error) {
	if len(seedW) == 0 {
		return map[int64]float64{}, map[int64]int64{}, nil, nil
	}
	seedIDs := make([]int64, 0, len(seedW))
	for id := range seedW {
		seedIDs = append(seedIDs, id)
	}

	type edge struct{ src, dst int64 }
	var edges []edge
	outdeg := map[int64]int{}
	acquire := map[string]*AcquireItem{}

	rows, err := a.db.Query(ctx, `
		SELECT r.media_item_id, r.title, r.media_type, r.external_ids->>'tmdb' AS tmdb, mie.media_item_id AS dst
		FROM media_recommendations r
		LEFT JOIN media_item_external_ids mie
		       ON mie.provider='tmdb' AND mie.external_id = r.external_ids->>'tmdb'
		WHERE r.media_item_id = ANY($1)`, seedIDs)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var src int64
		var title, mtype string
		var tmdb *string
		var dst *int64
		if err := rows.Scan(&src, &title, &mtype, &tmdb, &dst); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		if dst != nil {
			if _, isCand := idx.items[*dst]; isCand && seedW[*dst] == 0 {
				edges = append(edges, edge{src, *dst})
				outdeg[src]++
			}
		} else if tmdb != nil && fyTypeMatch(facets.Type, mtype) {
			e := acquire[*tmdb]
			if e == nil {
				e = &AcquireItem{TmdbID: *tmdb, Title: title, MediaType: mtype}
				acquire[*tmdb] = e
			}
			e.Score += seedW[src]
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}

	graphScore := map[int64]float64{}
	graphTopSeed := map[int64]int64{}
	topW := map[int64]float64{}
	for _, e := range edges {
		w := seedW[e.src] / math.Sqrt(float64(outdeg[e.src]))
		graphScore[e.dst] += w
		if w > topW[e.dst] {
			topW[e.dst] = w
			graphTopSeed[e.dst] = e.src
		}
	}

	acq := make([]AcquireItem, 0, len(acquire))
	for _, e := range acquire {
		e.Score = round3(e.Score)
		acq = append(acq, *e)
	}
	sort.Slice(acq, func(i, j int) bool { return acq[i].Score > acq[j].Score })
	return graphScore, graphTopSeed, acq, nil
}

// ---- diversity, reasons, helpers -------------------------------------------

// fyDiversify greedily re-ranks the top pool with MMR: relevance minus the max
// similarity (tag Jaccard, or "same graph-seed") to already-picked items, so no
// single seed or tight tag-cluster monopolizes the row.
func (a *App) fyDiversify(cands []*fyScored, idx *fyIndex, graphTopSeed map[int64]int64, limit int) []*fyScored {
	pool := cands
	if len(pool) > fyMMRPool {
		pool = pool[:fyMMRPool]
	}
	var picked []*fyScored
	used := map[int64]bool{}
	for len(picked) < limit && len(picked) < len(pool) {
		bestIdx, best := -1, math.Inf(-1)
		for i, c := range pool {
			if used[c.it.id] {
				continue
			}
			sim := 0.0
			for _, s := range picked {
				j := fyJaccard(idx.tags[c.it.id], idx.tags[s.it.id])
				if cs := graphTopSeed[c.it.id]; cs != 0 && cs == graphTopSeed[s.it.id] && j < 0.55 {
					j = 0.55
				}
				if j > sim {
					sim = j
				}
			}
			if m := fyMMRLambda*c.final - (1-fyMMRLambda)*sim; m > best {
				best, bestIdx = m, i
			}
		}
		if bestIdx < 0 {
			break
		}
		used[pool[bestIdx].it.id] = true
		picked = append(picked, pool[bestIdx])
	}
	return picked
}

func (a *App) fyReason(s *fyScored, idx *fyIndex, graphTopSeed map[int64]int64, names map[int64]string, hasSignal bool) string {
	if !hasSignal {
		return "Highly rated in your library"
	}
	if fyBeta*s.graph > fyAlpha*s.content {
		if sid, ok := graphTopSeed[s.it.id]; ok {
			if it := idx.items[sid]; it != nil {
				return "Because you like " + it.title
			}
		}
	}
	if s.contentTopTag != "" {
		return fyPrettyTag(s.contentTopTag, names)
	}
	return ""
}

// fyResolveNames batch-loads the display names for any person tags that will
// surface as a reason in the final set — avoids loading the whole people table.
func (a *App) fyResolveNames(ctx context.Context, picked []*fyScored) map[int64]string {
	var ids []int64
	for _, s := range picked {
		k := s.contentTopTag
		if len(k) > 2 && (k[0] == 'c' || k[0] == 'd') {
			if id, err := strconv.ParseInt(k[2:], 10, 64); err == nil {
				ids = append(ids, id)
			}
		}
	}
	names := map[int64]string{}
	if len(ids) == 0 {
		return names
	}
	rows, err := a.db.Query(ctx, `SELECT id, name FROM people WHERE id = ANY($1)`, ids)
	if err != nil {
		return names
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err == nil {
			names[id] = name
		}
	}
	return names
}

// fyLocalizeTitles overlays each item's title with its library's preferred-
// language variant, matching the rest of the app (important for anime).
func (a *App) fyLocalizeTitles(ctx context.Context, items []ForYouItem) {
	if len(items) == 0 {
		return
	}
	targets := make([]titleTarget, 0, len(items))
	for _, it := range items {
		targets = append(targets, titleTarget{ID: it.ID, LibraryID: it.libraryID})
	}
	overlay := a.preferredTitleOverlayFor(ctx, sqlc.New(a.db), targets)
	for i := range items {
		if t, ok := overlay[items[i].ID]; ok && t != "" {
			items[i].Title = t
		}
	}
}

func fyFacetMatch(f ForYouFacets, it *fyItem, tags map[string]struct{}) bool {
	if !fyTypeMatch(f.Type, it.mediaType) {
		return false
	}
	if f.MinRating > 0 && it.rating < f.MinRating {
		return false
	}
	if f.Genre != "" {
		if _, ok := tags["g:"+f.Genre]; !ok {
			return false
		}
	}
	if f.Keyword != "" {
		if _, ok := tags["k:"+f.Keyword]; !ok {
			return false
		}
	}
	return true
}

func fyTypeMatch(facetType, mediaType string) bool {
	switch facetType {
	case "":
		return true
	case "tv":
		return mediaType == "tv" || mediaType == "anime"
	case "movie":
		return mediaType == "movie"
	default:
		return mediaType == facetType
	}
}

func fyFieldWeight(tagKey string) float64 {
	switch tagKey[0] {
	case 'g':
		return fyFWGenre
	case 'k':
		return fyFWKeyword
	case 'c':
		return fyFWCast
	case 'd':
		return fyFWDirector
	}
	return 1
}

func fyPrettyTag(k string, names map[int64]string) string {
	if len(k) < 2 {
		return k
	}
	v := k[2:]
	switch k[0] {
	case 'g':
		return "More " + v
	case 'k':
		return "You like “" + v + "”"
	case 'c', 'd':
		id, _ := strconv.ParseInt(v, 10, 64)
		who := names[id]
		if who == "" {
			return "Matches your taste"
		}
		if k[0] == 'd' {
			return "Directed by " + who
		}
		return "Starring " + who
	}
	return k
}

func fyJaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	small, large := a, b
	if len(b) < len(a) {
		small, large = b, a
	}
	inter := 0
	for k := range small {
		if _, ok := large[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func round3(f float64) float64 { return math.Round(f*1000) / 1000 }

// fyScanPairs runs a (id, text) query and calls fn per row.
func (a *App) fyScanPairs(ctx context.Context, sql string, fn func(id int64, v string)) error {
	rows, err := a.db.Query(ctx, sql)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var v string
		if err := rows.Scan(&id, &v); err != nil {
			return err
		}
		fn(id, v)
	}
	return rows.Err()
}

// fyScanIDs runs a (id, id) query and calls fn per row.
func (a *App) fyScanIDs(ctx context.Context, sql string, fn func(id, pid int64)) error {
	rows, err := a.db.Query(ctx, sql)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, pid int64
		if err := rows.Scan(&id, &pid); err != nil {
			return err
		}
		fn(id, pid)
	}
	return rows.Err()
}

// fyScanOne runs a single-int-column query and calls fn per row.
func (a *App) fyScanOne(ctx context.Context, sql string, fn func(id int64), args ...any) error {
	rows, err := a.db.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		fn(id)
	}
	return rows.Err()
}
