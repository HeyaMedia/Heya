package scanner

import "github.com/karbowiak/heya/internal/database/sqlc"

func applySearchDecisionsToResult(result *Result, lib sqlc.Library, decisions SearchDecisions, emit Emitter) {
	applyMovieSearchDecisions(result.MovieSearch, decisions, emit)
	applyTVSearchDecisions(result.TVSearch, lib, decisions, emit)
	applyMusicSearchDecisions(result.MusicSearch, decisions, emit)
	applyBookSearchDecisions(result.BookSearch, decisions, emit)
}

func applyMovieSearchDecisions(searches []MovieSearchMatch, decisions SearchDecisions, emit Emitter) {
	for i := range searches {
		decision, ok := decisions[searches[i].Key]
		if !ok {
			continue
		}
		switch decision.Status {
		case "accepted":
			candidate := movieCandidateForDecision(searches[i], decision)
			searches[i].Accepted = true
			searches[i].Reason = ""
			searches[i].ProviderID = candidate.ProviderID
			searches[i].Provider = candidate.Provider
			searches[i].Title = candidate.Title
			searches[i].Year = candidate.Year
			searches[i].Confidence = candidate.Confidence
			searches[i].ExternalIDs = candidate.ExternalIDs
			searches[i].ManualDecision = decision.Status
			emitDecisionOverlay(emit, "movie", searches[i].Key, decision)
		case "rejected", "ignored":
			searches[i].Accepted = false
			searches[i].Reason = "manual_" + decision.Status
			searches[i].ProviderID = ""
			searches[i].Provider = ""
			searches[i].ExternalIDs = nil
			searches[i].ManualDecision = decision.Status
			emitDecisionOverlay(emit, "movie", searches[i].Key, decision)
		}
	}
}

func movieCandidateForDecision(search MovieSearchMatch, decision SearchDecision) MovieSearchCandidate {
	for _, candidate := range search.Candidates {
		if candidate.ProviderID == decision.ProviderID {
			if candidate.Confidence == 0 {
				candidate.Confidence = firstPositiveFloat64(decision.Confidence, 1)
			}
			return candidate
		}
	}
	confidence := firstPositiveFloat64(decision.Confidence, 1)
	return MovieSearchCandidate{
		ProviderID:  decision.ProviderID,
		Provider:    firstNonEmpty(decision.Provider, "heya"),
		Title:       firstNonEmpty(decision.Title, search.Title, search.Query.Title),
		Year:        firstNonEmpty(decision.Year, search.Year, search.Query.Year),
		Confidence:  confidence,
		ExternalIDs: decision.ExternalIDs,
	}
}

func applyTVSearchDecisions(searches []TVSearchMatch, lib sqlc.Library, decisions SearchDecisions, emit Emitter) {
	domain := "tv"
	if lib.MediaType == sqlc.MediaTypeAnime {
		domain = "anime"
	}
	for i := range searches {
		decision, ok := decisions[searches[i].Key]
		if !ok {
			continue
		}
		switch decision.Status {
		case "accepted":
			candidate := tvCandidateForDecision(searches[i], decision)
			searches[i].Accepted = true
			searches[i].Reason = ""
			searches[i].ProviderID = candidate.ProviderID
			searches[i].Provider = candidate.Provider
			searches[i].Title = candidate.Title
			searches[i].Year = candidate.Year
			searches[i].Confidence = candidate.Confidence
			searches[i].ExternalIDs = candidate.ExternalIDs
			searches[i].ManualDecision = decision.Status
			emitDecisionOverlay(emit, domain, searches[i].Key, decision)
		case "rejected", "ignored":
			searches[i].Accepted = false
			searches[i].Reason = "manual_" + decision.Status
			searches[i].ProviderID = ""
			searches[i].Provider = ""
			searches[i].ExternalIDs = nil
			searches[i].ManualDecision = decision.Status
			emitDecisionOverlay(emit, domain, searches[i].Key, decision)
		}
	}
}

func tvCandidateForDecision(search TVSearchMatch, decision SearchDecision) TVSearchCandidate {
	for _, candidate := range search.Candidates {
		if candidate.ProviderID == decision.ProviderID {
			if candidate.Confidence == 0 {
				candidate.Confidence = firstPositiveFloat64(decision.Confidence, 1)
			}
			return candidate
		}
	}
	return TVSearchCandidate{
		ProviderID:  decision.ProviderID,
		Provider:    firstNonEmpty(decision.Provider, "heya"),
		Title:       firstNonEmpty(decision.Title, search.Title, search.Query.Title),
		Year:        firstNonEmpty(decision.Year, search.Year, search.Query.Year),
		Confidence:  firstPositiveFloat64(decision.Confidence, 1),
		ExternalIDs: decision.ExternalIDs,
	}
}

func applyMusicSearchDecisions(searches []MusicSearchMatch, decisions SearchDecisions, emit Emitter) {
	for i := range searches {
		decision, ok := decisions[searches[i].Key]
		if !ok {
			continue
		}
		switch decision.Status {
		case "accepted":
			candidate := musicCandidateForDecision(searches[i], decision)
			searches[i].Accepted = true
			searches[i].Reason = ""
			searches[i].Error = ""
			searches[i].ProviderID = candidate.ProviderID
			searches[i].Provider = candidate.Provider
			searches[i].Artist = candidate.Artist
			searches[i].Confidence = candidate.Confidence
			searches[i].ExternalIDs = candidate.ExternalIDs
			searches[i].ManualDecision = decision.Status
			emitDecisionOverlay(emit, "music", searches[i].Key, decision)
		case "rejected", "ignored":
			searches[i].Accepted = false
			searches[i].Reason = "manual_" + decision.Status
			searches[i].ProviderID = ""
			searches[i].Provider = ""
			searches[i].ExternalIDs = nil
			searches[i].ManualDecision = decision.Status
			emitDecisionOverlay(emit, "music", searches[i].Key, decision)
		}
	}
}

func musicCandidateForDecision(search MusicSearchMatch, decision SearchDecision) MusicSearchCandidate {
	for _, candidate := range search.Candidates {
		if candidate.ProviderID == decision.ProviderID {
			if candidate.Confidence == 0 {
				candidate.Confidence = firstPositiveFloat64(decision.Confidence, 1)
			}
			return candidate
		}
	}
	return MusicSearchCandidate{
		ProviderID:  decision.ProviderID,
		Provider:    firstNonEmpty(decision.Provider, "heya"),
		Artist:      firstNonEmpty(decision.Title, search.Artist, search.Query.Artist),
		Confidence:  firstPositiveFloat64(decision.Confidence, 1),
		ExternalIDs: decision.ExternalIDs,
	}
}

func applyBookSearchDecisions(searches []BookSearchMatch, decisions SearchDecisions, emit Emitter) {
	for i := range searches {
		decision, ok := decisions[searches[i].Key]
		if !ok {
			continue
		}
		switch decision.Status {
		case "accepted":
			candidate := bookCandidateForDecision(searches[i], decision)
			searches[i].Accepted = true
			searches[i].Reason = ""
			searches[i].ProviderID = candidate.ProviderID
			searches[i].Provider = candidate.Provider
			searches[i].Title = candidate.Title
			searches[i].Author = candidate.Author
			searches[i].Year = candidate.Year
			searches[i].Confidence = candidate.Confidence
			searches[i].ExternalIDs = candidate.ExternalIDs
			searches[i].ManualDecision = decision.Status
			emitDecisionOverlay(emit, "book", searches[i].Key, decision)
		case "rejected", "ignored":
			searches[i].Accepted = false
			searches[i].Reason = "manual_" + decision.Status
			searches[i].ProviderID = ""
			searches[i].Provider = ""
			searches[i].ExternalIDs = nil
			searches[i].ManualDecision = decision.Status
			emitDecisionOverlay(emit, "book", searches[i].Key, decision)
		}
	}
}

func bookCandidateForDecision(search BookSearchMatch, decision SearchDecision) BookSearchCandidate {
	for _, candidate := range search.Candidates {
		if candidate.ProviderID == decision.ProviderID {
			if candidate.Confidence == 0 {
				candidate.Confidence = firstPositiveFloat64(decision.Confidence, 1)
			}
			return candidate
		}
	}
	return BookSearchCandidate{
		ProviderID:  decision.ProviderID,
		Provider:    firstNonEmpty(decision.Provider, "heya"),
		Title:       firstNonEmpty(decision.Title, search.Title, search.Query.Title),
		Author:      firstNonEmpty(search.Author, search.Query.Author),
		Year:        firstNonEmpty(decision.Year, search.Year, search.Query.Year),
		Confidence:  firstPositiveFloat64(decision.Confidence, 1),
		ExternalIDs: decision.ExternalIDs,
	}
}

func emitDecisionOverlay(emit Emitter, domain, key string, decision SearchDecision) {
	if emit == nil {
		return
	}
	emit.Emit(Event{
		Event: "match.decision_overlay",
		Kind:  domain,
		Data: map[string]any{
			"key":         key,
			"status":      decision.Status,
			"provider_id": decision.ProviderID,
		},
	})
}

func firstPositiveFloat64(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
