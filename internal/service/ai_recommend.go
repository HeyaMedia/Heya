package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/karbowiak/heya/internal/llm"
	"github.com/rs/zerolog/log"
)

// AI-curated recommendations: the LLM never invents titles — it proposes over
// candidates the embedding engine retrieved from the library ("AI proposes,
// code disposes"). Latency-first shape: the raw ask KNNs immediately
// (milliseconds) and ONE model call grades that pool — the common case pays a
// single LLM round-trip. The same call may request up to three follow-up
// search probes (the model's one "tool use"); code runs them ONLY when the
// first round's strong picks came up short, grading just the new candidates
// in a second call. Worst case is the old two-call latency; typical asks
// halve it.

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
	aiRecDefaultLimit      = 12
	aiRecMaxLimit          = 20
	aiRecPoolSize          = 32 // round-1 candidates from the raw ask
	aiRecFollowupPerProbe  = 16 // KNN hits fetched per follow-up probe
	aiRecFollowupPool      = 20 // new candidates a follow-up round may add
	aiRecHistoryLines      = 12 // recent watches shown to the model
	aiRecFollowupThreshold = 3  // strong (fit ≥4) picks below this allow a follow-up
)

var aiRecPicksSchema = []byte(`{
	"type": "object",
	"properties": {
		"picks": {
			"type": "array",
			"maxItems": 24,
			"items": {
				"type": "object",
				"properties": {
					"key": { "type": "integer", "minimum": 1, "maximum": 48 },
					"title": { "type": "string", "minLength": 1, "maxLength": 200 },
					"reason": { "type": "string", "maxLength": 60 },
					"fit": { "type": "integer", "minimum": 1, "maximum": 5 }
				},
				"required": ["key", "title", "reason", "fit"],
				"additionalProperties": false
			}
		},
		"note": { "type": "string", "minLength": 1, "maxLength": 600 },
		"more_probes": {
			"type": "array",
			"maxItems": 3,
			"items": { "type": "string", "minLength": 3, "maxLength": 160 }
		}
	},
	"required": ["picks", "note", "more_probes"],
	"additionalProperties": false
}`)

type aiRecPick struct {
	Key    int    `json:"key"`
	Title  string `json:"title"`
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
	if lease, err := a.borrowRecEmbedder(ctx); err != nil {
		return AIRecommendResult{}, fmt.Errorf("load embedder: %w", err)
	} else if lease == nil {
		return AIRecommendResult{}, ErrMLDisabled
	} else {
		lease.Close()
	}
	s := a.AISettings(ctx)
	client, model, err := a.aiClient(ctx, s)
	if err != nil {
		return AIRecommendResult{}, err
	}
	start := time.Now()

	// Personalization context — optional, recommendations still work without it.
	history, watched := a.aiRecHistory(ctx, userID)

	// Round 1: KNN the raw ask directly (milliseconds) and grade that pool in
	// ONE model call. Since episode embeddings landed, raw-ask retrieval finds
	// the right candidates for most asks — no up-front probe call needed.
	pool, err := a.aiRecSearch(ctx, []string{query}, in.Type, nil, aiRecPoolSize)
	if err != nil {
		return AIRecommendResult{}, err
	}
	result := AIRecommendResult{
		Items:  []ForYouItem{},
		Probes: []string{query},
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

	picked, err := a.aiRecCurate(ctx, client, model, s.Mode, query, in.Type, pool, watched, history, nil)
	if err != nil {
		return AIRecommendResult{}, fmt.Errorf("curate: %w", err)
	}
	graded := picked.Picks
	note := picked.Note
	round1Ms := time.Since(start).Milliseconds()

	// Follow-up: the grade call may have requested extra probes — its one
	// "tool use". Code decides whether to spend the second round: only when
	// the first round's strong picks came up short.
	if len(picked.MoreProbes) > 0 && countStrongPicks(picked.Picks) < aiRecFollowupThreshold {
		have := map[int64]bool{}
		for _, it := range pool {
			have[it.ID] = true
		}
		probes := capN(picked.MoreProbes, 3)
		extra, err := a.aiRecSearch(ctx, probes, in.Type, have, aiRecFollowupPool)
		if err != nil {
			return AIRecommendResult{}, err
		}
		if len(extra) > 0 {
			picked2, err := a.aiRecCurate(ctx, client, model, s.Mode, query, in.Type, extra, watched, history, probes)
			if err != nil {
				return AIRecommendResult{}, fmt.Errorf("curate follow-up: %w", err)
			}
			// Re-key the follow-up picks into the combined pool so one dispose
			// pass orders both rounds together.
			offset := len(pool)
			for _, p := range picked2.Picks {
				p.Key += offset
				graded = append(graded, p)
			}
			pool = append(pool, extra...)
			result.Probes = append(result.Probes, probes...)
			// The follow-up note explains the deeper find — prefer it when the
			// second round actually found something stronger.
			if maxFit(picked2.Picks) > maxFit(picked.Picks) && strings.TrimSpace(picked2.Note) != "" {
				note = picked2.Note
			}
			log.Debug().Int64("round1_ms", round1Ms).Int64("total_ms", time.Since(start).Milliseconds()).
				Strs("probes", probes).Int("extra_candidates", len(extra)).Msg("ai recommend: follow-up round used")
		}
	}

	result.Items = disposePicks(pool, graded, limit)
	result.Note = strings.TrimSpace(note)
	result.DurationMs = time.Since(start).Milliseconds()
	return result, nil
}

// aiRecCurated is one grading response: graded picks, the viewer-facing note,
// and optionally the model's request for a deeper search.
type aiRecCurated struct {
	Picks      []aiRecPick `json:"picks"`
	Note       string      `json:"note"`
	MoreProbes []string    `json:"more_probes"`
}

// aiRecCurate runs one grading call over a candidate pool. followupProbes is
// nil for round 1; for round 2 it carries the probes that produced the pool so
// the prompt can frame the candidates as additions.
func (a *App) aiRecCurate(ctx context.Context, client llm.Completer, model, mode, query, mediaType string, pool []ForYouItem, watched map[int64]bool, history, followupProbes []string) (aiRecCurated, error) {
	blurbs := a.aiRecBlurbs(ctx, pool)
	var picked aiRecCurated
	err := client.CompleteJSON(ctx, llm.Request{
		Model:       model,
		Temperature: &aiRecTemp,
		Messages: []llm.Message{
			{Role: "system", Content: aiRecCurateSystem()},
			{Role: "user", Content: aiRecCurateUser(query, mediaType, pool, blurbs, watched, history, followupProbes)},
		},
	}, "curated_picks", aiRecPicksSchema, &picked)
	if mode == "local" {
		a.llmLocal.Touch()
	}
	return picked, err
}

// countStrongPicks counts grades ≥4 — the "did round 1 already answer this?"
// signal that gates the follow-up round.
func countStrongPicks(picks []aiRecPick) int {
	n := 0
	for _, p := range picks {
		if p.Fit >= 4 {
			n++
		}
	}
	return n
}

func maxFit(picks []aiRecPick) int {
	best := 0
	for _, p := range picks {
		if p.Fit > best {
			best = p.Fit
		}
	}
	return best
}

// aiRecHistory returns the prompt-ready recent-watch lines plus the watched
// id set used to flag candidates. Failures degrade to no personalization.
func (a *App) aiRecHistory(ctx context.Context, userID int64) (lines []string, watched map[int64]bool) {
	watched = map[int64]bool{}
	rows, err := a.ListRecentlyWatched(ctx, userID, 20, 0)
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

// aiRecSearch runs each probe through SemanticSearch and merges the hits,
// keeping each item's best similarity, skipping ids in exclude (already
// graded in a prior round), capped to the strongest `cap` candidates.
func (a *App) aiRecSearch(ctx context.Context, probes []string, mediaType string, exclude map[int64]bool, poolCap int) ([]ForYouItem, error) {
	perProbe := aiRecPoolSize
	if len(probes) > 1 || exclude != nil {
		perProbe = aiRecFollowupPerProbe
	}
	byID := map[int64]ForYouItem{}
	for _, probe := range probes {
		probe = strings.TrimSpace(probe)
		if probe == "" {
			continue
		}
		hits, err := a.SemanticSearch(ctx, probe, ForYouFacets{Type: mediaType, Limit: int32(perProbe)})
		if err != nil {
			return nil, fmt.Errorf("search %q: %w", probe, err)
		}
		for _, h := range hits {
			if exclude[h.ID] {
				continue
			}
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
	if len(pool) > poolCap {
		pool = pool[:poolCap]
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
		if len(desc) > 160 {
			desc = desc[:160] + "…"
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
		if len(ov) > 180 {
			ov = ov[:180] + "…"
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
		return "TV series only"
	case "anime":
		return "anime series only"
	default:
		return "movies and TV series"
	}
}

func aiRecCurateSystem() string {
	return "You judge search results for a media library's recommendation engine. " +
		"You get a viewer request and a candidate list retrieved by semantic search — retrieval order is noisy, so judge each candidate on its own genres, themes, and plot, not its position. " +
		"The provided summaries are spoiler-safe and shallow; when you know a title yourself, judge it on everything you know about it, not just the summary. " +
		"Emit a pick for every candidate that plausibly fits the request, grading fit: " +
		"5 = exactly what was asked, 4 = strong match, 3 = decent match, 2 = tangential, 1 = poor. " +
		"Omit candidates that do not fit at all; do not pad. Typically a handful of candidates rate 4-5. Rules: " +
		"use only candidate keys from the list and copy that candidate's title exactly; " +
		"rate a title the viewer recently watched one grade lower unless the request implies rewatching or continuing something; " +
		"reason = a short line (max 8 words) shown under the poster saying why it fits — plain human language, never mention ids, grades, or \"the viewer\"; " +
		"note = 1-2 sentences speaking directly to the viewer as \"you\", explaining how you read the request and why the picks fit overall " +
		"(e.g. \"I looked for … — these fit because …\"). If nothing fits, use the note to say what you looked for and why nothing matched. " +
		"If you recognized a specific title the request was hinting at, name it in the note. Never mention ids, grades, or \"the viewer\" in the note. " +
		"Write reasons and the note entirely in English. " +
		"more_probes = almost always an empty array. Only when the candidates cover the request poorly (few or no strong fits) may you request ONE deeper search: " +
		"1-3 new probes for the library's semantic index — different phrasings, moods, themes, or plot elements than the original request, in English. " +
		"If the request hints at a specific title you recognize, put that title's name in a probe: titles are always indexed even though descriptions avoid spoilers. " +
		"Never request probes when strong candidates already exist."
}

func aiRecCurateUser(query, mediaType string, pool []ForYouItem, blurbs map[int64]string, watched map[int64]bool, history, followupProbes []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Viewer request: %s\n", query)
	fmt.Fprintf(&b, "Scope: %s\n", aiRecScope(mediaType))
	if len(history) > 0 {
		fmt.Fprintf(&b, "Recently watched (newest first): %s\n", strings.Join(history, "; "))
	}
	if len(followupProbes) > 0 {
		fmt.Fprintf(&b, "These are ADDITIONAL candidates from the deeper search you requested (probes: %s). "+
			"Grade them for the same request; do not request further probes.\n", strings.Join(followupProbes, " · "))
	}
	b.WriteString("\nCandidates:\n")
	for i, it := range pool {
		fmt.Fprintf(&b, "key=%d | title=%q", i+1, it.Title)
		if it.Year != "" {
			fmt.Fprintf(&b, " (%s)", it.Year)
		}
		candidateType := it.MediaType
		if mediaType != "anime" && candidateType == "anime" {
			// `anime` is a storage/search subtype, not a separate kind of media
			// on the TV surface. Do not expose that implementation detail to the
			// curator or it may treat the scope as an exclusion rule.
			candidateType = "tv"
		}
		fmt.Fprintf(&b, " | %s", candidateType)
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
// keys and duplicates are dropped, reasons are attached. The echoed title is
// cross-checked against the key so a model cannot attach the right reasoning to
// the wrong database item. Ordering is OURS, not
// the model's — fit grade first, embedding similarity as tiebreak — and weak
// grades (≤2) only surface when nothing rated ≥3, so the junk tail stays cut.
func disposePicks(pool []ForYouItem, picks []aiRecPick, limit int) []ForYouItem {
	byTitle := make(map[string]ForYouItem, len(pool))
	for _, it := range pool {
		byTitle[recommendationTitleKey(it.Title)] = it
	}
	type graded struct {
		item ForYouItem
		fit  int
	}
	var kept []graded
	used := map[int64]bool{}
	for _, p := range picks {
		if p.Key < 1 || p.Key > len(pool) {
			continue
		}
		it := pool[p.Key-1]
		pickedTitle := recommendationTitleKey(p.Title)
		if pickedTitle == "" {
			continue
		}
		if recommendationTitleKey(it.Title) != pickedTitle {
			// Small models occasionally emit the intended title beside another
			// candidate's key. Recover only on an exact normalized title match.
			var ok bool
			it, ok = byTitle[pickedTitle]
			if !ok {
				continue
			}
		}
		if used[it.ID] {
			continue
		}
		used[it.ID] = true
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

func recommendationTitleKey(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
