package scanner

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/karbowiak/heya/internal/metadata"
)

const bookAutoMatchThreshold = 0.70

type BookSearchProvider interface {
	Search(context.Context, metadata.MediaKind, metadata.SearchQuery) ([]metadata.SearchResult, error)
}

type BookSearchMatch struct {
	Key            string                `json:"key"`
	Query          BookSearchQuery       `json:"query"`
	Accepted       bool                  `json:"accepted"`
	Reason         string                `json:"reason,omitempty"`
	ProviderID     string                `json:"provider_id,omitempty"`
	Provider       string                `json:"provider,omitempty"`
	Title          string                `json:"title,omitempty"`
	Author         string                `json:"author,omitempty"`
	Year           string                `json:"year,omitempty"`
	Format         string                `json:"format,omitempty"`
	Confidence     float64               `json:"confidence"`
	Candidates     []BookSearchCandidate `json:"candidates,omitempty"`
	ExternalIDs    map[string]string     `json:"external_ids,omitempty"`
	ManualDecision string                `json:"manual_decision,omitempty"`
}

type BookSearchQuery struct {
	Title   string   `json:"title"`
	Author  string   `json:"author,omitempty"`
	Year    string   `json:"year,omitempty"`
	Format  string   `json:"format,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
}

type BookSearchCandidate struct {
	ProviderID  string            `json:"provider_id"`
	Provider    string            `json:"provider"`
	Title       string            `json:"title"`
	Author      string            `json:"author,omitempty"`
	Year        string            `json:"year,omitempty"`
	Description string            `json:"description,omitempty"`
	PosterURL   string            `json:"poster_url,omitempty"`
	HeyaSlug    string            `json:"heya_slug,omitempty"`
	Confidence  float64           `json:"confidence"`
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
}

func SearchBookPlans(ctx context.Context, plans []BookPlan, provider BookSearchProvider, emit Emitter, decisionsOpt ...SearchDecisions) ([]BookSearchMatch, error) {
	if provider == nil {
		return nil, fmt.Errorf("book search provider is required")
	}
	decisions := optionalSearchDecisions(decisionsOpt)
	results := make([]BookSearchMatch, 0, len(plans))
	for _, plan := range plans {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		query := metadata.SearchQuery{
			Title:  plan.Title,
			Year:   plan.Year,
			Author: plan.Author,
			Format: plan.Format,
		}
		search := BookSearchMatch{
			Key:    plan.Key,
			Query:  BookSearchQuery{Title: query.Title, Author: query.Author, Year: query.Year, Format: query.Format, Aliases: bookTitleSearchAliases(query.Title)},
			Author: plan.Author,
			Format: plan.Format,
		}
		emit.Emit(Event{
			Event: "match.search",
			Kind:  "book",
			Data: map[string]any{
				"key":    plan.Key,
				"title":  query.Title,
				"author": query.Author,
				"year":   query.Year,
				"format": query.Format,
			},
		})

		if decision, ok := decisions[plan.Key]; ok {
			if applied, handled := applyBookSearchDecision(plan, search, decision, emit); handled {
				results = append(results, applied)
				continue
			}
		}

		candidates, err := searchBookCandidates(ctx, provider, query, search.Query.Aliases)
		if err != nil {
			search.Reason = "search_error"
			emit.Emit(Event{
				Event:    "match.search_failed",
				Severity: SeverityWarn,
				Kind:     "book",
				Reason:   search.Reason,
				Message:  err.Error(),
				Data: map[string]any{
					"key":    plan.Key,
					"title":  query.Title,
					"author": query.Author,
					"year":   query.Year,
				},
			})
			results = append(results, search)
			continue
		}

		scored := make([]metadata.SearchResult, len(candidates))
		copy(scored, candidates)
		for i := range scored {
			scored[i].Confidence = scoreBookSearchCandidate(plan, scored[i])
		}
		sort.Slice(scored, func(i, j int) bool {
			if scored[i].Confidence == scored[j].Confidence {
				return scored[i].Title < scored[j].Title
			}
			return scored[i].Confidence > scored[j].Confidence
		})

		for _, candidate := range scored {
			search.Candidates = append(search.Candidates, BookSearchCandidate{
				ProviderID:  candidate.ProviderID,
				Provider:    candidate.ProviderName,
				Title:       candidate.Title,
				Author:      candidate.Description,
				Year:        candidate.Year,
				Description: candidate.Description,
				PosterURL:   candidate.PosterURL,
				HeyaSlug:    candidate.HeyaSlug,
				Confidence:  candidate.Confidence,
				ExternalIDs: candidate.ExternalIDs,
			})
			emit.Emit(Event{
				Event: "match.candidate",
				Kind:  "book",
				Data: map[string]any{
					"key":          plan.Key,
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
			emit.Emit(Event{Event: "match.unresolved", Kind: "book", Reason: search.Reason, Data: map[string]any{"key": plan.Key, "title": query.Title, "author": query.Author, "year": query.Year}})
			results = append(results, search)
			continue
		}

		top := scored[0]
		if top.Confidence >= bookAutoMatchThreshold && bookTitleAcceptable(plan.Title, top.Title) {
			search.Accepted = true
			search.ProviderID = top.ProviderID
			search.Provider = top.ProviderName
			search.Title = top.Title
			search.Author = firstNonEmpty(top.Description, plan.Author)
			search.Year = top.Year
			search.Confidence = top.Confidence
			search.ExternalIDs = top.ExternalIDs
			emit.Emit(Event{
				Event: "match.selected",
				Kind:  "book",
				Data: map[string]any{
					"key":          plan.Key,
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
				Kind:   "book",
				Reason: search.Reason,
				Data: map[string]any{
					"key":        plan.Key,
					"top_title":  top.Title,
					"top_year":   top.Year,
					"confidence": top.Confidence,
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
	emit.Emit(Event{Event: "match.search_summary", Data: map[string]any{"domain": "book", "matches": len(results), "accepted": accepted}})
	return results, nil
}

func searchBookCandidates(ctx context.Context, provider BookSearchProvider, query metadata.SearchQuery, aliases []string) ([]metadata.SearchResult, error) {
	queries := append([]string{query.Title}, aliases...)
	seenQueries := map[string]bool{}
	seenCandidates := map[string]bool{}
	var out []metadata.SearchResult
	for _, title := range queries {
		queryKey := strings.ToLower(strings.TrimSpace(title))
		if normalizeSearchTitle(title) == "" || seenQueries[queryKey] {
			continue
		}
		seenQueries[queryKey] = true
		q := query
		q.Title = title
		candidates, err := provider.Search(ctx, metadata.KindBook, q)
		if err != nil {
			return out, err
		}
		for _, candidate := range candidates {
			key := candidate.ProviderID
			if key == "" {
				key = normalizeSearchTitle(candidate.Title) + "|" + candidate.Year
			}
			if seenCandidates[key] {
				continue
			}
			seenCandidates[key] = true
			out = append(out, candidate)
		}
	}
	return out, nil
}

func bookTitleSearchAliases(title string) []string {
	fields := strings.Fields(title)
	if len(fields) < 3 {
		return nil
	}
	var aliases []string
	for i := 1; i < len(fields)-1; i++ {
		switch strings.ToLower(strings.Trim(fields[i], ":")) {
		case "the", "a", "an":
			alias := strings.Join(fields[:i], " ") + ": " + strings.Join(fields[i:], " ")
			if !strings.EqualFold(alias, title) {
				aliases = append(aliases, alias)
			}
		}
	}
	return aliases
}

func applyBookSearchDecision(plan BookPlan, search BookSearchMatch, decision SearchDecision, emit Emitter) (BookSearchMatch, bool) {
	switch decision.Status {
	case "accepted":
		if decision.ProviderID == "" {
			return search, false
		}
		candidate := BookSearchCandidate{
			ProviderID:  decision.ProviderID,
			Provider:    firstNonEmpty(decision.Provider, "heya"),
			Title:       firstNonEmpty(decision.Title, plan.Title),
			Author:      plan.Author,
			Year:        firstNonEmpty(decision.Year, plan.Year),
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
		search.Author = candidate.Author
		search.Year = candidate.Year
		search.Confidence = candidate.Confidence
		search.ExternalIDs = candidate.ExternalIDs
		search.Candidates = []BookSearchCandidate{candidate}
		search.ManualDecision = decision.Status
		emit.Emit(Event{
			Event: "match.manual_selected",
			Kind:  "book",
			Data: map[string]any{
				"key":         plan.Key,
				"provider_id": candidate.ProviderID,
				"title":       candidate.Title,
				"year":        candidate.Year,
				"confidence":  candidate.Confidence,
			},
		})
		return search, true
	case "rejected", "ignored":
		search.Reason = "manual_" + decision.Status
		search.ManualDecision = decision.Status
		emit.Emit(Event{
			Event:  "match.manual_blocked",
			Kind:   "book",
			Reason: search.Reason,
			Data: map[string]any{
				"key":    plan.Key,
				"status": decision.Status,
				"title":  plan.Title,
				"year":   plan.Year,
			},
		})
		return search, true
	default:
		return search, false
	}
}

func scoreBookSearchCandidate(plan BookPlan, candidate metadata.SearchResult) float64 {
	score := candidate.Confidence
	if score == 0 {
		score = 0.7
	}
	titleScore := stringSimilarity(plan.Title, candidate.Title)
	if titleScore >= 0.98 {
		score = maxFloat64(score, 0.95)
	} else if titleScore >= 0.90 {
		score = maxFloat64(score, 0.85)
	} else if titleScore < 0.72 {
		score = minFloat64(score, titleScore)
	}
	if plan.Year != "" && candidate.Year != "" {
		if plan.Year == candidate.Year {
			score += 0.03
		} else {
			score -= 0.25
		}
	}
	if plan.Author != "" && candidate.Description != "" {
		if strings.Contains(normalizeSearchTitle(candidate.Description), normalizeSearchTitle(plan.Author)) {
			score += 0.03
		} else {
			score -= 0.25
		}
	}
	if sharedExternalID(plan.ExternalIDs, candidate.ExternalIDs) {
		score = 1
	}
	if score > 1 {
		score = 1
	}
	if score < 0 {
		score = 0
	}
	return score
}

func bookTitleAcceptable(localTitle, remoteTitle string) bool {
	return normalizeSearchTitle(localTitle) == normalizeSearchTitle(remoteTitle) || stringSimilarity(localTitle, remoteTitle) >= 0.78 || substringSearchTitleMatch(localTitle, remoteTitle)
}

func maxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minFloat64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
