package scanner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/titlematch"
)

const musicArtistAutoMatchThreshold = 0.85
const musicArtistSearchTimeout = 3 * time.Minute
const musicArtistSearchConcurrency = 4

type MusicSearchProvider interface {
	Search(context.Context, metadata.MediaKind, metadata.SearchQuery) ([]metadata.SearchResult, error)
}

type MusicSearchMatch struct {
	Key            string                 `json:"key"`
	Query          MusicSearchQuery       `json:"query"`
	Accepted       bool                   `json:"accepted"`
	Reason         string                 `json:"reason,omitempty"`
	Error          string                 `json:"error,omitempty"`
	ProviderID     string                 `json:"provider_id,omitempty"`
	Provider       string                 `json:"provider,omitempty"`
	Artist         string                 `json:"artist,omitempty"`
	Confidence     float64                `json:"confidence"`
	Candidates     []MusicSearchCandidate `json:"candidates,omitempty"`
	ExternalIDs    map[string]string      `json:"external_ids,omitempty"`
	ManualDecision string                 `json:"manual_decision,omitempty"`
}

type MusicSearchQuery struct {
	Artist  string   `json:"artist"`
	Aliases []string `json:"aliases,omitempty"`
}

type MusicSearchCandidate struct {
	ProviderID  string            `json:"provider_id"`
	Provider    string            `json:"provider"`
	Artist      string            `json:"artist"`
	Description string            `json:"description,omitempty"`
	PosterURL   string            `json:"poster_url,omitempty"`
	HeyaSlug    string            `json:"heya_slug,omitempty"`
	Confidence  float64           `json:"confidence"`
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
}

func SearchMusicArtists(ctx context.Context, artists []MusicArtistPlan, provider MusicSearchProvider, emit Emitter, decisionsOpt ...SearchDecisions) ([]MusicSearchMatch, error) {
	if provider == nil {
		return nil, fmt.Errorf("music search provider is required")
	}

	decisions := optionalSearchDecisions(decisionsOpt)
	results := make([]MusicSearchMatch, len(artists))
	sem := make(chan struct{}, musicArtistSearchConcurrency)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var runErr error
	setErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		defer errMu.Unlock()
		if runErr == nil {
			runErr = err
		}
	}

	for i, artist := range artists {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(i int, artist MusicArtistPlan) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := ctx.Err(); err != nil {
				setErr(err)
				return
			}
			results[i] = searchOneMusicArtist(ctx, artist, provider, emit, decisions)
		}(i, artist)
	}
	wg.Wait()
	if runErr != nil {
		return results, runErr
	}

	accepted := 0
	for _, result := range results {
		if result.Accepted {
			accepted++
		}
	}
	emit.Emit(Event{Event: "match.search_summary", Data: map[string]any{"domain": "music", "matches": len(results), "accepted": accepted}})
	return results, nil
}

func searchOneMusicArtist(ctx context.Context, artist MusicArtistPlan, provider MusicSearchProvider, emit Emitter, decisions SearchDecisions) MusicSearchMatch {
	query := metadata.SearchQuery{Title: artist.Artist}
	search := MusicSearchMatch{
		Key:   artist.Key,
		Query: MusicSearchQuery{Artist: artist.Artist},
	}
	emit.Emit(Event{
		Event: "match.search",
		Kind:  "music",
		Data: map[string]any{
			"key":    artist.Key,
			"artist": artist.Artist,
		},
	})

	if decision, ok := decisions[artist.Key]; ok {
		if applied, handled := applyMusicSearchDecision(artist, search, decision, emit); handled {
			return applied
		}
	}

	if direct, ok := directMusicSearchMatch(artist, emit); ok {
		return direct
	}

	searchCtx, cancel := context.WithTimeout(ctx, musicArtistSearchTimeout)
	candidates, err := provider.Search(searchCtx, metadata.KindMusic, query)
	cancel()
	if err != nil {
		search.Reason = "search_error"
		search.Error = err.Error()
		emit.Emit(Event{
			Event:    "match.search_failed",
			Severity: SeverityWarn,
			Kind:     "music",
			Reason:   search.Reason,
			Message:  err.Error(),
			Data: map[string]any{
				"key":    artist.Key,
				"artist": artist.Artist,
			},
		})
		return search
	}

	scored := make([]metadata.SearchResult, len(candidates))
	copy(scored, candidates)
	for i := range scored {
		scored[i].Confidence = scoreMusicSearchCandidate(artist, scored[i])
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Confidence == scored[j].Confidence {
			iExact := musicSearchArtistExact(artist, scored[i].Title)
			jExact := musicSearchArtistExact(artist, scored[j].Title)
			if iExact != jExact {
				return iExact
			}
			iCase := strings.TrimSpace(scored[i].Title) == strings.TrimSpace(artist.Artist)
			jCase := strings.TrimSpace(scored[j].Title) == strings.TrimSpace(artist.Artist)
			if iCase != jCase {
				return iCase
			}
			if iCount, jCount := len(scored[i].ExternalIDs), len(scored[j].ExternalIDs); iCount != jCount {
				return iCount > jCount
			}
			if rankI, rankJ := musicSearchProviderRank(scored[i]), musicSearchProviderRank(scored[j]); rankI != rankJ {
				return rankI < rankJ
			}
			return scored[i].Title < scored[j].Title
		}
		return scored[i].Confidence > scored[j].Confidence
	})

	for _, candidate := range scored {
		providerID := musicPreferredProviderID(candidate)
		search.Candidates = append(search.Candidates, MusicSearchCandidate{
			ProviderID:  providerID,
			Provider:    candidate.ProviderName,
			Artist:      candidate.Title,
			Description: candidate.Description,
			PosterURL:   candidate.PosterURL,
			HeyaSlug:    candidate.HeyaSlug,
			Confidence:  candidate.Confidence,
			ExternalIDs: candidate.ExternalIDs,
		})
		emit.Emit(Event{
			Event: "match.candidate",
			Kind:  "music",
			Data: map[string]any{
				"key":          artist.Key,
				"provider_id":  providerID,
				"artist":       candidate.Title,
				"confidence":   candidate.Confidence,
				"external_ids": candidate.ExternalIDs,
			},
		})
	}

	if len(scored) == 0 {
		search.Reason = "no_candidates"
		emit.Emit(Event{Event: "match.unresolved", Kind: "music", Reason: search.Reason, Data: map[string]any{"key": artist.Key, "artist": artist.Artist}})
		return search
	}

	top := scored[0]
	clearGap := musicSearchClearGap(scored, artist.Artist)
	if top.Confidence >= musicArtistAutoMatchThreshold && clearGap {
		providerID := musicPreferredProviderID(top)
		search.Accepted = true
		search.ProviderID = providerID
		search.Provider = top.ProviderName
		search.Artist = top.Title
		search.Confidence = top.Confidence
		search.ExternalIDs = top.ExternalIDs
		emit.Emit(Event{
			Event: "match.selected",
			Kind:  "music",
			Data: map[string]any{
				"key":          artist.Key,
				"provider_id":  providerID,
				"artist":       top.Title,
				"confidence":   top.Confidence,
				"external_ids": top.ExternalIDs,
			},
		})
	} else {
		search.Reason = "ambiguous_or_low_confidence"
		search.Confidence = top.Confidence
		emit.Emit(Event{
			Event:  "match.rejected",
			Kind:   "music",
			Reason: search.Reason,
			Data: map[string]any{
				"key":        artist.Key,
				"top_artist": top.Title,
				"confidence": top.Confidence,
				"clear_gap":  clearGap,
			},
		})
	}
	return search
}

func directMusicSearchMatch(artist MusicArtistPlan, emit Emitter) (MusicSearchMatch, bool) {
	provider, value := musicDirectArtistID(artist.ExternalIDs)
	if provider == "" || value == "" {
		return MusicSearchMatch{}, false
	}
	providerID := "heya:artist:" + provider + ":" + value
	externalIDs := cloneStringMap(artist.ExternalIDs)
	candidate := MusicSearchCandidate{
		ProviderID:  providerID,
		Provider:    "heya",
		Artist:      artist.Artist,
		Confidence:  1,
		ExternalIDs: externalIDs,
	}
	search := MusicSearchMatch{
		Key:         artist.Key,
		Query:       MusicSearchQuery{Artist: artist.Artist},
		Accepted:    true,
		ProviderID:  providerID,
		Provider:    "heya",
		Artist:      artist.Artist,
		Confidence:  1,
		Candidates:  []MusicSearchCandidate{candidate},
		ExternalIDs: externalIDs,
	}
	emit.Emit(Event{
		Event: "match.direct_selected",
		Kind:  "music",
		Data: map[string]any{
			"key":          artist.Key,
			"provider_id":  providerID,
			"artist":       artist.Artist,
			"confidence":   search.Confidence,
			"external_ids": externalIDs,
		},
	})
	return search, true
}

func musicDirectArtistID(ids map[string]string) (provider, value string) {
	for _, provider := range []string{"mbid", "apple", "discogs", "deezer"} {
		if ids[provider] != "" {
			return provider, ids[provider]
		}
	}
	return "", ""
}

func applyMusicSearchDecision(artist MusicArtistPlan, search MusicSearchMatch, decision SearchDecision, emit Emitter) (MusicSearchMatch, bool) {
	switch decision.Status {
	case "accepted":
		if decision.ProviderID == "" {
			return search, false
		}
		candidate := MusicSearchCandidate{
			ProviderID:  decision.ProviderID,
			Provider:    firstNonEmpty(decision.Provider, "heya"),
			Artist:      firstNonEmpty(decision.Title, artist.Artist),
			Confidence:  decision.Confidence,
			ExternalIDs: decision.ExternalIDs,
		}
		if candidate.Confidence == 0 {
			candidate.Confidence = 1
		}
		search.Accepted = true
		search.ProviderID = candidate.ProviderID
		search.Provider = candidate.Provider
		search.Artist = candidate.Artist
		search.Confidence = candidate.Confidence
		search.ExternalIDs = candidate.ExternalIDs
		search.Candidates = []MusicSearchCandidate{candidate}
		search.ManualDecision = decision.Status
		emit.Emit(Event{
			Event: "match.manual_selected",
			Kind:  "music",
			Data: map[string]any{
				"key":          artist.Key,
				"provider_id":  candidate.ProviderID,
				"artist":       candidate.Artist,
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
			Kind:   "music",
			Reason: search.Reason,
			Data: map[string]any{
				"key":    artist.Key,
				"status": decision.Status,
				"artist": artist.Artist,
			},
		})
		return search, true
	default:
		return search, false
	}
}

func scoreMusicSearchCandidate(artist MusicArtistPlan, candidate metadata.SearchResult) float64 {
	if sharedExternalID(artist.ExternalIDs, candidate.ExternalIDs) {
		return 1
	}
	primary := musicNameSimilarity(artist.Artist, candidate.Title)
	best := primary
	for _, alt := range candidate.AltTitles {
		if alt == "" {
			continue
		}
		if score := musicNameSimilarity(artist.Artist, alt); score > best {
			if musicShortAliasNeedsPrimarySupport(artist.Artist, candidate.Title) && score >= musicArtistAutoMatchThreshold {
				score = maxFloat(primary, 0.80)
			}
			best = score
		}
	}
	return best
}

func musicShortAliasNeedsPrimarySupport(query, primaryTitle string) bool {
	nq := normalizeMusicKeyPart(query)
	if len(strings.Fields(nq)) != 1 || len(nq) >= 5 {
		return false
	}
	return nq != normalizeMusicKeyPart(primaryTitle)
}

func musicNameSimilarity(a, b string) float64 {
	na := normalizeMusicKeyPart(a)
	nb := normalizeMusicKeyPart(b)
	if na == nb && na != "" {
		return 1
	}
	if na == "" || nb == "" {
		return 0
	}
	if musicNumberedDisambiguationMismatch(a, b) {
		return musicNormalizedSimilarity(na, nb)
	}
	if titlematch.FuzzyEqual(a, b) && musicFuzzyMatchSafe(na, nb) {
		return 1
	}
	return musicNormalizedSimilarity(na, nb)
}

func musicFuzzyMatchSafe(na, nb string) bool {
	aFields := strings.Fields(na)
	bFields := strings.Fields(nb)
	if len(aFields) == 0 || len(bFields) == 0 {
		return false
	}
	if len(aFields) == len(bFields) {
		if len(aFields) == 1 && minInt(len(na), len(nb)) < 5 {
			return false
		}
		return true
	}
	shorterLen := minInt(len(na), len(nb))
	return absInt(len(aFields)-len(bFields)) <= 1 && shorterLen >= 8
}

func musicNormalizedSimilarity(na, nb string) float64 {
	if na == nb && na != "" {
		return 1
	}
	if na == "" || nb == "" {
		return 0
	}
	d := levenshteinDistance(na, nb)
	maxLen := len(na)
	if len(nb) > maxLen {
		maxLen = len(nb)
	}
	score := 1 - float64(d)/float64(maxLen)
	if substringSearchTitleMatch(na, nb) && score < 0.80 {
		score = 0.80
	}
	return score
}

func musicNumberedDisambiguationMismatch(a, b string) bool {
	sa := strings.TrimSpace(musicNumberedDisambigRE.ReplaceAllString(a, ""))
	sb := strings.TrimSpace(musicNumberedDisambigRE.ReplaceAllString(b, ""))
	if sa == a && sb == b {
		return false
	}
	return normalizeMusicKeyPart(sa) == normalizeMusicKeyPart(sb)
}

func musicSearchClearGap(results []metadata.SearchResult, queryArtist string) bool {
	if len(results) == 1 {
		return true
	}
	top := results[0]
	secondDifferent := -1
	for i := 1; i < len(results); i++ {
		if normalizeMusicKeyPart(results[i].Title) != normalizeMusicKeyPart(top.Title) {
			secondDifferent = i
			break
		}
	}
	if secondDifferent == -1 || top.Confidence-results[secondDifferent].Confidence > 0.10 {
		return true
	}
	return musicSearchArtistExact(MusicArtistPlan{Artist: queryArtist}, top.Title)
}

func musicSearchArtistExact(artist MusicArtistPlan, candidate string) bool {
	return normalizeMusicKeyPart(artist.Artist) == normalizeMusicKeyPart(candidate)
}

func musicSearchProviderRank(result metadata.SearchResult) int {
	if len(result.ExternalIDs) > 0 {
		return musicExternalIDsProviderRank(result.ExternalIDs)
	}
	provider := musicSearchProviderFromID(result.ProviderID)
	if provider == "" {
		return 99
	}
	return musicProviderRank(provider)
}

func musicExternalIDsProviderRank(ids map[string]string) int {
	for _, provider := range []string{"mbid", "musicbrainz", "apple", "deezer", "discogs"} {
		if ids[provider] != "" {
			return musicProviderRank(provider)
		}
	}
	return 99
}

func musicPreferredProviderID(result metadata.SearchResult) string {
	if value := firstNonEmpty(result.ExternalIDs["mbid"], result.ExternalIDs["musicbrainz"]); value != "" {
		return "heya:artist:mbid:" + value
	}
	if value := result.ExternalIDs["apple"]; value != "" {
		return "heya:artist:apple:" + value
	}
	if value := result.ExternalIDs["deezer"]; value != "" {
		return "heya:artist:deezer:" + value
	}
	if value := result.ExternalIDs["discogs"]; value != "" {
		return "heya:artist:discogs:" + value
	}
	return result.ProviderID
}

func musicSearchProviderFromID(providerID string) string {
	rest := strings.TrimPrefix(providerID, "heya:")
	parts := strings.SplitN(rest, ":", 3)
	if len(parts) != 3 {
		return ""
	}
	return parts[1]
}

func musicProviderRank(provider string) int {
	switch provider {
	case "mbid", "musicbrainz":
		return 0
	case "apple":
		return 1
	case "deezer":
		return 2
	case "discogs":
		return 3
	default:
		return 99
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for k, v := range values {
		out[k] = v
	}
	return out
}

func sortMusicSearchResults(items []MusicSearchMatch) {
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Query.Artist) < strings.ToLower(items[j].Query.Artist)
	})
}
