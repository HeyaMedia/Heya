package scanner

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/karbowiak/heya/internal/metadata"
)

const tvAutoMatchThreshold = 0.85

type TVSearchProvider interface {
	Search(context.Context, metadata.MediaKind, metadata.SearchQuery) ([]metadata.SearchResult, error)
}

type TVSearchMatch struct {
	Key            string              `json:"key"`
	Query          TVSearchQuery       `json:"query"`
	Accepted       bool                `json:"accepted"`
	Reason         string              `json:"reason,omitempty"`
	ProviderID     string              `json:"provider_id,omitempty"`
	Provider       string              `json:"provider,omitempty"`
	Title          string              `json:"title,omitempty"`
	Year           string              `json:"year,omitempty"`
	Confidence     float64             `json:"confidence"`
	Candidates     []TVSearchCandidate `json:"candidates,omitempty"`
	ExternalIDs    map[string]string   `json:"external_ids,omitempty"`
	ManualDecision string              `json:"manual_decision,omitempty"`
}

type TVSearchQuery struct {
	Title   string   `json:"title"`
	Year    string   `json:"year,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
}

type TVSearchCandidate struct {
	ProviderID  string            `json:"provider_id"`
	Provider    string            `json:"provider"`
	Title       string            `json:"title"`
	Year        string            `json:"year,omitempty"`
	Description string            `json:"description,omitempty"`
	PosterURL   string            `json:"poster_url,omitempty"`
	HeyaSlug    string            `json:"heya_slug,omitempty"`
	Confidence  float64           `json:"confidence"`
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
}

func SearchTVMatches(ctx context.Context, matches []TVMatch, provider TVSearchProvider, emit Emitter, decisionsOpt ...SearchDecisions) ([]TVSearchMatch, error) {
	return searchTVLikeMatches(ctx, matches, provider, emit, "tv", decisionsOpt...)
}

func SearchAnimeMatches(ctx context.Context, matches []TVMatch, provider TVSearchProvider, emit Emitter, decisionsOpt ...SearchDecisions) ([]TVSearchMatch, error) {
	return searchTVLikeMatches(ctx, matches, provider, emit, "anime", decisionsOpt...)
}

func searchTVLikeMatches(ctx context.Context, matches []TVMatch, provider TVSearchProvider, emit Emitter, domain string, decisionsOpt ...SearchDecisions) ([]TVSearchMatch, error) {
	if provider == nil {
		return nil, fmt.Errorf("%s search provider is required", domain)
	}

	decisions := optionalSearchDecisions(decisionsOpt)
	results := make([]TVSearchMatch, 0, len(matches))
	for _, match := range matches {
		if err := ctx.Err(); err != nil {
			return results, err
		}

		query := metadata.SearchQuery{Title: match.Title, Year: match.Year}
		search := TVSearchMatch{
			Key:   match.Key,
			Query: TVSearchQuery{Title: query.Title, Year: query.Year, Aliases: match.Aliases},
		}
		emit.Emit(Event{
			Event: "match.search",
			Kind:  domain,
			Data: map[string]any{
				"key":   match.Key,
				"title": query.Title,
				"year":  query.Year,
			},
		})

		if decision, ok := decisions[match.Key]; ok {
			if applied, handled := applyTVSearchDecision(match, search, decision, domain, emit); handled {
				results = append(results, applied)
				continue
			}
		}

		if direct, ok := directTVSearchMatch(match, domain, emit); ok {
			results = append(results, direct)
			continue
		}

		candidates, err := provider.Search(ctx, metadata.KindTV, query)
		if err != nil {
			search.Reason = "search_error"
			emit.Emit(Event{
				Event:    "match.search_failed",
				Severity: SeverityWarn,
				Kind:     domain,
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
			scored[i].Confidence = scoreTVSearchCandidate(match, scored[i])
		}
		sort.Slice(scored, func(i, j int) bool {
			if scored[i].Confidence == scored[j].Confidence {
				iExact := tvSearchPrimaryTitleExact(match, scored[i].Title)
				jExact := tvSearchPrimaryTitleExact(match, scored[j].Title)
				if iExact != jExact {
					return iExact
				}
				return scored[i].Title < scored[j].Title
			}
			return scored[i].Confidence > scored[j].Confidence
		})

		for _, candidate := range scored {
			search.Candidates = append(search.Candidates, TVSearchCandidate{
				ProviderID:  candidate.ProviderID,
				Provider:    candidate.ProviderName,
				Title:       candidate.Title,
				Year:        candidate.Year,
				Description: candidate.Description,
				PosterURL:   candidate.PosterURL,
				HeyaSlug:    candidate.HeyaSlug,
				Confidence:  candidate.Confidence,
				ExternalIDs: candidate.ExternalIDs,
			})
			emit.Emit(Event{
				Event: "match.candidate",
				Kind:  domain,
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
			emit.Emit(Event{Event: "match.unresolved", Kind: domain, Reason: search.Reason, Data: map[string]any{"key": match.Key, "title": query.Title, "year": query.Year}})
			results = append(results, search)
			continue
		}

		top := scored[0]
		clearGap := movieSearchClearGap(scored, match.Title)
		if top.Confidence >= tvAutoMatchThreshold && clearGap {
			search.Accepted = true
			search.ProviderID = top.ProviderID
			search.Provider = top.ProviderName
			search.Title = top.Title
			search.Year = top.Year
			search.Confidence = top.Confidence
			search.ExternalIDs = top.ExternalIDs
			emit.Emit(Event{
				Event: "match.selected",
				Kind:  domain,
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
				Kind:   domain,
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
	emit.Emit(Event{Event: "match.search_summary", Data: map[string]any{"domain": domain, "matches": len(results), "accepted": accepted}})
	return results, nil
}

func directTVSearchMatch(match TVMatch, domain string, emit Emitter) (TVSearchMatch, bool) {
	provider := tvDirectIDProvider(match.KeyType)
	if provider == "" {
		return TVSearchMatch{}, false
	}
	value := strings.TrimSpace(match.ExternalIDs[provider])
	if value == "" {
		prefix := provider + ":"
		if strings.HasPrefix(match.Key, prefix) {
			value = strings.TrimSpace(strings.TrimPrefix(match.Key, prefix))
		}
	}
	if value == "" {
		return TVSearchMatch{}, false
	}

	providerID := "heya:tv:" + provider + ":" + value
	candidate := TVSearchCandidate{
		ProviderID:  providerID,
		Provider:    "heya",
		Title:       match.Title,
		Year:        match.Year,
		Confidence:  1,
		ExternalIDs: cloneTVExternalIDs(match.ExternalIDs),
	}
	if candidate.ExternalIDs == nil {
		candidate.ExternalIDs = map[string]string{}
	}
	candidate.ExternalIDs[provider] = value

	search := TVSearchMatch{
		Key:         match.Key,
		Query:       TVSearchQuery{Title: match.Title, Year: match.Year, Aliases: match.Aliases},
		Accepted:    true,
		ProviderID:  providerID,
		Provider:    "heya",
		Title:       match.Title,
		Year:        match.Year,
		Confidence:  1,
		Candidates:  []TVSearchCandidate{candidate},
		ExternalIDs: candidate.ExternalIDs,
	}
	emit.Emit(Event{
		Event: "match.direct_selected",
		Kind:  domain,
		Data: map[string]any{
			"key":          match.Key,
			"provider_id":  providerID,
			"title":        match.Title,
			"year":         match.Year,
			"confidence":   search.Confidence,
			"external_ids": candidate.ExternalIDs,
		},
	})
	return search, true
}

func tvDirectIDProvider(keyType string) string {
	switch keyType {
	case "tmdb", "tvdb", "imdb", "anidb", "mal":
		return keyType
	default:
		return ""
	}
}

func cloneTVExternalIDs(ids map[string]string) map[string]string {
	if len(ids) == 0 {
		return nil
	}
	out := make(map[string]string, len(ids))
	for key, value := range ids {
		out[key] = value
	}
	return out
}

func applyTVSearchDecision(match TVMatch, search TVSearchMatch, decision SearchDecision, domain string, emit Emitter) (TVSearchMatch, bool) {
	switch decision.Status {
	case "accepted":
		if decision.ProviderID == "" {
			return search, false
		}
		candidate := TVSearchCandidate{
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
		search.Candidates = []TVSearchCandidate{candidate}
		search.ManualDecision = decision.Status
		emit.Emit(Event{
			Event: "match.manual_selected",
			Kind:  domain,
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
			Kind:   domain,
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

func scoreTVSearchCandidate(match TVMatch, candidate metadata.SearchResult) float64 {
	if sharedExternalID(match.ExternalIDs, candidate.ExternalIDs) {
		return 1
	}
	best := 0.0
	for _, queryTitle := range tvSearchQueryTitles(match) {
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

func tvSearchQueryTitles(match TVMatch) []string {
	titles := []string{match.Title}
	titles = append(titles, match.Aliases...)
	return sortedTVAliases("", titles)
}

func tvSearchPrimaryTitleExact(match TVMatch, title string) bool {
	title = normalizeSearchTitle(title)
	if title == "" {
		return false
	}
	for _, queryTitle := range tvSearchQueryTitles(match) {
		if normalizeSearchTitle(queryTitle) == title {
			return true
		}
	}
	return false
}
