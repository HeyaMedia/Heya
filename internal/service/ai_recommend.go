package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/llm"
	"github.com/rs/zerolog/log"
)

// AI-curated recommendations: the LLM never invents titles — it proposes over
// candidates the embedding engine retrieved from the library ("AI proposes,
// code disposes"). Flow: (1) the model turns the viewer's ask + recent watch
// history into a few doc-styled search probes, (2) each probe runs through the
// existing SemanticSearch KNN and the hits merge into one candidate pool,
// (3) the model re-ranks the pool by id with a short reason each, dropping
// retrieval noise — the junk tail a raw KNN leaves behind.

// AIRecommendRequest is one "find me something to watch" ask.
type AIRecommendRequest struct {
	Query string `json:"query"`
	Type  string `json:"type,omitempty"`  // "", movie, tv, anime
	Limit int    `json:"limit,omitempty"` // picks to return (default 12, max 20)
}

// AIRecommendResult carries the curated picks plus enough metadata to show
// where they came from (model, probe queries, latency).
type AIRecommendResult struct {
	Items      []ForYouItem `json:"items"`
	Note       string       `json:"note,omitempty" doc:"the model's overall explanation of how it read the ask and why the picks fit"`
	Probes     []string     `json:"probes,omitempty" doc:"embedding probes the model searched with"`
	Model      string       `json:"model,omitempty"`
	Mode       string       `json:"mode"`
	DurationMs int64        `json:"duration_ms"`
}

const (
	aiRecDefaultLimit = 12
	aiRecMaxLimit     = 20
	aiRecPoolSize     = 48 // candidates offered to the re-ranker
	aiRecPerProbe     = 24 // KNN hits fetched per probe
	aiRecHistoryLines = 12 // recent watches shown to the model
)

var aiRecProbesSchema = []byte(`{
	"type": "object",
	"properties": {
		"probes": {
			"type": "array",
			"minItems": 1,
			"maxItems": 4,
			"items": { "type": "string", "minLength": 3, "maxLength": 200 }
		}
	},
	"required": ["probes"],
	"additionalProperties": false
}`)

var aiRecPicksSchema = []byte(`{
	"type": "object",
	"properties": {
		"picks": {
			"type": "array",
			"maxItems": 24,
			"items": {
				"type": "object",
				"properties": {
					"id": { "type": "integer" },
					"reason": { "type": "string", "maxLength": 90 },
					"fit": { "type": "integer", "minimum": 1, "maximum": 5 }
				},
				"required": ["id", "reason", "fit"],
				"additionalProperties": false
			}
		},
		"note": { "type": "string", "minLength": 1, "maxLength": 600 }
	},
	"required": ["picks", "note"],
	"additionalProperties": false
}`)

type aiRecPick struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
	Fit    int    `json:"fit"`
}

// aiRecTemp keeps both stages near-deterministic — at the default sampling
// temperature a 4B model's strictness swings between "nothing fits" and
// "everything fits" on identical input.
var aiRecTemp = 0.2

// AIRecommend answers a freeform "I want to watch…" ask with library titles,
// personalized by the user's recent watch history. Needs both the AI subsystem
// (ErrAIDisabled / llm.ErrNotConfigured) and the recommendations embedding
// engine (ErrMLDisabled).
func (a *App) AIRecommend(ctx context.Context, userID int64, in AIRecommendRequest) (AIRecommendResult, error) {
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return AIRecommendResult{}, fmt.Errorf("empty query")
	}
	switch in.Type {
	case "", "movie", "tv", "anime":
	default:
		return AIRecommendResult{}, fmt.Errorf("invalid type %q", in.Type)
	}
	limit := in.Limit
	if limit <= 0 {
		limit = aiRecDefaultLimit
	}
	if limit > aiRecMaxLimit {
		limit = aiRecMaxLimit
	}

	// Fail fast on either missing prerequisite before spawning anything.
	if emb, err := a.recEmbedderInstance(ctx); err != nil {
		return AIRecommendResult{}, fmt.Errorf("load embedder: %w", err)
	} else if emb == nil {
		return AIRecommendResult{}, ErrMLDisabled
	}
	s := a.AISettings(ctx)
	client, model, err := a.aiClient(ctx, s)
	if err != nil {
		return AIRecommendResult{}, err
	}
	start := time.Now()

	// Personalization context — optional, recommendations still work without it.
	history, watched := a.aiRecHistory(ctx, userID)

	// Stage 1: ask → embedding probes. A generation failure here degrades to
	// searching with the raw ask instead of failing the whole request.
	probes := a.aiRecProbes(ctx, client, model, query, in.Type, history)

	// Stage 2: pool candidates via the existing KNN, best similarity wins dupes.
	pool, err := a.aiRecPool(ctx, probes, in.Type)
	if err != nil {
		return AIRecommendResult{}, err
	}
	result := AIRecommendResult{
		Items:  []ForYouItem{},
		Probes: probes,
		Mode:   s.Mode,
		Model:  model,
	}
	if s.Mode == "local" {
		result.Model = s.LocalModel
	}
	if len(pool) == 0 {
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// Stage 3: re-rank. The model only picks ids from the pool; anything else
	// is dropped in disposePicks.
	blurbs := a.aiRecBlurbs(ctx, pool)
	var picked struct {
		Picks []aiRecPick `json:"picks"`
		Note  string      `json:"note"`
	}
	err = client.CompleteJSON(ctx, llm.Request{
		Model:       model,
		Temperature: &aiRecTemp,
		Messages: []llm.Message{
			{Role: "system", Content: aiRecCurateSystem()},
			{Role: "user", Content: aiRecCurateUser(query, in.Type, pool, blurbs, watched, history)},
		},
	}, "curated_picks", aiRecPicksSchema, &picked)
	if s.Mode == "local" {
		a.llmLocal.Touch()
	}
	if err != nil {
		return AIRecommendResult{}, fmt.Errorf("curate: %w", err)
	}

	result.Items = disposePicks(pool, picked.Picks, limit)
	result.Note = strings.TrimSpace(picked.Note)
	result.DurationMs = time.Since(start).Milliseconds()
	return result, nil
}

// aiRecHistory returns the prompt-ready recent-watch lines plus the watched
// id set used to flag candidates. Failures degrade to no personalization.
func (a *App) aiRecHistory(ctx context.Context, userID int64) (lines []string, watched map[int64]bool) {
	watched = map[int64]bool{}
	rows, err := a.ListRecentlyWatched(ctx, userID)
	if err != nil {
		log.Warn().Err(err).Msg("ai recommend: watch history unavailable")
		return nil, watched
	}
	for _, r := range rows {
		watched[r.MediaItemID] = true
		if len(lines) < aiRecHistoryLines {
			lines = append(lines, fmt.Sprintf("%s (%s)", r.Title, r.MediaType))
		}
	}
	return lines, watched
}

// aiRecProbes turns the ask into 1–4 embedding probes. The raw ask is always
// probe zero; the model's paraphrases diversify the candidate pool.
func (a *App) aiRecProbes(ctx context.Context, client *llm.Client, model, query, mediaType string, history []string) []string {
	sys := "You write search probes for a media library's semantic search engine. " +
		"Each library item is embedded as text shaped like: \"Title. Genres: <genres>. Themes: <keywords>. Starring: <cast>. <plot summary>\" " +
		"and a probe retrieves its nearest neighbors by embedding similarity. " +
		"Given a viewer's request, write 2-4 diverse probes that surface fitting titles: mood words, genres, themes, plot elements — " +
		"each covering a different interpretation or angle of the request. Do not repeat the request verbatim; it is already searched. " +
		"If the request describes a specific title you recognize (a plot point, a character, an oblique reference), name that title " +
		"in a probe — descriptions in the index avoid spoilers, but the title itself is always indexed. Write probes entirely in English."

	var b strings.Builder
	fmt.Fprintf(&b, "Viewer request: %s\n", query)
	fmt.Fprintf(&b, "Scope: %s\n", aiRecScope(mediaType))
	if len(history) > 0 {
		fmt.Fprintf(&b, "Recently watched (newest first): %s\n", strings.Join(history, "; "))
	}
	fmt.Fprintf(&b, "Today: %s (use the season/holidays only if the request implies it)", time.Now().Format("2006-01-02"))

	var out struct {
		Probes []string `json:"probes"`
	}
	err := client.CompleteJSON(ctx, llm.Request{
		Model:       model,
		Temperature: &aiRecTemp,
		Messages: []llm.Message{
			{Role: "system", Content: sys},
			{Role: "user", Content: b.String()},
		},
	}, "search_probes", aiRecProbesSchema, &out)
	if err != nil {
		log.Warn().Err(err).Msg("ai recommend: probe generation failed — searching with the raw ask only")
	}

	probes := []string{query}
	seen := map[string]bool{strings.ToLower(query): true}
	for _, p := range out.Probes {
		p = strings.TrimSpace(p)
		if p == "" || seen[strings.ToLower(p)] || len(probes) >= 5 {
			continue
		}
		seen[strings.ToLower(p)] = true
		probes = append(probes, p)
	}
	return probes
}

// aiRecPool runs every probe through SemanticSearch and merges the hits,
// keeping each item's best similarity, capped to the strongest aiRecPoolSize.
func (a *App) aiRecPool(ctx context.Context, probes []string, mediaType string) ([]ForYouItem, error) {
	byID := map[int64]ForYouItem{}
	for _, probe := range probes {
		hits, err := a.SemanticSearch(ctx, probe, ForYouFacets{Type: mediaType, Limit: aiRecPerProbe})
		if err != nil {
			return nil, fmt.Errorf("search %q: %w", probe, err)
		}
		for _, h := range hits {
			if prev, ok := byID[h.ID]; !ok || h.Score > prev.Score {
				byID[h.ID] = h
			}
		}
	}
	pool := make([]ForYouItem, 0, len(byID))
	for _, it := range byID {
		pool = append(pool, it)
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].Score > pool[j].Score })
	if len(pool) > aiRecPoolSize {
		pool = pool[:aiRecPoolSize]
	}
	return pool, nil
}

// aiRecBlurbs fetches genres + a short overview per pool item for the
// re-rank prompt. Failures degrade to title-only candidate lines.
func (a *App) aiRecBlurbs(ctx context.Context, pool []ForYouItem) map[int64]string {
	out := map[int64]string{}
	ids := make([]int64, 0, len(pool))
	for _, it := range pool {
		ids = append(ids, it.ID)
	}
	rows, err := a.db.Query(ctx, `
		SELECT mi.id, coalesce(mi.description,''), coalesce(m.genres, ts.genres, '{}'::text[])
		FROM media_item_cards mi
		LEFT JOIN movies m     ON m.media_item_id  = mi.id
		LEFT JOIN tv_series ts ON ts.media_item_id = mi.id
		WHERE mi.id = ANY($1)`, ids)
	if err != nil {
		log.Warn().Err(err).Msg("ai recommend: blurb hydration failed")
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var desc string
		var genres []string
		if err := rows.Scan(&id, &desc, &genres); err != nil {
			return out
		}
		if len(desc) > 220 {
			desc = desc[:220] + "…"
		}
		desc = strings.ReplaceAll(desc, "\n", " ")
		parts := []string{}
		if len(genres) > 0 {
			parts = append(parts, strings.Join(capN(genres, 4), ", "))
		}
		if desc != "" {
			parts = append(parts, desc)
		}
		out[id] = strings.Join(parts, " | ")
	}

	// Items that surfaced via an episode-embedding hit get that episode's
	// overview appended — it names the characters and plot points the
	// spoiler-safe series blurb omits, which is often the only evidence that
	// lets the grader connect an oblique ask to the right show.
	epByItem := map[int64]int64{}
	epIDs := make([]int64, 0, 4)
	for _, it := range pool {
		if it.matchedEpisodeID != 0 {
			epByItem[it.ID] = it.matchedEpisodeID
			epIDs = append(epIDs, it.matchedEpisodeID)
		}
	}
	if len(epIDs) == 0 {
		return out
	}
	epRows, err := a.db.Query(ctx, `SELECT id, overview FROM tv_episodes WHERE id = ANY($1) AND overview <> ''`, epIDs)
	if err != nil {
		log.Warn().Err(err).Msg("ai recommend: episode evidence hydration failed")
		return out
	}
	defer epRows.Close()
	epOverviews := map[int64]string{}
	for epRows.Next() {
		var id int64
		var ov string
		if err := epRows.Scan(&id, &ov); err != nil {
			return out
		}
		if len(ov) > 240 {
			ov = ov[:240] + "…"
		}
		epOverviews[id] = strings.ReplaceAll(ov, "\n", " ")
	}
	for itemID, epID := range epByItem {
		if ov := epOverviews[epID]; ov != "" {
			out[itemID] += " | matched episode: " + ov
		}
	}
	return out
}

func aiRecScope(mediaType string) string {
	switch mediaType {
	case "movie":
		return "movies only"
	case "tv":
		return "TV shows only"
	case "anime":
		return "anime only"
	default:
		return "movies and TV shows"
	}
}

func aiRecCurateSystem() string {
	return "You judge search results for a media library's recommendation engine. " +
		"You get a viewer request and a candidate list retrieved by semantic search — retrieval order is noisy, so judge each candidate on its own genres, themes, and plot, not its position. " +
		"The provided summaries are spoiler-safe and shallow; when you know a title yourself, judge it on everything you know about it, not just the summary. " +
		"Emit a pick for every candidate that plausibly fits the request, grading fit: " +
		"5 = exactly what was asked, 4 = strong match, 3 = decent match, 2 = tangential, 1 = poor. " +
		"Omit candidates that do not fit at all; do not pad. Typically a handful of candidates rate 4-5. Rules: " +
		"use only ids from the list; " +
		"rate a title the viewer recently watched one grade lower unless the request implies rewatching or continuing something; " +
		"reason = a short line (max 8 words) shown under the poster saying why it fits — plain human language, never mention ids, grades, or \"the viewer\"; " +
		"note = 1-2 sentences speaking directly to the viewer as \"you\", explaining how you read the request and why the picks fit overall " +
		"(e.g. \"I looked for … — these fit because …\"). If nothing fits, use the note to say what you looked for and why nothing matched. " +
		"If you recognized a specific title the request was hinting at, name it in the note. Never mention ids, grades, or \"the viewer\" in the note. " +
		"Write reasons and the note entirely in English."
}

func aiRecCurateUser(query, mediaType string, pool []ForYouItem, blurbs map[int64]string, watched map[int64]bool, history []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Viewer request: %s\n", query)
	fmt.Fprintf(&b, "Scope: %s\n", aiRecScope(mediaType))
	if len(history) > 0 {
		fmt.Fprintf(&b, "Recently watched (newest first): %s\n", strings.Join(history, "; "))
	}
	b.WriteString("\nCandidates:\n")
	for _, it := range pool {
		fmt.Fprintf(&b, "id=%d | %s", it.ID, it.Title)
		if it.Year != "" {
			fmt.Fprintf(&b, " (%s)", it.Year)
		}
		fmt.Fprintf(&b, " | %s", it.MediaType)
		if it.Rating > 0 {
			fmt.Fprintf(&b, " | rated %.1f", it.Rating)
		}
		if watched[it.ID] {
			b.WriteString(" | recently watched")
		}
		// Episode-driven retrieval evidence ("Matched S02E05 — …") — tells the
		// grader WHY this candidate surfaced when the series blurb won't.
		if it.Reason != "" {
			fmt.Fprintf(&b, " | %s", it.Reason)
		}
		if extra := blurbs[it.ID]; extra != "" {
			fmt.Fprintf(&b, " | %s", extra)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// disposePicks maps the model's picks back onto the retrieved pool: unknown
// ids and duplicates are dropped, reasons are attached. Ordering is OURS, not
// the model's — fit grade first, embedding similarity as tiebreak — and weak
// grades (≤2) only surface when nothing rated ≥3, so the junk tail stays cut.
func disposePicks(pool []ForYouItem, picks []aiRecPick, limit int) []ForYouItem {
	byID := make(map[int64]ForYouItem, len(pool))
	for _, it := range pool {
		byID[it.ID] = it
	}
	type graded struct {
		item ForYouItem
		fit  int
	}
	var kept []graded
	used := map[int64]bool{}
	for _, p := range picks {
		it, ok := byID[p.ID]
		if !ok || used[p.ID] {
			continue
		}
		used[p.ID] = true
		it.Reason = strings.TrimSpace(p.Reason)
		kept = append(kept, graded{item: it, fit: p.Fit})
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].fit != kept[j].fit {
			return kept[i].fit > kept[j].fit
		}
		return kept[i].item.Score > kept[j].item.Score
	})
	hasStrong := len(kept) > 0 && kept[0].fit >= 3
	weakCap := limit
	if !hasStrong && weakCap > 4 {
		weakCap = 4 // nothing really fits — offer a few near-misses, not a page
	}
	out := make([]ForYouItem, 0, limit)
	for _, g := range kept {
		if g.fit <= 2 && hasStrong {
			break // sorted by fit — everything from here down is the junk tail
		}
		out = append(out, g.item)
		if len(out) >= weakCap {
			break
		}
	}
	return out
}
