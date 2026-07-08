package ingestv2

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/karbowiak/heya/internal/metadata"
)

const movieAutoMatchThreshold = 0.85

type MovieSearchProvider interface {
	Search(context.Context, metadata.MediaKind, metadata.SearchQuery) ([]metadata.SearchResult, error)
}

type MovieSearchMatch struct {
	Key            string                 `json:"key"`
	Query          MovieSearchQuery       `json:"query"`
	Accepted       bool                   `json:"accepted"`
	Reason         string                 `json:"reason,omitempty"`
	ProviderID     string                 `json:"provider_id,omitempty"`
	Provider       string                 `json:"provider,omitempty"`
	Title          string                 `json:"title,omitempty"`
	Year           string                 `json:"year,omitempty"`
	Confidence     float64                `json:"confidence"`
	Candidates     []MovieSearchCandidate `json:"candidates,omitempty"`
	ExternalIDs    map[string]string      `json:"external_ids,omitempty"`
	ManualDecision string                 `json:"manual_decision,omitempty"`
}

type MovieSearchQuery struct {
	Title   string   `json:"title"`
	Year    string   `json:"year,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
}

type MovieSearchCandidate struct {
	ProviderID  string            `json:"provider_id"`
	Provider    string            `json:"provider"`
	Title       string            `json:"title"`
	Year        string            `json:"year,omitempty"`
	Confidence  float64           `json:"confidence"`
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
}

func SearchMovieMatches(ctx context.Context, matches []MovieMatch, provider MovieSearchProvider, emit Emitter, decisionsOpt ...SearchDecisions) ([]MovieSearchMatch, error) {
	if provider == nil {
		return nil, fmt.Errorf("movie search provider is required")
	}

	decisions := optionalSearchDecisions(decisionsOpt)
	results := make([]MovieSearchMatch, 0, len(matches))
	for _, match := range matches {
		if err := ctx.Err(); err != nil {
			return results, err
		}

		query := metadata.SearchQuery{Title: match.Title, Year: match.Year}
		search := MovieSearchMatch{
			Key:   match.Key,
			Query: MovieSearchQuery{Title: query.Title, Year: query.Year, Aliases: match.Aliases},
		}
		emit.Emit(Event{
			Event: "match.search",
			Kind:  "movie",
			Data: map[string]any{
				"key":   match.Key,
				"title": query.Title,
				"year":  query.Year,
			},
		})

		if decision, ok := decisions[match.Key]; ok {
			if applied, handled := applyMovieSearchDecision(match, search, decision, emit); handled {
				results = append(results, applied)
				continue
			}
		}

		candidates, err := provider.Search(ctx, metadata.KindMovie, query)
		if err != nil {
			search.Reason = "search_error"
			emit.Emit(Event{
				Event:    "match.search_failed",
				Severity: SeverityWarn,
				Kind:     "movie",
				Reason:   search.Reason,
				Message:  err.Error(),
				Data: map[string]any{
					"key":   match.Key,
					"title": query.Title,
					"year":  query.Year,
				},
			})
			results = append(results, search)
			continue
		}

		scored := make([]metadata.SearchResult, len(candidates))
		copy(scored, candidates)
		for i := range scored {
			scored[i].Confidence = scoreMovieSearchCandidate(match, scored[i])
		}
		sort.Slice(scored, func(i, j int) bool {
			if scored[i].Confidence == scored[j].Confidence {
				return scored[i].Title < scored[j].Title
			}
			return scored[i].Confidence > scored[j].Confidence
		})

		for _, candidate := range scored {
			search.Candidates = append(search.Candidates, MovieSearchCandidate{
				ProviderID:  candidate.ProviderID,
				Provider:    candidate.ProviderName,
				Title:       candidate.Title,
				Year:        candidate.Year,
				Confidence:  candidate.Confidence,
				ExternalIDs: candidate.ExternalIDs,
			})
			emit.Emit(Event{
				Event: "match.candidate",
				Kind:  "movie",
				Data: map[string]any{
					"key":          match.Key,
					"provider_id":  candidate.ProviderID,
					"title":        candidate.Title,
					"year":         candidate.Year,
					"confidence":   candidate.Confidence,
					"external_ids": candidate.ExternalIDs,
				},
			})
		}

		if len(scored) == 0 {
			search.Reason = "no_candidates"
			emit.Emit(Event{Event: "match.unresolved", Kind: "movie", Reason: search.Reason, Data: map[string]any{"key": match.Key, "title": query.Title, "year": query.Year}})
			results = append(results, search)
			continue
		}

		top := scored[0]
		clearGap := movieSearchClearGap(scored, match.Title)
		if top.Confidence >= movieAutoMatchThreshold && clearGap {
			search.Accepted = true
			search.ProviderID = top.ProviderID
			search.Provider = top.ProviderName
			search.Title = top.Title
			search.Year = top.Year
			search.Confidence = top.Confidence
			search.ExternalIDs = top.ExternalIDs
			emit.Emit(Event{
				Event: "match.selected",
				Kind:  "movie",
				Data: map[string]any{
					"key":          match.Key,
					"provider_id":  top.ProviderID,
					"title":        top.Title,
					"year":         top.Year,
					"confidence":   top.Confidence,
					"external_ids": top.ExternalIDs,
				},
			})
		} else {
			search.Reason = "ambiguous_or_low_confidence"
			search.Confidence = top.Confidence
			emit.Emit(Event{
				Event:  "match.rejected",
				Kind:   "movie",
				Reason: search.Reason,
				Data: map[string]any{
					"key":        match.Key,
					"top_title":  top.Title,
					"top_year":   top.Year,
					"confidence": top.Confidence,
					"clear_gap":  clearGap,
				},
			})
		}
		results = append(results, search)
	}

	accepted := 0
	for _, result := range results {
		if result.Accepted {
			accepted++
		}
	}
	emit.Emit(Event{Event: "match.search_summary", Data: map[string]any{"domain": "movie", "matches": len(results), "accepted": accepted}})
	return results, nil
}

func applyMovieSearchDecision(match MovieMatch, search MovieSearchMatch, decision SearchDecision, emit Emitter) (MovieSearchMatch, bool) {
	switch decision.Status {
	case "accepted":
		if decision.ProviderID == "" {
			return search, false
		}
		candidate := MovieSearchCandidate{
			ProviderID:  decision.ProviderID,
			Provider:    firstNonEmpty(decision.Provider, "heya"),
			Title:       firstNonEmpty(decision.Title, match.Title),
			Year:        firstNonEmpty(decision.Year, match.Year),
			Confidence:  decision.Confidence,
			ExternalIDs: decision.ExternalIDs,
		}
		if candidate.Confidence == 0 {
			candidate.Confidence = 1
		}
		search.Accepted = true
		search.ProviderID = candidate.ProviderID
		search.Provider = candidate.Provider
		search.Title = candidate.Title
		search.Year = candidate.Year
		search.Confidence = candidate.Confidence
		search.ExternalIDs = candidate.ExternalIDs
		search.Candidates = []MovieSearchCandidate{candidate}
		search.ManualDecision = decision.Status
		emit.Emit(Event{
			Event: "match.manual_selected",
			Kind:  "movie",
			Data: map[string]any{
				"key":          match.Key,
				"provider_id":  candidate.ProviderID,
				"title":        candidate.Title,
				"year":         candidate.Year,
				"confidence":   candidate.Confidence,
				"external_ids": candidate.ExternalIDs,
			},
		})
		return search, true
	case "rejected", "ignored":
		search.Reason = "manual_" + decision.Status
		search.ManualDecision = decision.Status
		emit.Emit(Event{
			Event:  "match.manual_blocked",
			Kind:   "movie",
			Reason: search.Reason,
			Data: map[string]any{
				"key":    match.Key,
				"status": decision.Status,
				"title":  match.Title,
				"year":   match.Year,
			},
		})
		return search, true
	default:
		return search, false
	}
}

func scoreMovieSearchCandidate(match MovieMatch, candidate metadata.SearchResult) float64 {
	if sharedExternalID(match.ExternalIDs, candidate.ExternalIDs) {
		return 1
	}
	best := 0.0
	for _, queryTitle := range searchQueryTitles(match) {
		if score := scoreMovieTitleYear(queryTitle, candidate.Title, match.Year, candidate.Year); score > best {
			best = score
		}
		for _, alt := range candidate.AltTitles {
			if alt == "" {
				continue
			}
			if score := scoreMovieTitleYear(queryTitle, alt, match.Year, candidate.Year); score > best {
				best = score
			}
		}
	}
	return best
}

func searchQueryTitles(match MovieMatch) []string {
	titles := []string{match.Title}
	titles = append(titles, match.Aliases...)
	return sortedMovieAliases("", titles)
}

func movieSearchClearGap(results []metadata.SearchResult, queryTitle string) bool {
	if len(results) == 1 {
		return true
	}
	top := results[0]
	secondDifferent := -1
	for i := 1; i < len(results); i++ {
		if normalizeSearchTitle(results[i].Title) != normalizeSearchTitle(top.Title) {
			secondDifferent = i
			break
		}
	}
	if secondDifferent == -1 || top.Confidence-results[secondDifferent].Confidence > 0.10 {
		return true
	}
	if normalizeSearchTitle(top.Title) == normalizeSearchTitle(queryTitle) {
		exact := 0
		for _, result := range results {
			if normalizeSearchTitle(result.Title) == normalizeSearchTitle(queryTitle) {
				exact++
			}
		}
		return exact == 1
	}
	return false
}

func scoreMovieTitleYear(queryTitle, resultTitle, queryYear, resultYear string) float64 {
	sim := stringSimilarity(queryTitle, resultTitle)
	score := sim * 0.85
	if substringSearchTitleMatch(queryTitle, resultTitle) && score < 0.80 {
		score = 0.80
	}
	if queryYear != "" && resultYear != "" {
		if queryYear == resultYear {
			score += 0.10
		} else if absInt(atoiDigits(queryYear)-atoiDigits(resultYear)) <= 1 {
			score += 0.05
		}
	}
	if score > 1 {
		return 1
	}
	return score
}

func sharedExternalID(a, b map[string]string) bool {
	for provider, av := range a {
		if av == "" {
			continue
		}
		if bv := b[provider]; bv != "" && strings.EqualFold(av, bv) {
			return true
		}
	}
	return false
}

func stringSimilarity(a, b string) float64 {
	a = normalizeSearchTitle(a)
	b = normalizeSearchTitle(b)
	if a == b {
		return 1
	}
	if a == "" || b == "" {
		return 0
	}
	d := levenshteinDistance(a, b)
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	return 1 - float64(d)/float64(maxLen)
}

func normalizeSearchTitle(s string) string {
	s = strings.ToLower(normalizeTitleSymbols(s))
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == ' ' {
			return r
		}
		return ' '
	}, s)
	for _, article := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(s, article) {
			s = s[len(article):]
			break
		}
	}
	return strings.Join(strings.Fields(s), " ")
}

var titleSymbolReplacer = strings.NewReplacer(
	"&", " and ",
	"¼", " 1 4 ",
	"½", " 1 2 ",
	"¾", " 3 4 ",
	"⅐", " 1 7 ",
	"⅑", " 1 9 ",
	"⅒", " 1 10 ",
	"⅓", " 1 3 ",
	"⅔", " 2 3 ",
	"⅕", " 1 5 ",
	"⅖", " 2 5 ",
	"⅗", " 3 5 ",
	"⅘", " 4 5 ",
	"⅙", " 1 6 ",
	"⅚", " 5 6 ",
	"⅛", " 1 8 ",
	"⅜", " 3 8 ",
	"⅝", " 5 8 ",
	"⅞", " 7 8 ",
	"⁰", "0",
	"¹", "1",
	"²", "2",
	"³", "3",
	"⁴", "4",
	"⁵", "5",
	"⁶", "6",
	"⁷", "7",
	"⁸", "8",
	"⁹", "9",
)

func normalizeTitleSymbols(s string) string {
	return titleSymbolReplacer.Replace(s)
}

func substringSearchTitleMatch(a, b string) bool {
	na := normalizeSearchTitle(a)
	nb := normalizeSearchTitle(b)
	if na == "" || nb == "" || na == nb {
		return false
	}
	shorter, longer := na, nb
	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}
	if !strings.Contains(longer, shorter) {
		return false
	}
	return len(strings.Fields(shorter)) >= 2
}

func levenshteinDistance(a, b string) int {
	la := len(a)
	lb := len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = minInt(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func minInt(values ...int) int {
	min := values[0]
	for _, value := range values[1:] {
		if value < min {
			min = value
		}
	}
	return min
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func atoiDigits(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}
