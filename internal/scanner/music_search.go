package scanner

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/karbowiak/heya/internal/titlematch"
)

const musicArtistAutoMatchThreshold = 0.85
const musicArtistSearchTimeout = 3 * time.Minute
const musicArtistSearchConcurrency = 4
const musicArtistDiscoveryReleaseHintLimit = 3

// Literal artist identity is always searched first. These separators are used
// only for the second-chance primary-credit lookup after that literal lookup
// fails, so a fixed-name group such as "Above & Beyond" is never split while
// its canonical identity is available. Keep this aligned with HeyaMetadata's
// retained-credit parser; the deliberately compact production regression
// corpus covers every form seen in the real music library.
var musicCollaborationSeparatorRE = regexp.MustCompile(`(?i)(?:\s+(?:&|and|with|w/|feat\.?|featuring|ft\.?|f/|x|×|vs\.?|versus|presents|meets|/)\s+|\s+f\.\s*|\s*;\s+|\s+:\s+)`)

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
	Artist   string                 `json:"artist"`
	Aliases  []string               `json:"aliases,omitempty"`
	Releases []metadata.ReleaseHint `json:"releases,omitempty"`
}

type MusicSearchCandidate struct {
	ProviderID     string                    `json:"provider_id"`
	Provider       string                    `json:"provider"`
	Artist         string                    `json:"artist"`
	Description    string                    `json:"description,omitempty"`
	PosterURL      string                    `json:"poster_url,omitempty"`
	HeyaSlug       string                    `json:"heya_slug,omitempty"`
	Confidence     float64                   `json:"confidence"`
	Recommendation string                    `json:"recommendation,omitempty"`
	Evidence       []metadata.SearchEvidence `json:"evidence,omitempty"`
	RequiresReview bool                      `json:"requires_review,omitempty"`
	ExternalIDs    map[string]string         `json:"external_ids,omitempty"`
}

func SearchMusicArtists(ctx context.Context, artists []MusicArtistPlan, provider MusicSearchProvider, emit Emitter, threshold float64, decisionsOpt ...SearchDecisions) ([]MusicSearchMatch, error) {
	if provider == nil {
		return nil, fmt.Errorf("music search provider is required")
	}
	if threshold <= 0 {
		threshold = musicArtistAutoMatchThreshold
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
			result, err := searchOneMusicArtist(ctx, artist, provider, emit, threshold, decisions)
			if err != nil {
				setErr(err)
				return
			}
			results[i] = result
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

func searchOneMusicArtist(ctx context.Context, artist MusicArtistPlan, provider MusicSearchProvider, emit Emitter, threshold float64, decisions SearchDecisions) (MusicSearchMatch, error) {
	releases := musicDiscoveryReleaseHints(artist.Albums)
	identifiers := musicDiscoveryArtistIdentifiers(artist.ExternalIDs)
	if identifiers["mbid"] != "" {
		// A consistent MusicBrainz artist ID is the authoritative artist spine.
		// Release identifiers remain useful when no artist spine is known, but
		// submitting them alongside an exact MBID lets one stale album/NFO turn
		// an otherwise exact artist lookup into conflicting-identifiers review.
		releases = musicReleaseHintsWithoutIdentifiers(releases)
	}
	query := metadata.SearchQuery{Title: artist.Artist, Identifiers: identifiers, Releases: releases}
	search := MusicSearchMatch{
		Key:   artist.Key,
		Query: MusicSearchQuery{Artist: artist.Artist, Releases: releases},
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
			return applied, nil
		}
	}

	searchCtx, cancel := context.WithTimeout(ctx, musicArtistSearchTimeout)
	candidates, err := provider.Search(searchCtx, metadata.KindMusic, query)
	cancel()
	if err != nil {
		if _, deferred := metadata.DeferredWorkRetryAfter(err); deferred {
			return search, err
		}
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
		return search, nil
	}

	selectionArtist := artist
	scored := scoreMusicSearchResults(artist, candidates)
	if !musicSearchCanAutoAccept(scored, artist.Artist, threshold) {
		if primary := musicPrimaryCollaborationArtist(artist.Artist); primary != "" {
			fallbackArtist := artist
			fallbackArtist.Artist = primary
			fallbackArtist.ExternalIDs = nil
			fallbackQuery := metadata.SearchQuery{Title: primary, Releases: releases}
			fallbackCtx, fallbackCancel := context.WithTimeout(ctx, musicArtistSearchTimeout)
			fallbackCandidates, fallbackErr := provider.Search(fallbackCtx, metadata.KindMusic, fallbackQuery)
			fallbackCancel()
			if fallbackErr != nil {
				if _, deferred := metadata.DeferredWorkRetryAfter(fallbackErr); deferred {
					return search, fallbackErr
				}
				emit.Emit(Event{
					Event: "match.collaboration_fallback_failed", Severity: SeverityInfo, Kind: "music",
					Message: fallbackErr.Error(),
					Data:    map[string]any{"key": artist.Key, "artist": artist.Artist, "primary_artist": primary},
				})
			} else {
				selectionArtist = fallbackArtist
				search.Query.Aliases = []string{artist.Artist}
				scored = mergeScoredMusicSearchResults(scored, scoreMusicSearchResults(fallbackArtist, fallbackCandidates))
				sortMusicSearchCandidates(scored, selectionArtist)
				emit.Emit(Event{
					Event: "match.collaboration_fallback", Kind: "music",
					Data: map[string]any{
						"key": artist.Key, "artist": artist.Artist, "primary_artist": primary,
						"candidates": len(fallbackCandidates),
					},
				})
			}
		}
	}

	if !musicSearchCanAutoAccept(scored, selectionArtist.Artist, threshold) {
		converged, ok, err := resolveConvergedMusicCandidates(ctx, selectionArtist, scored, provider, threshold, emit)
		if err != nil {
			return search, err
		}
		if ok {
			scored = []metadata.SearchResult{converged}
		}
	}

	for _, candidate := range scored {
		providerID := musicPreferredProviderID(candidate)
		search.Candidates = append(search.Candidates, MusicSearchCandidate{
			ProviderID:     providerID,
			Provider:       candidate.ProviderName,
			Artist:         candidate.Title,
			Description:    candidate.Description,
			PosterURL:      candidate.PosterURL,
			HeyaSlug:       candidate.HeyaSlug,
			Confidence:     candidate.Confidence,
			Recommendation: candidate.Recommendation,
			Evidence:       candidate.Evidence,
			RequiresReview: candidate.RequiresReview,
			ExternalIDs:    candidate.ExternalIDs,
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
		return search, nil
	}

	top := scored[0]
	clearGap := musicSearchClearGap(scored, selectionArtist.Artist)
	if !top.RequiresReview && top.Confidence >= threshold && clearGap {
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
	return search, nil
}

func scoreMusicSearchResults(artist MusicArtistPlan, candidates []metadata.SearchResult) []metadata.SearchResult {
	scored := append([]metadata.SearchResult(nil), candidates...)
	for i := range scored {
		scored[i].Confidence = scoreMusicSearchCandidate(artist, scored[i])
	}
	sortMusicSearchCandidates(scored, artist)
	return scored
}

func sortMusicSearchCandidates(scored []metadata.SearchResult, artist MusicArtistPlan) {
	sort.Slice(scored, func(i, j int) bool {
		// Query-only canonical hits are deliberately review-only when artist
		// discovery has structured release evidence. Never let their exact-name
		// score tie place them ahead of the provider-approved discovery result.
		if scored[i].RequiresReview != scored[j].RequiresReview {
			return !scored[i].RequiresReview
		}
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
			return scored[i].Title < scored[j].Title
		}
		return scored[i].Confidence > scored[j].Confidence
	})
}

func musicSearchCanAutoAccept(scored []metadata.SearchResult, queryArtist string, threshold float64) bool {
	if len(scored) == 0 {
		return false
	}
	top := scored[0]
	return !top.RequiresReview && top.Confidence >= threshold && musicSearchClearGap(scored, queryArtist)
}

func musicPrimaryCollaborationArtist(value string) string {
	parts := musicCollaborationSeparatorRE.Split(strings.TrimSpace(value), 2)
	if len(parts) != 2 {
		return ""
	}
	primary := strings.TrimSpace(parts[0])
	if primary == "" || strings.TrimSpace(parts[1]) == "" || normalizeMusicKeyPart(primary) == normalizeMusicKeyPart(value) {
		return ""
	}
	return primary
}

func mergeScoredMusicSearchResults(groups ...[]metadata.SearchResult) []metadata.SearchResult {
	indices := map[string]int{}
	var merged []metadata.SearchResult
	for _, group := range groups {
		for _, candidate := range group {
			if index, ok := indices[candidate.ProviderID]; ok {
				current := merged[index]
				if (current.RequiresReview && !candidate.RequiresReview) ||
					(current.RequiresReview == candidate.RequiresReview && candidate.Confidence > current.Confidence) {
					merged[index] = candidate
				}
				continue
			}
			indices[candidate.ProviderID] = len(merged)
			merged = append(merged, candidate)
		}
	}
	return merged
}

func musicDiscoveryReleaseHints(albums []MusicAlbumPlan) []metadata.ReleaseHint {
	if len(albums) == 0 {
		return nil
	}
	candidates := append([]MusicAlbumPlan(nil), albums...)
	sort.SliceStable(candidates, func(i, j int) bool {
		iPriority := musicDiscoveryReleasePriority(candidates[i].ReleaseKind)
		jPriority := musicDiscoveryReleasePriority(candidates[j].ReleaseKind)
		if iPriority != jPriority {
			return iPriority > jPriority
		}
		if len(candidates[i].Tracks) != len(candidates[j].Tracks) {
			return len(candidates[i].Tracks) > len(candidates[j].Tracks)
		}
		if candidates[i].Year != candidates[j].Year {
			return candidates[i].Year < candidates[j].Year
		}
		return candidates[i].Album < candidates[j].Album
	})

	seen := make(map[string]struct{}, musicArtistDiscoveryReleaseHintLimit)
	hints := make([]metadata.ReleaseHint, 0, musicArtistDiscoveryReleaseHintLimit)
	for _, album := range candidates {
		title := strings.TrimSpace(album.Album)
		key := normalizeMusicKeyPart(title)
		if key == "" {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		hints = append(hints, metadata.ReleaseHint{
			Title:       title,
			Year:        album.Year,
			Type:        album.ReleaseKind,
			Identifiers: musicReleaseHintIdentifiers(album.ExternalIDs),
		})
		if len(hints) == musicArtistDiscoveryReleaseHintLimit {
			break
		}
	}
	return hints
}

func musicDiscoveryArtistIdentifiers(values map[string]string) map[string]string {
	if mbid := strings.TrimSpace(values["mbid"]); mbid != "" {
		return map[string]string{"mbid": mbid}
	}
	if apple := strings.TrimSpace(values["apple"]); apple != "" {
		return map[string]string{"apple": apple}
	}
	return nil
}

func musicReleaseHintsWithoutIdentifiers(values []metadata.ReleaseHint) []metadata.ReleaseHint {
	if len(values) == 0 {
		return nil
	}
	result := append([]metadata.ReleaseHint(nil), values...)
	for i := range result {
		result[i].Identifiers = nil
	}
	return result
}

// resolveConvergedMusicCandidates handles the safe subset of duplicate
// review candidates: opaque conflict candidates which all resolve (including
// redirects) to one canonical Heya entity. Same labels alone are never enough;
// genuinely distinct same-name artists retain their separate canonical IDs
// and stay in review.
func resolveConvergedMusicCandidates(ctx context.Context, artist MusicArtistPlan, scored []metadata.SearchResult, provider MusicSearchProvider, threshold float64, emit Emitter) (metadata.SearchResult, bool, error) {
	if len(scored) < 2 || scored[0].Recommendation != "conflicting_identifiers" ||
		scored[0].Confidence < threshold || !musicSearchArtistExact(artist, scored[0].Title) {
		return metadata.SearchResult{}, false, nil
	}
	detailProvider, ok := provider.(MusicDetailProvider)
	if !ok {
		return metadata.SearchResult{}, false, nil
	}

	top := scored[0]
	topTitle := normalizeMusicKeyPart(top.Title)
	var duplicates []metadata.SearchResult
	for _, candidate := range scored {
		if candidate.Confidence < threshold || top.Confidence-candidate.Confidence > 0.10 {
			continue
		}
		if candidate.Recommendation != top.Recommendation || candidate.RequiresReview != top.RequiresReview {
			continue
		}
		if normalizeMusicKeyPart(candidate.Title) != topTitle {
			return metadata.SearchResult{}, false, nil
		}
		duplicates = append(duplicates, candidate)
	}
	if len(duplicates) < 2 || len(duplicates) > musicFetchCandidateLimit {
		return metadata.SearchResult{}, false, nil
	}

	canonicalID := ""
	var canonical *metadata.MediaDetail
	for _, candidate := range duplicates {
		fetchCtx, cancel := context.WithTimeout(ctx, musicMetadataFetchTimeout)
		detail, err := detailProvider.GetDetail(fetchCtx, candidate.ProviderID, nil)
		cancel()
		if err != nil {
			if _, deferred := metadata.DeferredWorkRetryAfter(err); deferred {
				return metadata.SearchResult{}, false, err
			}
			emit.Emit(Event{Event: "match.candidate_convergence_failed", Severity: SeverityInfo, Kind: "music", Message: err.Error(), Data: map[string]any{
				"key": artist.Key, "provider_id": candidate.ProviderID,
			}})
			return metadata.SearchResult{}, false, nil
		}
		if detail == nil || strings.TrimSpace(detail.CanonicalID) == "" {
			return metadata.SearchResult{}, false, nil
		}
		if canonicalID == "" {
			canonicalID = detail.CanonicalID
			canonical = detail
		} else if canonicalID != detail.CanonicalID {
			return metadata.SearchResult{}, false, nil
		}
	}

	result := top
	result.ProviderID = heyametadata.EncodeEntityProviderID(canonicalID)
	result.Title = firstNonEmpty(canonical.ArtistName, canonical.Title, top.Title)
	result.Description = firstNonEmpty(canonical.ArtistDisambiguation, top.Description)
	result.ExternalIDs = cloneStringMap(canonical.ExternalIDs)
	result.HeyaSlug = canonicalID
	result.Recommendation = "canonical_convergence"
	result.RequiresReview = false
	result.Enriched = true
	emit.Emit(Event{Event: "match.candidates_converged", Kind: "music", Data: map[string]any{
		"key": artist.Key, "artist": artist.Artist, "canonical_id": canonicalID, "candidates": len(duplicates),
	}})
	return result, true, nil
}

// musicReleaseHintIdentifiers keeps exact release/catalog identifiers while
// excluding artist-level evidence that happens to be carried by the album
// plan. HeyaMetadata owns provider routing and namespace normalization; Heya
// merely preserves the structured evidence it already parsed from tags/NFOs.
func musicReleaseHintIdentifiers(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	allowed := map[string]struct{}{
		"apple_album": {}, "apple_music_album": {}, "itunes_album": {},
		"deezer_album": {}, "discogs_release": {}, "discogs_master": {},
		"musicbrainz_album": {}, "musicbrainz_release_group": {},
		"spotify_album": {}, "audiodb_album": {},
	}
	result := make(map[string]string)
	for key, value := range values {
		if _, ok := allowed[key]; ok && strings.TrimSpace(value) != "" {
			result[key] = strings.TrimSpace(value)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func musicDiscoveryReleasePriority(releaseType string) int {
	switch normalizeMusicReleaseKind(releaseType) {
	case "album":
		return 4
	case "ep":
		return 3
	case "single":
		return 2
	case "compilation":
		return 1
	default:
		return 0
	}
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
		if !top.RequiresReview && results[i].RequiresReview {
			// Provider-approved discovery evidence outranks query-only canonical
			// suggestions which the provider explicitly marked for review.
			continue
		}
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

func musicPreferredProviderID(result metadata.SearchResult) string {
	return result.ProviderID
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
