package heyametadata

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/karbowiak/heya/clients/heyametadata"
	"github.com/karbowiak/heya/internal/metadata"
)

// HeyaProvider adapts kind-specific HeyaMetadata V2 canonical documents to
// Heya's existing relational read models. The adapter is intentionally the
// only place where the transitional MediaDetail shape is assembled.
type HeyaProvider struct {
	client      *Client
	store       *workflowStore
	credentials ProviderCredentials
}

// WithProviderCredentials keeps optional upstream credentials in process
// memory and adds them only to individual HeyaMetadata requests. Workflow
// keys/rows, errors, and logs never receive these values.
func (p *HeyaProvider) WithProviderCredentials(credentials ProviderCredentials) *HeyaProvider {
	p.credentials = credentials
	return p
}

func NewHeyaProvider(client *Client, db ...*pgxpool.Pool) *HeyaProvider {
	var pool *pgxpool.Pool
	if len(db) > 0 {
		pool = db[0]
	}
	return &HeyaProvider{client: client, store: newWorkflowStore(pool)}
}

// Name remains "heya" while local provider_name columns are a compatibility
// label. Network traffic and canonical identity are exclusively V2.
func (*HeyaProvider) Name() string { return "heya" }

// RecordingLyrics reads lyric evidence by canonical recording UUID. Provider
// provenance is intentionally not exposed to callers: it is passive evidence,
// not an identity or routing mechanism.
func (p *HeyaProvider) RecordingLyrics(ctx context.Context, entityID string) ([]RecordingLyrics, error) {
	return p.client.RecordingLyrics(ctx, entityID, p.credentials)
}

// ResolveRecordingMBID lets Heya turn direct AcoustID evidence into the same
// canonical recording/artist graph used by normal metadata ingestion. The
// MusicBrainz identifier is submitted as evidence; Heya never constructs or
// persists a provider-specific canonical identity itself.
func (p *HeyaProvider) ResolveRecordingMBID(ctx context.Context, mbid string) (metadata.RecordingMetadata, error) {
	mbid = strings.ToLower(strings.TrimSpace(mbid))
	if mbid == "" {
		return metadata.RecordingMetadata{}, errors.New("recording MBID is required")
	}
	request := discoveryRequest("recording", metadata.SearchQuery{
		Identifiers: map[string]string{"musicbrainz_recording": mbid},
	})
	resource, err := p.client.Discover(ctx, request, p.credentials, p.store)
	if err != nil {
		return metadata.RecordingMetadata{}, err
	}
	if resource == nil || resource.Result == nil || resource.Result.EntityId == nil {
		return metadata.RecordingMetadata{}, fmt.Errorf("recording MBID %s did not resolve to a canonical entity", mbid)
	}
	return p.client.RecordingMetadata(ctx, resource.Result.EntityId.String(), p.credentials)
}

func canonicalKind(kind metadata.MediaKind, explicit string, externalIDs map[string]string) string {
	if explicit != "" {
		return explicit
	}
	switch kind {
	case metadata.KindMovie:
		return "movie"
	case metadata.KindMusic:
		return "artist"
	case metadata.KindBook:
		return "book_work"
	case metadata.KindTV:
		if externalIDs["anidb"] != "" || externalIDs["mal"] != "" {
			return "anime"
		}
		return "tv_show"
	default:
		return ""
	}
}

func legacyKind(kind string) string {
	switch kind {
	case "tv_show", "anime":
		return "tv"
	case "release_group":
		return "album"
	case "book_work", "book_edition":
		return "book"
	default:
		return kind
	}
}

// BuildLookupIDs returns canonical IDs first, followed by every external
// identifier as migration evidence. GetDetailFallback reconciles the complete
// evidence set in one discovery request; ordering never selects an upstream
// identity source.
func BuildLookupIDs(kind metadata.MediaKind, externalIDs map[string]string, canonicalID string) []string {
	canonical := canonicalKind(kind, "", externalIDs)
	var result []string
	if _, err := uuid.Parse(canonicalID); err == nil {
		result = append(result, EncodeEntityProviderID(canonicalID))
	}
	for _, key := range providerOrder(canonical) {
		if value := strings.TrimSpace(externalIDs[key]); value != "" {
			result = append(result, encodeExternalProviderID(canonical, key, value))
		}
	}
	return dedupeStrings(result)
}

func BuildLookupID(kind metadata.MediaKind, externalIDs map[string]string, canonicalID string) string {
	ids := BuildLookupIDs(kind, externalIDs, canonicalID)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func providerOrder(kind string) []string {
	switch kind {
	case "movie":
		return []string{"tmdb", "imdb"}
	case "tv_show":
		return []string{"tvmaze", "tmdb", "tvdb", "imdb"}
	case "anime":
		return []string{"anidb", "mal", "tmdb", "tvdb", "imdb"}
	case "artist":
		return []string{"musicbrainz", "mbid", "discogs", "deezer", "apple"}
	case "release_group":
		return []string{"musicbrainz", "mbid"}
	case "book_work":
		return []string{"openlibrary", "ol_work_id", "isbn"}
	default:
		return nil
	}
}

func encodeExternalProviderID(kind, provider, value string) string {
	return providerIDPrefix + "external:" + kind + ":" + provider + ":" + value
}

type externalReference struct{ Kind, Provider, Value string }

func decodeExternalProviderID(value string) (externalReference, bool) {
	rest := strings.TrimPrefix(value, providerIDPrefix+"external:")
	if rest == value {
		// Accept stored v1 IDs during the backfill window.
		rest = strings.TrimPrefix(value, "heya:")
		if rest == value {
			return externalReference{}, false
		}
	}
	parts := strings.SplitN(rest, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return externalReference{}, false
	}
	kind := parts[0]
	switch kind {
	case "tv":
		if parts[1] == "anidb" || parts[1] == "mal" {
			kind = "anime"
		} else {
			kind = "tv_show"
		}
	case "album":
		kind = "release_group"
	case "book":
		kind = "book_work"
	}
	provider := parts[1]
	if provider == "mbid" {
		provider = "musicbrainz"
	}
	if provider == "ol_work_id" {
		provider = "openlibrary"
	}
	return externalReference{Kind: kind, Provider: provider, Value: parts[2]}, true
}

func (p *HeyaProvider) Search(ctx context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	canonical := canonicalKind(kind, query.CanonicalKind, nil)
	if canonical == "" {
		return nil, fmt.Errorf("heyametadata: unsupported kind %q", kind)
	}
	var localResults []metadata.SearchResult
	if len(query.Identifiers) == 0 {
		year, _ := strconv.Atoi(query.Year)
		local, err := p.client.Search(ctx, canonical, query.Title, year, query.Language)
		if err != nil {
			return nil, err
		}
		localResults = p.mapSummaries(local)
		if localSearchSufficient(localResults, query, canonical) {
			return localResults, nil
		}
		// A fuzzy canonical-index hit is useful as a manual candidate, but it is
		// not enough to suppress query-only discovery.
		for index := range localResults {
			localResults[index].RequiresReview = true
		}
	}

	request := discoveryRequest(canonical, query)
	discovery, err := p.client.Discover(ctx, request, p.credentials, p.store)
	if err != nil {
		return nil, err
	}
	discovered, err := mapDiscovery(discovery, query)
	if err != nil {
		return nil, err
	}
	return mergeSearchResults(localResults, discovered), nil
}

func localSearchSufficient(results []metadata.SearchResult, query metadata.SearchQuery, canonicalKind string) bool {
	// Artist names are not identities. The canonical index can legitimately
	// contain several unrelated artists with the same display name, and its
	// text-search ordering is not identity evidence. Keep local hits as useful
	// review candidates, but require discovery, a durable decision, structured
	// catalog/identifier evidence, or fingerprints before an artist can be
	// selected automatically.
	if canonicalKind == "artist" {
		return false
	}
	// Artist names alone are not durable identity evidence. When the scanner
	// supplied a bounded local discography, let discovery evaluate it instead
	// of accepting the first same-name canonical index hit.
	if len(query.Releases) > 0 {
		return false
	}
	// A release title and year are not globally unique. If the scanner knows
	// the credited artist, discovery must corroborate it instead of accepting a
	// same-named release group from the query-only canonical index.
	if query.CanonicalKind == "release_group" && strings.TrimSpace(query.Artist) != "" {
		return false
	}
	// Canonical book summaries do not currently carry structured authors. An
	// exact title in the local index therefore cannot corroborate an audiobook
	// (or ebook) whose scanner supplied an author; discovery must compare the
	// author evidence before this hit becomes auto-selectable.
	if canonicalKind == "book_work" && strings.TrimSpace(query.Author) != "" {
		return false
	}
	wanted := normalizedLabel(query.Title)
	if wanted == "" {
		return false
	}
	for _, result := range results {
		if query.Year != "" && result.Year != query.Year {
			continue
		}
		if normalizedLabel(result.Title) == wanted {
			return true
		}
		for _, alias := range result.AltTitles {
			if normalizedLabel(alias) == wanted {
				return true
			}
		}
	}
	return false
}

func normalizedLabel(value string) string {
	var normalized strings.Builder
	space := false
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && normalized.Len() > 0 {
				normalized.WriteByte(' ')
			}
			normalized.WriteRune(r)
			space = false
		} else {
			space = true
		}
	}
	return normalized.String()
}

func mergeSearchResults(groups ...[]metadata.SearchResult) []metadata.SearchResult {
	seen := make(map[string]int)
	var result []metadata.SearchResult
	for _, group := range groups {
		for _, candidate := range group {
			if index, ok := seen[candidate.ProviderID]; ok {
				result[index] = mergeEquivalentSearchResult(result[index], candidate)
				continue
			}
			seen[candidate.ProviderID] = len(result)
			result = append(result, candidate)
		}
	}
	return result
}

// mergeEquivalentSearchResult is only used when both paths returned the exact
// same canonical provider ID. Discovery's resolved decision must beat an
// earlier fuzzy local-search review gate, while the canonical summary remains
// useful for artwork, aliases, and already-known external evidence.
func mergeEquivalentSearchResult(existing, incoming metadata.SearchResult) metadata.SearchResult {
	preferred, other := existing, incoming
	if (preferred.RequiresReview && !incoming.RequiresReview) ||
		(preferred.RequiresReview == incoming.RequiresReview && incoming.Confidence > preferred.Confidence) {
		preferred, other = incoming, existing
	}

	preferred.RequiresReview = existing.RequiresReview && incoming.RequiresReview
	preferred.Confidence = max(existing.Confidence, incoming.Confidence)
	preferred.Enriched = existing.Enriched || incoming.Enriched
	preferred.ProviderName = firstNonEmpty(preferred.ProviderName, other.ProviderName)
	preferred.Title = firstNonEmpty(preferred.Title, other.Title)
	preferred.Year = firstNonEmpty(preferred.Year, other.Year)
	preferred.Description = firstNonEmpty(preferred.Description, other.Description)
	preferred.PosterURL = firstNonEmpty(preferred.PosterURL, other.PosterURL)
	preferred.HeyaSlug = firstNonEmpty(preferred.HeyaSlug, other.HeyaSlug)
	preferred.Recommendation = firstNonEmpty(preferred.Recommendation, other.Recommendation)
	if preferred.RawData == nil {
		preferred.RawData = other.RawData
	}
	preferred.ExternalIDs = mergeStringMaps(preferred.ExternalIDs, other.ExternalIDs)
	preferred.AltTitles = mergeStrings(preferred.AltTitles, other.AltTitles)
	preferred.Evidence = mergeEvidence(preferred.Evidence, other.Evidence)
	return preferred
}

func mergeStringMaps(preferred, other map[string]string) map[string]string {
	if len(preferred) == 0 && len(other) == 0 {
		return nil
	}
	merged := cloneStringMap(preferred)
	if merged == nil {
		merged = make(map[string]string, len(other))
	}
	for key, value := range other {
		if merged[key] == "" {
			merged[key] = value
		}
	}
	return merged
}

func mergeStrings(preferred, other []string) []string {
	seen := make(map[string]struct{}, len(preferred)+len(other))
	merged := make([]string, 0, len(preferred)+len(other))
	for _, values := range [][]string{preferred, other} {
		for _, value := range values {
			key := normalizedLabel(value)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, value)
		}
	}
	return merged
}

func mergeEvidence(preferred, other []metadata.SearchEvidence) []metadata.SearchEvidence {
	seen := make(map[string]struct{}, len(preferred)+len(other))
	merged := make([]metadata.SearchEvidence, 0, len(preferred)+len(other))
	for _, values := range [][]metadata.SearchEvidence{preferred, other} {
		for _, value := range values {
			key := value.Field + "\x00" + value.Outcome + "\x00" + value.Detail
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, value)
		}
	}
	return merged
}

func discoveryRequest(kind string, query metadata.SearchQuery) gen.Request {
	limit := int64(20)
	hints := &gen.Hints{}
	if year, err := strconv.ParseInt(query.Year, 10, 64); err == nil && year > 0 {
		hints.Year = &year
	}
	if query.Artist != "" {
		hints.Artists = &[]string{query.Artist}
	}
	if query.Author != "" {
		hints.Authors = &[]string{query.Author}
	}
	if query.ISBN != "" {
		hints.Isbns = &[]string{query.ISBN}
	}
	if query.Language != "" {
		hints.Language = &query.Language
	}
	if query.Country != "" {
		hints.Country = &query.Country
	}
	if query.Format != "" {
		hints.Type = &query.Format
	}
	if len(query.Aliases) > 0 {
		aliases := append([]string(nil), query.Aliases...)
		hints.Aliases = &aliases
	}
	if len(query.Episodes) > 0 {
		episodes := make([]gen.EpisodeHint, 0, len(query.Episodes))
		for _, hint := range query.Episodes {
			episode := gen.EpisodeHint{}
			if hint.Title != "" {
				title := hint.Title
				episode.Title = &title
			}
			if hint.Season > 0 {
				season := int64(hint.Season)
				episode.Season = &season
			}
			if hint.Number > 0 {
				number := int64(hint.Number)
				episode.Number = &number
			}
			if episode.Title != nil || episode.Season != nil || episode.Number != nil {
				episodes = append(episodes, episode)
			}
		}
		if len(episodes) > 0 {
			hints.Episodes = &episodes
		}
	}
	if len(query.Releases) > 0 {
		releases := make([]gen.ReleaseHint, 0, len(query.Releases))
		for _, hint := range query.Releases {
			title := strings.TrimSpace(hint.Title)
			if title == "" {
				continue
			}
			release := gen.ReleaseHint{Title: title}
			if identifiers := discoveryIdentifiers(hint.Identifiers); len(identifiers) > 0 {
				release.Identifiers = &identifiers
			}
			if year, err := strconv.ParseInt(strings.TrimSpace(hint.Year), 10, 64); err == nil && year > 0 {
				release.Year = &year
			}
			if releaseType := strings.TrimSpace(hint.Type); releaseType != "" {
				release.Type = &releaseType
			}
			releases = append(releases, release)
		}
		if len(releases) > 0 {
			hints.Releases = &releases
		}
	}
	request := gen.Request{Kind: kind, Limit: &limit, Hints: hints}
	if title := strings.TrimSpace(query.Title); title != "" {
		request.Query = &title
	}
	identifiers := discoveryIdentifiers(query.Identifiers)
	if len(identifiers) > 0 {
		request.Identifiers = &identifiers
	}
	return request
}

func discoveryIdentifiers(values map[string]string) []gen.Identifier {
	seen := make(map[string]struct{}, len(values))
	identifiers := make([]gen.Identifier, 0, len(values))
	for key, value := range values {
		scheme := identifierScheme(key)
		value = strings.TrimSpace(value)
		if scheme == "" || value == "" {
			continue
		}
		identity := scheme + "\x00" + value
		if _, ok := seen[identity]; ok {
			continue
		}
		seen[identity] = struct{}{}
		identifiers = append(identifiers, gen.Identifier{Scheme: scheme, Value: value})
	}
	sort.Slice(identifiers, func(i, j int) bool {
		if identifiers[i].Scheme != identifiers[j].Scheme {
			return identifiers[i].Scheme < identifiers[j].Scheme
		}
		return identifiers[i].Value < identifiers[j].Value
	})
	return identifiers
}

func identifierScheme(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if index := strings.IndexByte(key, ':'); index >= 0 {
		key = key[:index]
	}
	switch key {
	case "", "provider_id", "heyametadata", "canonical_id":
		return ""
	case "mbid", "musicbrainz_artist", "musicbrainz_album", "musicbrainz_release_group", "musicbrainz_recording":
		return "musicbrainz"
	case "mal", "mal_id":
		return "myanimelist"
	case "ol_work_id", "openlibrary_work", "openlibrary_edition":
		return "openlibrary"
	case "isbn10", "isbn_10", "isbn13", "isbn_13":
		return "isbn"
	}
	return strings.TrimSuffix(key, "_id")
}

func (p *HeyaProvider) mapSummaries(summaries []Summary) []metadata.SearchResult {
	result := make([]metadata.SearchResult, 0, len(summaries))
	for _, summary := range summaries {
		label := firstNonEmpty(summary.Display.Name, summary.Display.Title, summary.Display.OriginalTitle, "Unknown")
		external := flattenExternalIDs(summary.ExternalIDs)
		altTitles := append([]string(nil), summary.Display.Aliases...)
		if original := strings.TrimSpace(summary.Display.OriginalTitle); original != "" && !strings.EqualFold(original, label) {
			altTitles = append(altTitles, original)
		}
		result = append(result, metadata.SearchResult{
			ProviderID: EncodeEntityProviderID(summary.ID), ProviderName: p.Name(),
			Title: label, Year: yearString(summary.Display.Year), PosterURL: p.client.ImageURL(summary.Display.ImageID),
			Confidence: 1, ExternalIDs: external, AltTitles: altTitles,
			HeyaSlug: summary.ID, Enriched: true, RawData: summary,
		})
	}
	return result
}

func mapDiscovery(resource *gen.DiscoveryResource, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	if resource == nil || resource.Result == nil {
		return nil, nil
	}
	if resource.Result.EntityId != nil {
		entityID := resource.Result.EntityId.String()
		return []metadata.SearchResult{{
			ProviderID: EncodeEntityProviderID(entityID), ProviderName: "heya",
			Title: firstNonEmpty(query.Title, "Unknown"), Year: query.Year,
			Confidence: 1, Recommendation: resource.Result.Recommendation,
			Evidence:    mapIdentifierEvidence(resource.Result.IdentifierEvidence),
			ExternalIDs: cloneStringMap(query.Identifiers), HeyaSlug: entityID,
			Enriched: true,
		}}, nil
	}
	if resource.Result.Candidates == nil {
		return nil, nil
	}
	results := make([]metadata.SearchResult, 0, len(*resource.Result.Candidates))
	for i, candidate := range *resource.Result.Candidates {
		providerID := EncodeCandidateProviderID(candidate.CandidateRef, resource.Result.Kind)
		label := displayLabel(candidate.Display)
		var evidence []metadata.SearchEvidence
		if candidate.Evidence != nil {
			evidence = make([]metadata.SearchEvidence, 0, len(*candidate.Evidence))
			for _, item := range *candidate.Evidence {
				evidence = append(evidence, metadata.SearchEvidence{
					Field: item.Field, Outcome: item.Outcome, Weight: item.Weight, Detail: stringValue(item.Detail),
				})
			}
		}
		recommendation := resource.Result.Recommendation
		requiresReview := i != 0 || !discoveryAutoMatchAllowed(recommendation, resource.Result.Kind, query, evidence)
		results = append(results, metadata.SearchResult{
			ProviderID: providerID, ProviderName: "heya", Title: label,
			Year: yearPtrString(candidate.Display.Year), Description: stringValue(candidate.Display.Disambiguation),
			Confidence: candidate.Confidence, Recommendation: recommendation,
			Evidence: evidence, RequiresReview: requiresReview,
			AltTitles: sliceValue(candidate.Display.Aliases), Enriched: false,
			RawData: candidate,
		})
	}
	return results, nil
}

func mapIdentifierEvidence(items *[]gen.IdentifierEvidence) []metadata.SearchEvidence {
	if items == nil {
		return nil
	}
	result := make([]metadata.SearchEvidence, 0, len(*items))
	for _, item := range *items {
		result = append(result, metadata.SearchEvidence{
			Field: "identifier:" + item.Scheme, Outcome: string(item.Outcome),
			Weight: 1, Detail: stringValue(item.Detail),
		})
	}
	return result
}

func discoveryAutoMatchAllowed(recommendation, kind string, query metadata.SearchQuery, evidence []metadata.SearchEvidence) bool {
	switch recommendation {
	case "strong_match":
		return true
	case "likely_match":
		// V2 explicitly requires multiple structured corroborating hints before
		// a likely match may be automatic. The free-text query is not a hint.
		// Audiobooks have no Audible identity spine yet, but an exact title plus
		// a complete author match is still independent canonical-work evidence.
		// Keep every weaker/partial audiobook result review-only.
		if strings.EqualFold(query.Format, "audiobook") {
			return discoveryEvidenceIsExact(evidence, "title") && discoveryEvidenceHasCompleteAuthors(evidence)
		}
		if discoveryHintCount(query) >= 2 {
			return true
		}
		// Movie filenames commonly provide only a title and year. Treat that
		// pair as corroborated only when HeyaMetadata explicitly reports both
		// fields as exact for its top candidate. The scanner still applies its
		// own confidence, year, and candidate-gap checks before accepting it.
		if (kind == "movie" || kind == "anime") && strings.TrimSpace(query.Year) != "" &&
			discoveryEvidenceIsExact(evidence, "title") && discoveryEvidenceIsExact(evidence, "year") {
			return true
		}
		// Anime sources frequently split each season into a separate work, so a
		// year is not always present locally. An exact title plus complete
		// coverage of the submitted absolute episode hints is still two
		// independent pieces of structured evidence.
		return kind == "anime" && len(query.Episodes) > 0 &&
			discoveryEvidenceIsExact(evidence, "title") && discoveryEvidenceHasCompleteEpisodes(evidence)
	default:
		return false
	}
}

func discoveryEvidenceIsExact(evidence []metadata.SearchEvidence, field string) bool {
	for _, item := range evidence {
		if strings.EqualFold(item.Field, field) &&
			(strings.EqualFold(item.Outcome, "exact") || strings.EqualFold(item.Outcome, "exact_alias")) {
			return true
		}
	}
	return false
}

func discoveryEvidenceHasCompleteAuthors(evidence []metadata.SearchEvidence) bool {
	for _, item := range evidence {
		if (strings.EqualFold(item.Field, "author") || strings.EqualFold(item.Field, "authors")) &&
			(strings.EqualFold(item.Outcome, "exact") || strings.EqualFold(item.Outcome, "exact_alias") || strings.EqualFold(item.Outcome, "1_of_1")) {
			return true
		}
	}
	return false
}

func discoveryEvidenceHasCompleteEpisodes(evidence []metadata.SearchEvidence) bool {
	for _, item := range evidence {
		if !strings.EqualFold(item.Field, "episodes") {
			continue
		}
		matchedText, totalText, ok := strings.Cut(strings.TrimSpace(item.Outcome), "_of_")
		if !ok {
			continue
		}
		matched, matchedErr := strconv.Atoi(matchedText)
		total, totalErr := strconv.Atoi(totalText)
		if matchedErr == nil && totalErr == nil && total > 0 && matched == total {
			return true
		}
	}
	return false
}

func discoveryHintCount(query metadata.SearchQuery) int {
	count := 0
	for _, value := range []string{query.Year, query.Artist, query.Author, query.ISBN, query.Format, query.Language, query.Country} {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	if len(query.Seasons) > 0 {
		count++
	}
	if len(query.Episodes) > 0 {
		count++
	}
	if len(query.Releases) > 0 {
		count++
	}
	return count
}

func displayLabel(display gen.Display) string {
	return firstNonEmpty(stringValue(display.Name), stringValue(display.Title), stringValue(display.OriginalTitle), "Unknown")
}

func (p *HeyaProvider) SearchAlbums(ctx context.Context, title, artist string) ([]metadata.SearchResult, error) {
	return p.Search(ctx, metadata.KindMusic, metadata.SearchQuery{CanonicalKind: "release_group", Title: title, Artist: artist})
}

// ResolveReleaseGroup materializes one release group that the local library
// actually contains. Artist detail intentionally includes unresolved catalog
// evidence, so scanners use this bounded operation instead of forcing an
// artist's complete discography to become canonical eagerly.
func (p *HeyaProvider) ResolveReleaseGroup(ctx context.Context, query metadata.SearchQuery) (*metadata.MediaDetail, error) {
	query.CanonicalKind = "release_group"
	results, err := p.Search(ctx, metadata.KindMusic, query)
	if err != nil {
		return nil, err
	}
	for _, result := range results {
		if result.ProviderID == "" || result.RequiresReview {
			continue
		}
		return p.GetDetail(ctx, result.ProviderID, &metadata.FetchOptions{
			Title:         query.Title,
			Year:          query.Year,
			CanonicalKind: "release_group",
		})
	}
	return nil, nil
}

type SearchHit struct {
	ID          string
	Kind        string
	Name        string
	Year        int
	Country     string
	Image       string
	Snippet     string
	Sources     []string
	ExternalIDs map[string]string
	AltTitles   []string
	Score       float64
	Enriched    bool
	Slug        string
}

func (p *HeyaProvider) SearchArtistBest(ctx context.Context, name string) (*SearchHit, error) {
	results, err := p.Search(ctx, metadata.KindMusic, metadata.SearchQuery{Title: name})
	if err != nil || len(results) == 0 {
		return nil, err
	}
	best := results[0]
	year, _ := strconv.Atoi(best.Year)
	return &SearchHit{ID: best.ProviderID, Kind: "artist", Name: best.Title, Year: year, Image: best.PosterURL,
		ExternalIDs: best.ExternalIDs, AltTitles: best.AltTitles, Score: best.Confidence, Enriched: best.Enriched, Slug: best.HeyaSlug}, nil
}

func (p *HeyaProvider) GetDetailFallback(ctx context.Context, providerIDs []string, opts *metadata.FetchOptions) (*metadata.MediaDetail, string, error) {
	var references []externalReference
	for _, providerID := range providerIDs {
		if reference, ok := decodeExternalProviderID(providerID); ok {
			references = append(references, reference)
			continue
		}
		detail, err := p.GetDetail(ctx, providerID, opts)
		if err == nil {
			return detail, providerID, nil
		}
		if !IsNotFound(err) {
			return nil, providerID, err
		}
	}
	if len(references) == 0 {
		return nil, "", errors.New("heyametadata: no canonical or external identifiers to try")
	}
	entityID, err := p.resolveReferences(ctx, references, opts)
	if err != nil {
		return nil, "", err
	}
	detail, err := p.getDetailByEntity(ctx, entityID, opts)
	return detail, EncodeEntityProviderID(entityID), err
}

func (p *HeyaProvider) GetDetail(ctx context.Context, providerID string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	selected, known, err := decodeProviderID(providerID)
	if err != nil {
		return nil, err
	}
	entityID := selected.EntityID
	if known && entityID == "" {
		candidateRef, parseErr := uuid.Parse(selected.CandidateRef)
		if parseErr != nil {
			return nil, fmt.Errorf("heyametadata: invalid candidate reference %q: %w", selected.CandidateRef, parseErr)
		}
		entityID, err = p.client.Resolve(ctx, candidateRef, selected.Kind, p.credentials, p.store)
		if err != nil {
			return nil, err
		}
	}
	if !known {
		reference, ok := decodeExternalProviderID(providerID)
		if !ok {
			return nil, fmt.Errorf("heyametadata: invalid provider ID %q", providerID)
		}
		entityID, err = p.resolveReferences(ctx, []externalReference{reference}, opts)
		if err != nil {
			return nil, err
		}
	}
	detail, err := p.getDetailByEntity(ctx, entityID, opts)
	return detail, deferRetryableDetailError(ctx, err)
}

func deferRetryableDetailError(ctx context.Context, err error) error {
	if err == nil || !IsRetryable(err) {
		return err
	}
	if deferred := transientDeferredWorkError(ctx, "retry canonical metadata detail after "+err.Error(), nil); deferred != nil {
		return deferred
	}
	return err
}

func (p *HeyaProvider) getDetailByEntity(ctx context.Context, entityID string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	if _, err := uuid.Parse(entityID); err != nil {
		return nil, fmt.Errorf("heyametadata: invalid canonical entity ID %q: %w", entityID, err)
	}
	language, country := "", ""
	if opts != nil {
		language, country = opts.Language, opts.Country
	}
	body, err := p.client.Entity(ctx, entityID, language, country, p.credentials)
	if err != nil {
		return nil, err
	}
	detail, err := p.mapDocument(ctx, body)
	if err != nil {
		return nil, err
	}

	// Entity detail determines the canonical kind; the remaining resources are
	// independent read projections. Fetch them concurrently so a large credit
	// list does not sit serially in front of image selection (or an issued
	// release) on every scan.
	sideCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type imagesResult struct {
		value *gen.EntityImagesOutputBody
		err   error
	}
	imagesCh := make(chan imagesResult, 1)
	go func() {
		value, fetchErr := p.client.Images(sideCtx, detail.CanonicalID, language, country, p.credentials)
		imagesCh <- imagesResult{value: value, err: fetchErr}
	}()

	type creditsResult struct {
		value []credit
		err   error
	}
	var creditsCh chan creditsResult
	if detail.CanonicalKind == "movie" || detail.CanonicalKind == "tv_show" || detail.CanonicalKind == "anime" {
		creditsCh = make(chan creditsResult, 1)
		go func() {
			value, fetchErr := p.client.Credits(sideCtx, detail.CanonicalID, p.credentials)
			creditsCh <- creditsResult{value: value, err: fetchErr}
		}()
	}

	type editionResult struct {
		value *releaseDocument
		err   error
	}
	var editionCh chan editionResult
	if detail.CanonicalKind == "release_group" && len(detail.Albums) > 0 {
		editionCh = make(chan editionResult, 1)
		go func() {
			value, fetchErr := p.firstIssuedRelease(sideCtx, detail.CanonicalID)
			editionCh <- editionResult{value: value, err: fetchErr}
		}()
	}

	if editionCh != nil {
		edition := <-editionCh
		if edition.err != nil {
			return nil, edition.err
		}
		if edition.value != nil {
			album := detail.Albums[0]
			mergeIssuedRelease(&album, *edition.value)
			detail.Albums[0] = album
			detail.Tracks = album.Tracks
			detail.TotalDiscs = musicAlbumDiscCount(album.Tracks)
			detail.Label = album.Label
			detail.Country = album.Country
			detail.Barcode = album.Barcode
			detail.CoverURL = firstNonEmpty(album.CoverURL, detail.CoverURL)
		}
	}
	if creditsCh != nil {
		credits := <-creditsCh
		if credits.err != nil {
			return nil, credits.err
		}
		detail.Cast, detail.Crew = p.mapCredits(credits.value)
	}
	images := <-imagesCh
	if images.err != nil {
		return nil, images.err
	}
	p.applyCanonicalImages(detail, images.value)
	return detail, nil
}

func musicAlbumDiscCount(tracks []metadata.TrackDetail) int {
	count := 0
	for _, track := range tracks {
		if track.DiscNumber > count {
			count = track.DiscNumber
		}
	}
	return count
}

func (p *HeyaProvider) applyCanonicalImages(detail *metadata.MediaDetail, response *gen.EntityImagesOutputBody) {
	if response == nil {
		return
	}
	artwork := make([]metadata.ArtworkResult, 0)
	if response.Results != nil {
		artwork = make([]metadata.ArtworkResult, 0, len(*response.Results))
		for _, image := range *response.Results {
			artwork = append(artwork, metadata.ArtworkResult{
				ImageID: image.Id, URL: p.client.ImageURL(image.Id), AssetType: image.Class,
				Language: stringValue(image.Language), Source: image.Provider,
				Score: float64PtrValue(image.ProviderScore), Width: int64PtrValue(image.Width), Height: int64PtrValue(image.Height),
			})
		}
	}
	detail.Artwork = artwork
	if detail.CanonicalKind == "artist" {
		detail.ArtistImages = artwork
	}
	if imageID := response.Selections["poster"]; imageID != "" {
		detail.PosterURL = p.client.ImageURL(imageID)
	} else if imageID := response.Selections["cover"]; imageID != "" {
		detail.PosterURL, detail.CoverURL = p.client.ImageURL(imageID), p.client.ImageURL(imageID)
	} else if imageID := response.Selections["profile"]; imageID != "" {
		detail.PosterURL = p.client.ImageURL(imageID)
	}
	if imageID := response.Selections["backdrop"]; imageID != "" {
		detail.BackdropURL = p.client.ImageURL(imageID)
	}
}

func float64PtrValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func int64PtrValue(value *int64) int {
	if value == nil {
		return 0
	}
	return int(*value)
}

func (p *HeyaProvider) resolveReferences(ctx context.Context, references []externalReference, opts *metadata.FetchOptions) (string, error) {
	query := metadata.SearchQuery{Identifiers: make(map[string]string, len(references))}
	if opts != nil {
		query.Title = opts.Title
		query.Year = opts.Year
		query.CanonicalKind = opts.CanonicalKind
	}
	for _, reference := range references {
		if query.CanonicalKind == "" {
			query.CanonicalKind = reference.Kind
		}
		if reference.Kind != query.CanonicalKind {
			return "", fmt.Errorf("heyametadata: mixed canonical kinds %q and %q in identifier evidence", query.CanonicalKind, reference.Kind)
		}
		query.Identifiers[reference.Provider] = reference.Value
	}
	if query.CanonicalKind == "" || len(query.Identifiers) == 0 {
		return "", errors.New("heyametadata: discovery requires a kind and at least one identifier")
	}

	discovery, err := p.client.Discover(ctx, discoveryRequest(query.CanonicalKind, query), p.credentials, p.store)
	if err != nil {
		return "", err
	}
	if discovery.Result == nil {
		return "", &APIError{Operation: "resolve identifier evidence", Status: http.StatusNotFound}
	}
	if discovery.Result.EntityId != nil {
		return discovery.Result.EntityId.String(), nil
	}
	if discovery.Result.Candidates == nil || len(*discovery.Result.Candidates) == 0 {
		return "", &APIError{Operation: "resolve identifier evidence", Status: http.StatusNotFound}
	}
	if !discoveryAutoMatchAllowed(discovery.Result.Recommendation, discovery.Result.Kind, query, nil) {
		return "", fmt.Errorf("heyametadata: identifier evidence requires review (recommendation %q)", discovery.Result.Recommendation)
	}
	candidate := (*discovery.Result.Candidates)[0]
	return p.client.Resolve(ctx, candidate.CandidateRef, discovery.Result.Kind, p.credentials, p.store)
}

func (p *HeyaProvider) FetchByKindID(ctx context.Context, kind, id string) (*metadata.MediaDetail, string, error) {
	if strings.HasPrefix(id, providerIDPrefix) || strings.HasPrefix(id, "heya:") {
		detail, err := p.GetDetail(ctx, id, nil)
		return detail, id, err
	}
	parts := strings.SplitN(id, ":", 2)
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("heyametadata: invalid %s identity %q", kind, id)
	}
	providerID := encodeExternalProviderID(canonicalKindFromLegacy(kind, parts[0]), parts[0], parts[1])
	detail, err := p.GetDetail(ctx, providerID, nil)
	return detail, providerID, err
}

func canonicalKindFromLegacy(kind, provider string) string {
	switch kind {
	case "tv":
		if provider == "anidb" || provider == "mal" {
			return "anime"
		}
		return "tv_show"
	case "album":
		return "release_group"
	case "book":
		return "book_work"
	default:
		return kind
	}
}

func (p *HeyaProvider) LookupByNFO(ctx context.Context, kind metadata.MediaKind, ids metadata.NFOIDs, opts *metadata.FetchOptions) (*metadata.MediaDetail, string, error) {
	external := map[string]string{"tmdb": ids.TMDBID, "tvdb": ids.TVDBID, "imdb": ids.IMDBID, "mbid": ids.MBID, "anidb": ids.AniDBID, "mal": ids.MALID}
	return p.GetDetailFallback(ctx, BuildLookupIDs(kind, external, ""), opts)
}

func (p *HeyaProvider) FetchArtwork(ctx context.Context, kind metadata.MediaKind, externalIDs map[string]string, opts *metadata.FetchOptions) ([]metadata.ArtworkResult, error) {
	detail, _, err := p.GetDetailFallback(ctx, BuildLookupIDs(kind, externalIDs, ""), opts)
	if err != nil {
		return nil, err
	}
	return detail.Artwork, nil
}

func (p *HeyaProvider) FetchRatings(ctx context.Context, kind metadata.MediaKind, externalIDs map[string]string) (*metadata.RatingsData, error) {
	detail, _, err := p.GetDetailFallback(ctx, BuildLookupIDs(kind, externalIDs, ""), nil)
	if err != nil {
		return nil, err
	}
	ratings, err := p.client.Ratings(ctx, detail.CanonicalID, p.credentials)
	if err != nil {
		return nil, err
	}
	return mapRatings(ratings), nil
}

type SimilarHit struct {
	Kind, Name, Artist, MBID, Source, Image, URL string
	Score                                        float64
}

func (p *HeyaProvider) SimilarArtists(ctx context.Context, mbid, name string) ([]SimilarHit, error) {
	detail, _, err := p.FetchByKindID(ctx, "artist", "musicbrainz:"+mbid)
	if err != nil && name != "" {
		hit, searchErr := p.SearchArtistBest(ctx, name)
		if searchErr != nil || hit == nil {
			return nil, err
		}
		detail, err = p.GetDetail(ctx, hit.ID, nil)
	}
	if err != nil {
		return nil, err
	}
	result := make([]SimilarHit, 0, len(detail.ArtistSimilarArtists))
	for _, item := range detail.ArtistSimilarArtists {
		result = append(result, SimilarHit{Kind: "artist", Name: item.Name, MBID: item.MBID, Score: item.Match, Source: firstNonEmpty(item.Provider, "heyametadata"), URL: item.URL})
	}
	return result, nil
}

// RecordingCredits exposes the canonical recording's performance credits
// for the enrich pipeline (per-track fetch during album refresh).
func (p *HeyaProvider) RecordingCredits(ctx context.Context, entityID string) ([]metadata.RecordingCredit, error) {
	return p.client.RecordingCredits(ctx, entityID, p.credentials)
}

// RecordingMetadata exposes the canonical recording's focused musical
// metadata for local catalog hydration and text-embedding generation.
func (p *HeyaProvider) RecordingMetadata(ctx context.Context, entityID string) (metadata.RecordingMetadata, error) {
	return p.client.RecordingMetadata(ctx, entityID, p.credentials)
}

func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

func flattenExternalIDs(ids []ExternalID) map[string]string {
	result := make(map[string]string, len(ids))
	for _, id := range ids {
		key := id.Provider
		if id.Provider == "musicbrainz" {
			key = "mbid"
			switch id.Namespace {
			case "artist":
				result["musicbrainz_artist"] = id.Value
			case "release_group":
				result["musicbrainz_release_group"] = id.Value
			case "release":
				result["musicbrainz_album"] = id.Value
			case "recording":
				result["musicbrainz_recording"] = id.Value
			}
		}
		if id.Provider == "openlibrary" && id.Namespace == "work" {
			key = "ol_work_id"
		}
		result[key] = id.Value
		result[id.Provider+":"+id.Namespace] = id.Value
	}
	return result
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	result := values[:0]
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func sliceValue(value *[]string) []string {
	if value == nil {
		return nil
	}
	return *value
}
func yearString(value int) string {
	if value <= 0 {
		return ""
	}
	return strconv.Itoa(value)
}
func yearPtrString(value *int64) string {
	if value == nil || *value <= 0 {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}
