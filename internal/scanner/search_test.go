package scanner

import (
	"context"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/metadata"
)

type fakeMovieSearchProvider struct {
	results map[string][]metadata.SearchResult
	queries []metadata.SearchQuery
}

func (f *fakeMovieSearchProvider) Search(_ context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	if kind != metadata.KindMovie {
		return nil, nil
	}
	f.queries = append(f.queries, query)
	return f.results[query.Title+"\x00"+query.Year], nil
}

type fakeTVSearchProvider struct {
	results map[string][]metadata.SearchResult
	queries []metadata.SearchQuery
}

type deferredSearchProvider struct{}

func (deferredSearchProvider) Search(context.Context, metadata.MediaKind, metadata.SearchQuery) ([]metadata.SearchResult, error) {
	return nil, &metadata.DeferredWorkError{Operation: "test discovery", RetryAfter: 30 * time.Second}
}

func TestTVSearchPropagatesDeferredMetadataWork(t *testing.T) {
	results, err := SearchTVMatches(context.Background(), []TVMatch{{Key: "title:doctor who", Title: "Doctor Who", Year: "1963"}}, deferredSearchProvider{}, &captureEmitter{}, 0)
	if len(results) != 0 {
		t.Fatalf("deferred search persisted results: %#v", results)
	}
	if retryAfter, ok := metadata.DeferredWorkRetryAfter(err); !ok || retryAfter != 30*time.Second {
		t.Fatalf("deferred search error = %v, retry_after = %s, ok = %v", err, retryAfter, ok)
	}
}

func TestLibraryRunReportsDeferredMetadataAsWaiting(t *testing.T) {
	recorder := &EventRecorder{}
	run := &LibraryRun{sink: NewEventSink(Event{}, recorder)}
	err := &metadata.DeferredWorkError{Operation: "test discovery", RetryAfter: 30 * time.Second}

	if got := run.fail(err); got != err {
		t.Fatalf("fail returned %v, want original deferred error", got)
	}
	if len(recorder.Events) != 1 {
		t.Fatalf("events = %#v, want one", recorder.Events)
	}
	event := recorder.Events[0]
	if event.Event != "scan.deferred" || event.Severity != SeverityInfo {
		t.Fatalf("event = %#v, want informational scan.deferred", event)
	}
	if event.Data["retry_after_ms"] != int64(30_000) {
		t.Fatalf("retry_after_ms = %#v, want 30000", event.Data["retry_after_ms"])
	}
}

func TestDurableIdentityKeysPreserveLeadingArticles(t *testing.T) {
	tvKey, tvKeyType := tvMatchKey(TVPlan{Title: "The Office (US)", Year: "2005"})
	if tvKey != "title_year:the office us|2005" || tvKeyType != "title_year" {
		t.Fatalf("TV identity key = %q (%s)", tvKey, tvKeyType)
	}
	movieKey, movieKeyType := movieMatchKey(MoviePlan{Title: "A Goofy Movie", Year: "1995"})
	if movieKey != "title_year:a goofy movie|1995" || movieKeyType != "title_year" {
		t.Fatalf("movie identity key = %q (%s)", movieKey, movieKeyType)
	}
	if got := normalizeSearchTitle("The Office"); got != "office" {
		t.Fatalf("fuzzy search normalization = %q, want article-insensitive office", got)
	}
}

func (f *fakeTVSearchProvider) Search(_ context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	if kind != metadata.KindTV {
		return nil, nil
	}
	f.queries = append(f.queries, query)
	return f.results[query.Title+"\x00"+query.Year], nil
}

func TestSearchMovieMatchesSelectsCandidatesWithoutFetchingMetadata(t *testing.T) {
	matches := []MovieMatch{
		{
			Key:         "tmdb:438631",
			KeyType:     "tmdb",
			Title:       "Dune",
			Year:        "2021",
			ExternalIDs: map[string]string{"tmdb": "438631"},
		},
		{
			Key:     "title_year:the naked gun|2025",
			KeyType: "title_year",
			Title:   "The Naked Gun",
			Year:    "2025",
		},
		{
			Key:     "title_year:very wrong|2024",
			KeyType: "title_year",
			Title:   "Very Wrong",
			Year:    "2024",
		},
		{
			Key:         "imdb:tt0103644",
			KeyType:     "imdb",
			Title:       "Alien",
			Year:        "1992",
			Aliases:     []string{"Alien³"},
			ExternalIDs: map[string]string{"imdb": "tt0103644"},
		},
		{
			Key:     "title_year:jackass presents bad grandpa 5|2014",
			KeyType: "title_year",
			Title:   "Jackass Presents Bad Grandpa .5",
			Year:    "2014",
		},
	}
	provider := &fakeMovieSearchProvider{results: map[string][]metadata.SearchResult{
		"Dune\x002021": {
			{ProviderID: "heya:movie:tmdb:438631", ProviderName: "heya", Title: "Dune", Year: "2021", ExternalIDs: map[string]string{"tmdb": "438631"}},
			{ProviderID: "heya:movie:tmdb:841", ProviderName: "heya", Title: "Dune", Year: "1984", ExternalIDs: map[string]string{"tmdb": "841"}},
		},
		"The Naked Gun\x002025": {
			{ProviderID: "heya:movie:tmdb:1035259", ProviderName: "heya", Title: "The Naked Gun", Year: "2025", ExternalIDs: map[string]string{"tmdb": "1035259"}},
		},
		"Very Wrong\x002024": {
			{ProviderID: "heya:movie:tmdb:1", ProviderName: "heya", Title: "Completely Different", Year: "1980", ExternalIDs: map[string]string{"tmdb": "1"}},
		},
		"Alien\x001992": {
			{ProviderID: "heya:movie:tmdb:8077", ProviderName: "heya", Title: "Alien³", Year: "1992", ExternalIDs: map[string]string{"tmdb": "8077"}},
			{ProviderID: "heya:movie:tmdb:679", ProviderName: "heya", Title: "Aliens", Year: "1986", ExternalIDs: map[string]string{"tmdb": "679"}},
		},
		"Jackass Presents Bad Grandpa .5\x002014": {
			{ProviderID: "heya:movie:tmdb:273641", ProviderName: "heya", Title: "Jackass Presents: Bad Grandpa .5", Year: "2014", ExternalIDs: map[string]string{"tmdb": "273641"}},
			{ProviderID: "heya:movie:tmdb:208134", ProviderName: "heya", Title: "Jackass Presents: Bad Grandpa", Year: "2013", ExternalIDs: map[string]string{"tmdb": "208134"}},
		},
	}}

	emit := &captureEmitter{}
	results, err := SearchMovieMatches(context.Background(), matches, provider, emit, 0)
	if err != nil {
		t.Fatalf("search movie matches: %v", err)
	}

	if len(results) != 5 {
		t.Fatalf("results: got %d, want 5", len(results))
	}
	if len(provider.queries) != 5 {
		t.Fatalf("queries: got %d, want 5", len(provider.queries))
	}

	byKey := map[string]MovieSearchMatch{}
	for _, result := range results {
		byKey[result.Key] = result
	}
	assertSelectedSearch(t, byKey["tmdb:438631"], "heya:movie:tmdb:438631", 1)
	assertSelectedSearch(t, byKey["title_year:the naked gun|2025"], "heya:movie:tmdb:1035259", 0.95)
	assertSelectedSearch(t, byKey["imdb:tt0103644"], "heya:movie:tmdb:8077", 0.95)
	assertSelectedSearch(t, byKey["title_year:jackass presents bad grandpa 5|2014"], "heya:movie:tmdb:273641", 0.95)

	rejected := byKey["title_year:very wrong|2024"]
	if rejected.Accepted {
		t.Fatalf("wrong candidate should be rejected: %#v", rejected)
	}
	if rejected.Reason != "ambiguous_or_low_confidence" {
		t.Fatalf("rejected reason: got %q", rejected.Reason)
	}
	if !eventSeen(emit.events, "match.selected") {
		t.Fatalf("expected match.selected event")
	}
	if !eventSeen(emit.events, "match.rejected") {
		t.Fatalf("expected match.rejected event")
	}
}

func TestSearchSelectionSuspicionNormalizesHarmlessFormatting(t *testing.T) {
	nakedGun := MovieSearchMatch{
		Accepted:   true,
		Query:      MovieSearchQuery{Title: "Naked Gun 33 1/3: The Final Insult", Year: "1994"},
		Title:      "Naked Gun 33⅓: The Final Insult",
		Year:       "1994",
		Confidence: 0.95,
	}
	if searchSelectionLooksSuspicious(nakedGun) {
		t.Fatalf("unicode fraction formatting should not be suspicious")
	}

	badGrandpa := MovieSearchMatch{
		Accepted:   true,
		Query:      MovieSearchQuery{Title: "Jackass Presents Bad Grandpa", Year: "2014"},
		Title:      "Jackass Presents: Bad Grandpa",
		Year:       "2013",
		Confidence: 0.90,
	}
	if !searchSelectionLooksSuspicious(badGrandpa) {
		t.Fatalf("year mismatch and lower confidence should remain suspicious")
	}

	badGrandpaPointFive := MovieSearchMatch{
		Accepted:   true,
		Query:      MovieSearchQuery{Title: "Jackass Presents Bad Grandpa .5", Year: "2014"},
		Title:      "Jackass Presents: Bad Grandpa .5",
		Year:       "2014",
		Confidence: 1,
	}
	if searchSelectionLooksSuspicious(badGrandpaPointFive) {
		t.Fatalf(".5 title punctuation should not be suspicious")
	}
}

func TestSearchMovieMatchesUsesStoredDecisions(t *testing.T) {
	matches := []MovieMatch{
		{
			Key:     "title_year:poker face|2023",
			KeyType: "title_year",
			Title:   "Poker Face",
			Year:    "2023",
		},
		{
			Key:     "title_year:wrong|2024",
			KeyType: "title_year",
			Title:   "Wrong",
			Year:    "2024",
		},
	}
	provider := &fakeMovieSearchProvider{results: map[string][]metadata.SearchResult{}}
	decisions := SearchDecisions{
		"title_year:poker face|2023": {
			Key:         "title_year:poker face|2023",
			Status:      "accepted",
			ProviderID:  "heya:movie:tmdb:999",
			Provider:    "heya",
			Title:       "Poker Face",
			Year:        "2023",
			Confidence:  0.5,
			ExternalIDs: map[string]string{"tmdb": "999"},
		},
		"title_year:wrong|2024": {
			Key:    "title_year:wrong|2024",
			Status: "rejected",
		},
	}

	emit := &captureEmitter{}
	results, err := SearchMovieMatches(context.Background(), matches, provider, emit, 0, decisions)
	if err != nil {
		t.Fatalf("search movie matches: %v", err)
	}
	if len(provider.queries) != 0 {
		t.Fatalf("manual decisions should bypass provider queries, got %d", len(provider.queries))
	}

	byKey := map[string]MovieSearchMatch{}
	for _, result := range results {
		byKey[result.Key] = result
	}
	approved := byKey["title_year:poker face|2023"]
	assertSelectedSearch(t, approved, "heya:movie:tmdb:999", 0.5)
	if approved.ManualDecision != "accepted" {
		t.Fatalf("approved manual decision: got %q", approved.ManualDecision)
	}
	if got := approved.ExternalIDs["tmdb"]; got != "999" {
		t.Fatalf("approved external id: got %q", got)
	}

	rejected := byKey["title_year:wrong|2024"]
	if rejected.Accepted {
		t.Fatalf("rejected manual decision should not be accepted: %#v", rejected)
	}
	if rejected.ManualDecision != "rejected" || rejected.Reason != "manual_rejected" {
		t.Fatalf("rejected manual decision: got decision=%q reason=%q", rejected.ManualDecision, rejected.Reason)
	}
}

func TestSearchTVMatchesSelectsCandidates(t *testing.T) {
	matches := []TVMatch{
		{
			Key:         "tmdb:1396",
			KeyType:     "tmdb",
			Title:       "Breaking Bad",
			Year:        "2008",
			ExternalIDs: map[string]string{"tmdb": "1396"},
			Episodes:    []TVEpisodeRef{{Season: 1, Episode: 1}},
		},
		{
			Key:     "title:the bear",
			KeyType: "title",
			Title:   "The Bear",
		},
		{
			Key:     "title_year:the office us|2005",
			KeyType: "title_year",
			Title:   "The Office (US)",
			Year:    "2005",
			Aliases: []string{"The Office"},
		},
		{
			Key:     "title_year:wrong|2024",
			KeyType: "title_year",
			Title:   "Wrong",
			Year:    "2024",
		},
	}
	provider := &fakeTVSearchProvider{results: map[string][]metadata.SearchResult{
		"Breaking Bad\x002008": {
			{ProviderID: "heyametadata:v2:resolution:opaque-breaking-bad", ProviderName: "heya", Title: "Breaking Bad", Year: "2008"},
		},
		"The Bear\x00": {
			{ProviderID: "heya:tv:tmdb:136315", ProviderName: "heya", Title: "The Bear", Year: "2022", ExternalIDs: map[string]string{"tmdb": "136315"}},
		},
		"The Office (US)\x002005": {
			{ProviderID: "heya:tv:tmdb:2316", ProviderName: "heya", Title: "The Office", Year: "2005", ExternalIDs: map[string]string{"tmdb": "2316"}},
		},
		"Wrong\x002024": {
			{ProviderID: "heya:tv:tmdb:1", ProviderName: "heya", Title: "Completely Different", Year: "1980", ExternalIDs: map[string]string{"tmdb": "1"}},
		},
	}}

	emit := &captureEmitter{}
	results, err := SearchTVMatches(context.Background(), matches, provider, emit, 0)
	if err != nil {
		t.Fatalf("search TV matches: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("results: got %d, want 4", len(results))
	}
	if len(provider.queries) != 4 {
		t.Fatalf("queries: got %d, want 4", len(provider.queries))
	}
	if len(provider.queries[0].Episodes) != 1 || provider.queries[0].Episodes[0].Season != 1 || provider.queries[0].Episodes[0].Number != 1 {
		t.Fatalf("Breaking Bad discovery evidence: %#v", provider.queries[0])
	}

	byKey := map[string]TVSearchMatch{}
	for _, result := range results {
		byKey[result.Key] = result
	}
	assertSelectedTVSearch(t, byKey["tmdb:1396"], "heyametadata:v2:resolution:opaque-breaking-bad", 0.95)
	assertSelectedTVSearch(t, byKey["title:the bear"], "heya:tv:tmdb:136315", 0.85)
	assertSelectedTVSearch(t, byKey["title_year:the office us|2005"], "heya:tv:tmdb:2316", 0.95)

	rejected := byKey["title_year:wrong|2024"]
	if rejected.Accepted {
		t.Fatalf("wrong candidate should be rejected: %#v", rejected)
	}
	if rejected.Reason != "ambiguous_or_low_confidence" {
		t.Fatalf("rejected reason: got %q", rejected.Reason)
	}
}

func TestSearchAnimeMatchesPrefersExactPrimaryTitleOverAltTitleTie(t *testing.T) {
	matches := []TVMatch{
		{
			Key:         "title:eureka seven ao",
			KeyType:     "title",
			Title:       "Eureka Seven AO",
			ExternalIDs: map[string]string{"anidb": "8854"},
		},
	}
	provider := &fakeTVSearchProvider{results: map[string][]metadata.SearchResult{
		"Eureka Seven AO\x00": {
			{
				ProviderID:   "heya:tv:tmdb:889",
				ProviderName: "heya",
				Title:        "Eureka Seven",
				Year:         "2005",
				ExternalIDs:  map[string]string{"tmdb": "889"},
				AltTitles:    []string{"Eureka Seven AO"},
			},
			{
				ProviderID:   "heya:tv:tmdb:321121",
				ProviderName: "heya",
				Title:        "Eureka Seven AO",
				Year:         "2012",
				ExternalIDs:  map[string]string{"tmdb": "321121"},
			},
		},
	}}

	emit := &captureEmitter{}
	results, err := SearchAnimeMatches(context.Background(), matches, provider, emit, 0)
	if err != nil {
		t.Fatalf("search anime matches: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results: got %d, want 1", len(results))
	}
	assertSelectedTVSearch(t, results[0], "heya:tv:tmdb:321121", 0.85)
}

func TestSearchAnimeMatchesTrustsExactHeyaAliasEvidenceAcrossCloseSeasonCandidates(t *testing.T) {
	matches := []TVMatch{{
		Key: "tvdb:371898", KeyType: "tvdb", Title: "86 Eighty Six", Year: "2021",
		ExternalIDs: map[string]string{"tvdb": "371898"},
		Episodes:    []TVEpisodeRef{{Season: 1, Episode: 1}},
	}}
	provider := &fakeTVSearchProvider{results: map[string][]metadata.SearchResult{
		"86 Eighty Six\x002021": {
			{
				ProviderID: "heyametadata:v2:candidate:anime:top", ProviderName: "heya",
				Title: "86", Year: "2021", AltTitles: []string{"86 Eighty Six"},
				Recommendation: "strong_match", Evidence: []metadata.SearchEvidence{
					{Field: "title", Outcome: "exact_alias"},
					{Field: "year", Outcome: "exact"},
				},
			},
			{
				ProviderID: "heyametadata:v2:candidate:anime:season", ProviderName: "heya",
				Title: "86 (2021)", Year: "2021", AltTitles: []string{"86 Eighty Six"},
				Recommendation: "strong_match", RequiresReview: true,
			},
		},
	}}

	results, err := SearchAnimeMatches(context.Background(), matches, provider, &captureEmitter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results: got %d, want 1", len(results))
	}
	assertSelectedTVSearch(t, results[0], "heyametadata:v2:candidate:anime:top", 0.95)
	if tvSearchSelectionLooksSuspicious(results[0]) {
		t.Fatalf("Heya exact-alias selection remained suspicious: %#v", results[0])
	}
	if heyaEvidenceClearsTVAmbiguity("2021", "2021", []metadata.SearchEvidence{{Field: "title", Outcome: "fuzzy"}, {Field: "year", Outcome: "exact"}}) {
		t.Fatal("fuzzy title evidence cleared anime ambiguity")
	}
}

func TestSearchAnimeMatchesSendsIdentifiersThroughUnifiedDiscovery(t *testing.T) {
	matches := []TVMatch{
		{
			Key:         "anidb:8854",
			KeyType:     "anidb",
			Title:       "Eureka Seven AO",
			ExternalIDs: map[string]string{"anidb": "8854"},
		},
	}
	provider := &fakeTVSearchProvider{results: map[string][]metadata.SearchResult{
		"Eureka Seven AO\x00": {{
			ProviderID: "heyametadata:v2:entity:30000000-0000-4000-8000-000000000001", ProviderName: "heya",
			Title: "Eureka Seven AO", Confidence: 1, ExternalIDs: map[string]string{"anidb": "8854"},
		}},
	}}

	emit := &captureEmitter{}
	results, err := SearchAnimeMatches(context.Background(), matches, provider, emit, 0)
	if err != nil {
		t.Fatalf("search anime matches: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results: got %d, want 1", len(results))
	}
	if len(provider.queries) != 1 || provider.queries[0].Identifiers["anidb"] != "8854" || provider.queries[0].CanonicalKind != "anime" {
		t.Fatalf("unified discovery query = %#v", provider.queries)
	}
	assertSelectedTVSearch(t, results[0], "heyametadata:v2:entity:30000000-0000-4000-8000-000000000001", 1)
	if got := results[0].ExternalIDs["anidb"]; got != "8854" {
		t.Fatalf("external anidb id: got %q, want 8854", got)
	}
}

func TestTVDiscoveryEpisodeHintsUseAbsoluteNumbersForAnime(t *testing.T) {
	refs := []TVEpisodeRef{
		{Season: 1, Episode: 2, Absolute: 14},
		{Season: 1, Episode: 2, Absolute: 14},
		{Season: 1, Episode: 3},
	}
	anime := tvDiscoveryEpisodeHints(refs, "anime")
	if len(anime) != 2 || anime[0].Season != 0 || anime[0].Number != 14 || anime[1].Season != 1 || anime[1].Number != 3 {
		t.Fatalf("anime hints = %#v", anime)
	}
	tv := tvDiscoveryEpisodeHints(refs, "tv")
	if len(tv) != 2 || tv[0].Season != 1 || tv[0].Number != 2 || tv[1].Number != 3 {
		t.Fatalf("TV hints = %#v", tv)
	}
}

func TestSearchTVMatchesUsesStoredDecisions(t *testing.T) {
	matches := []TVMatch{
		{
			Key:     "title:poker face",
			KeyType: "title",
			Title:   "Poker Face",
		},
		{
			Key:     "title_year:show with extras|2020",
			KeyType: "title_year",
			Title:   "Show With Extras",
			Year:    "2020",
		},
	}
	provider := &fakeTVSearchProvider{results: map[string][]metadata.SearchResult{}}
	decisions := SearchDecisions{
		"title:poker face": {
			Key:         "title:poker face",
			Status:      "accepted",
			ProviderID:  "heya:tv:tmdb:120998",
			Provider:    "heya",
			Title:       "Poker Face",
			Year:        "2023",
			Confidence:  0.85,
			ExternalIDs: map[string]string{"tmdb": "120998"},
		},
		"title_year:show with extras|2020": {
			Key:    "title_year:show with extras|2020",
			Status: "ignored",
		},
	}

	emit := &captureEmitter{}
	results, err := SearchTVMatches(context.Background(), matches, provider, emit, 0, decisions)
	if err != nil {
		t.Fatalf("search TV matches: %v", err)
	}
	if len(provider.queries) != 0 {
		t.Fatalf("manual decisions should bypass provider queries, got %d", len(provider.queries))
	}

	byKey := map[string]TVSearchMatch{}
	for _, result := range results {
		byKey[result.Key] = result
	}
	approved := byKey["title:poker face"]
	assertSelectedTVSearch(t, approved, "heya:tv:tmdb:120998", 0.85)
	if approved.ManualDecision != "accepted" {
		t.Fatalf("approved manual decision: got %q", approved.ManualDecision)
	}

	ignored := byKey["title_year:show with extras|2020"]
	if ignored.Accepted {
		t.Fatalf("ignored manual decision should not be accepted: %#v", ignored)
	}
	if ignored.ManualDecision != "ignored" || ignored.Reason != "manual_ignored" {
		t.Fatalf("ignored manual decision: got decision=%q reason=%q", ignored.ManualDecision, ignored.Reason)
	}
}

func TestManualSearchDecisionsSuppressReviewPersistence(t *testing.T) {
	result := Result{
		TVMatches: []TVMatch{
			{
				Key:     "title:poker face",
				KeyType: "title",
				Title:   "Poker Face",
			},
			{
				Key:     "title_year:show with extras|2020",
				KeyType: "title_year",
				Title:   "Show With Extras",
				Year:    "2020",
			},
		},
		TVSearch: []TVSearchMatch{
			{
				Key:            "title:poker face",
				Query:          TVSearchQuery{Title: "Poker Face"},
				Accepted:       true,
				ProviderID:     "heya:tv:tmdb:120998",
				Title:          "Poker Face",
				Year:           "2023",
				Confidence:     0.85,
				ManualDecision: "accepted",
			},
			{
				Key:            "title_year:show with extras|2020",
				Query:          TVSearchQuery{Title: "Show With Extras", Year: "2020"},
				Reason:         "manual_rejected",
				ManualDecision: "rejected",
			},
		},
	}

	statuses := scanIdentityReviewStatuses(result)
	if got := statuses["title:poker face"]; got != "" {
		t.Fatalf("manual accepted title-only identity should not remain needs_review, got %q", got)
	}
	if got := statuses["title_year:show with extras|2020"]; got != "rejected" {
		t.Fatalf("manual rejected status: got %q", got)
	}

	findings := scanFindingDrafts(result, nil)
	for _, finding := range findings {
		if finding.Code == "title_only_identity" || finding.Code == "search_rejected" || finding.Code == "search_suspicious" {
			t.Fatalf("manual decision should suppress review finding: %#v", finding)
		}
	}
}

func assertSelectedSearch(t *testing.T, result MovieSearchMatch, providerID string, confidence float64) {
	t.Helper()
	if !result.Accepted {
		t.Fatalf("%s should be accepted: %#v", result.Key, result)
	}
	if result.ProviderID != providerID {
		t.Fatalf("%s provider id: got %q, want %q", result.Key, result.ProviderID, providerID)
	}
	if result.Confidence != confidence {
		t.Fatalf("%s confidence: got %.2f, want %.2f", result.Key, result.Confidence, confidence)
	}
}

func assertSelectedTVSearch(t *testing.T, result TVSearchMatch, providerID string, confidence float64) {
	t.Helper()
	if !result.Accepted {
		t.Fatalf("%s should be accepted: %#v", result.Key, result)
	}
	if result.ProviderID != providerID {
		t.Fatalf("%s provider id: got %q, want %q", result.Key, result.ProviderID, providerID)
	}
	if result.Confidence != confidence {
		t.Fatalf("%s confidence: got %.2f, want %.2f", result.Key, result.Confidence, confidence)
	}
}
