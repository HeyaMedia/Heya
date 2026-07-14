package heyametadata

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/karbowiak/heya/clients/heyametadata"
	"github.com/karbowiak/heya/internal/metadata"
)

const (
	testMovieID   = "11111111-1111-4111-8111-111111111111"
	testPersonID  = "22222222-2222-4222-8222-222222222222"
	testImageID   = "33333333-3333-4333-8333-333333333333"
	testSeasonID  = "44444444-4444-4444-8444-444444444444"
	testEpisodeID = "55555555-5555-4555-8555-555555555555"
	testAuthorID  = "66666666-6666-4666-8666-666666666666"
	testSeriesID  = "77777777-7777-4777-8777-777777777777"
	testRecordID  = "88888888-8888-4888-8888-888888888888"
)

func TestMapDiscoveryPreservesRecommendationEvidenceAndReviewGate(t *testing.T) {
	evidence := []gen.Evidence{{Field: "title", Outcome: "exact", Weight: .5}, {Field: "year", Outcome: "exact", Weight: .2}}
	emptyEvidence := []gen.Evidence{}
	candidates := []gen.Candidate{
		{CandidateRef: uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"), Rank: 1, Confidence: .91, Match: "likely", Display: gen.Display{Title: strPointer("Dune")}, Evidence: &evidence},
		{CandidateRef: uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"), Rank: 2, Confidence: .80, Match: "possible", Display: gen.Display{Title: strPointer("Dune Two")}, Evidence: &emptyEvidence},
	}
	resource := &gen.DiscoveryResource{Result: &gen.Result{Kind: "movie", Recommendation: "ambiguous", Candidates: &candidates}}

	results, err := mapDiscovery(resource, metadata.SearchQuery{Title: "Dune", Year: "2021", Country: "US"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || !results[0].RequiresReview || !results[1].RequiresReview {
		t.Fatalf("ambiguous candidates must require review: %#v", results)
	}
	if results[0].Recommendation != "ambiguous" || len(results[0].Evidence) != 2 {
		t.Fatalf("discovery decision was not preserved: %#v", results[0])
	}
	if results[0].ExternalIDs != nil || strings.Contains(results[0].ProviderID, "tmdb") || strings.Contains(results[0].ProviderID, "musicbrainz") {
		t.Fatalf("opaque candidate leaked provider identity: %#v", results[0])
	}

	resource.Result.Recommendation = "likely_match"
	results, err = mapDiscovery(resource, metadata.SearchQuery{Title: "Dune", Year: "2021"})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].RequiresReview {
		t.Fatal("movie likely match with exact title and year evidence remained review-only")
	}
	missingYearEvidence := []gen.Evidence{{Field: "title", Outcome: "exact", Weight: .5}}
	candidates[0].Evidence = &missingYearEvidence
	resource.Result.Candidates = &candidates
	results, err = mapDiscovery(resource, metadata.SearchQuery{Title: "Dune", Year: "2021"})
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].RequiresReview {
		t.Fatal("movie likely match without exact year evidence was auto-selectable")
	}
	candidates[0].Evidence = &evidence
	resource.Result.Candidates = &candidates
	results, err = mapDiscovery(resource, metadata.SearchQuery{
		Title: "Dune", Year: "2021", Episodes: []metadata.EpisodeHint{{Season: 1, Number: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].RequiresReview {
		t.Fatal("likely match corroborated by year and episode evidence remained review-only")
	}
	results, err = mapDiscovery(resource, metadata.SearchQuery{Title: "Dune", Year: "2021", Country: "US"})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].RequiresReview || !results[1].RequiresReview {
		t.Fatalf("only rank one corroborated likely match may auto-select: %#v", results)
	}
	results, err = mapDiscovery(resource, metadata.SearchQuery{Title: "Dune", Author: "Frank Herbert", Format: "audiobook"})
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].RequiresReview {
		t.Fatal("audiobook-specific likely match must remain manual")
	}
}

func TestUnresolvedDiscographyAlbumRetainsOnlyMatchingEvidence(t *testing.T) {
	album, ok := unresolvedDiscographyAlbum(map[string]any{
		"title":              "Da Funk",
		"first_release_date": "1995-12-07",
		"primary_type":       "Single",
		"sources": []any{map[string]any{
			"provider": "musicbrainz", "value": "opaque-provider-value",
		}},
	})
	if !ok {
		t.Fatal("unresolved relation was discarded")
	}
	if album.Title != "Da Funk" || album.Year != 1995 || album.Type != "single" || album.ReleaseDate != "1995-12-07" {
		t.Fatalf("unresolved album = %#v", album)
	}
	if album.CanonicalID != "" || len(album.ExternalIDs) != 0 {
		t.Fatalf("unresolved provider evidence leaked as canonical identity: %#v", album)
	}
}

func TestMapDiscoveryAllowsExactAnimeTitleWithCompleteEpisodeEvidence(t *testing.T) {
	evidence := []gen.Evidence{
		{Field: "title", Outcome: "exact", Weight: .4},
		{Field: "episodes", Outcome: "1_of_1", Weight: .16},
	}
	candidates := []gen.Candidate{{
		CandidateRef: uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		Rank:         1,
		Confidence:   .85,
		Match:        "likely",
		Display:      gen.Display{Title: strPointer("Bocchi the Rock!")},
		Evidence:     &evidence,
	}}
	resource := &gen.DiscoveryResource{Result: &gen.Result{
		Kind: "anime", Recommendation: "likely_match", Candidates: &candidates,
	}}
	query := metadata.SearchQuery{
		CanonicalKind: "anime", Title: "Bocchi the Rock!",
		Episodes: []metadata.EpisodeHint{{Number: 1}},
	}

	results, err := mapDiscovery(resource, query)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].RequiresReview {
		t.Fatalf("exact anime title with complete episode evidence remained review-only: %#v", results)
	}

	evidence[1].Outcome = "0_of_1"
	results, err = mapDiscovery(resource, query)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].RequiresReview {
		t.Fatalf("anime candidate without episode corroboration was auto-selectable: %#v", results)
	}
}

func TestMergeSameCanonicalIDPrefersResolvedDiscoveryAndPreservesSummary(t *testing.T) {
	providerID := EncodeEntityProviderID(testSeriesID)
	local := metadata.SearchResult{
		ProviderID: providerID, ProviderName: "heya", Title: "The Office", Year: "2005",
		PosterURL: "poster.webp", Confidence: 1, RequiresReview: true, Enriched: true,
		AltTitles: []string{"The Office"}, ExternalIDs: map[string]string{"tmdb": "2316"},
	}
	resolved := metadata.SearchResult{
		ProviderID: providerID, ProviderName: "heya", Title: "The Office (US)", Year: "2005",
		Confidence: 1, Recommendation: "existing_entity", RequiresReview: false, HeyaSlug: testSeriesID,
	}

	results := mergeSearchResults([]metadata.SearchResult{local}, []metadata.SearchResult{resolved})
	if len(results) != 1 {
		t.Fatalf("merged results = %#v", results)
	}
	got := results[0]
	if got.RequiresReview || got.Recommendation != "existing_entity" || got.Title != "The Office (US)" {
		t.Fatalf("resolved discovery did not win merge: %#v", got)
	}
	if got.PosterURL != "poster.webp" || got.ExternalIDs["tmdb"] != "2316" || len(got.AltTitles) != 1 || !got.Enriched {
		t.Fatalf("canonical summary fields were lost: %#v", got)
	}
}

func TestDiscoveryRequestIncludesTVEvidence(t *testing.T) {
	request := discoveryRequest("tv_show", metadata.SearchQuery{
		Title:       "The Office (US)",
		Year:        "2005",
		Identifiers: map[string]string{"tvdb": "73244", "imdb_id": "tt0386676"},
		Aliases:     []string{"The Office"},
		Episodes: []metadata.EpisodeHint{
			{Season: 1, Number: 1},
			{Number: 2},
		},
	})
	if request.Hints == nil || request.Hints.Year == nil || *request.Hints.Year != 2005 {
		t.Fatalf("year hint = %#v", request.Hints)
	}
	if request.Hints.Aliases == nil || len(*request.Hints.Aliases) != 1 || (*request.Hints.Aliases)[0] != "The Office" {
		t.Fatalf("alias hints = %#v", request.Hints.Aliases)
	}
	if request.Hints.Episodes == nil || len(*request.Hints.Episodes) != 2 {
		t.Fatalf("episode hints = %#v", request.Hints.Episodes)
	}
	first := (*request.Hints.Episodes)[0]
	if first.Season == nil || *first.Season != 1 || first.Number == nil || *first.Number != 1 {
		t.Fatalf("first episode hint = %#v", first)
	}
	second := (*request.Hints.Episodes)[1]
	if second.Season != nil || second.Number == nil || *second.Number != 2 {
		t.Fatalf("absolute episode hint = %#v", second)
	}
	if request.Identifiers == nil || len(*request.Identifiers) != 2 || (*request.Identifiers)[0].Scheme != "imdb" || (*request.Identifiers)[1].Scheme != "tvdb" {
		t.Fatalf("identifier evidence = %#v", request.Identifiers)
	}
}

func TestDiscoveryRequestIncludesCompactReleaseEvidence(t *testing.T) {
	request := discoveryRequest("artist", metadata.SearchQuery{
		Title: "Daft Punk",
		Releases: []metadata.ReleaseHint{
			{Title: "Homework", Year: "1997", Type: "album", Identifiers: map[string]string{
				"itunes_album": "123", "deezer_album": "456", "itunes_artist": "ignored-artist-id",
			}},
			{Title: "Discovery", Year: "2001", Type: "album"},
		},
	})
	if request.Hints == nil || request.Hints.Releases == nil || len(*request.Hints.Releases) != 2 {
		t.Fatalf("release hints = %#v", request.Hints)
	}
	first := (*request.Hints.Releases)[0]
	if first.Title != "Homework" || first.Year == nil || *first.Year != 1997 || first.Type == nil || *first.Type != "album" {
		t.Fatalf("first release hint = %#v", first)
	}
	if first.Identifiers == nil || len(*first.Identifiers) != 3 {
		t.Fatalf("first release identifiers = %#v", first.Identifiers)
	}
	if (*first.Identifiers)[0].Scheme != "deezer_album" || (*first.Identifiers)[1].Scheme != "itunes_album" || (*first.Identifiers)[2].Scheme != "itunes_artist" {
		t.Fatalf("sorted first release identifiers = %#v", *first.Identifiers)
	}
}

func TestFlattenExternalIDsPreservesMusicBrainzNamespace(t *testing.T) {
	ids := flattenExternalIDs([]ExternalID{
		{Provider: "musicbrainz", Namespace: "release_group", Value: "group-id"},
	})
	if ids["mbid"] != "group-id" || ids["musicbrainz_release_group"] != "group-id" || ids["musicbrainz:release_group"] != "group-id" {
		t.Fatalf("flattened release-group IDs = %#v", ids)
	}
}

func TestKindSpecificCanonicalMapping(t *testing.T) {
	client, err := NewClient("http://metadata.test", "")
	if err != nil {
		t.Fatal(err)
	}
	provider := NewHeyaProvider(client)

	movie := mustMapDocument(t, provider, `{
      "schema_version":1,"projection_version":9,"id":"`+testMovieID+`","kind":"movie","slug":"matrix",
      "external_ids":[{"provider":"tmdb","namespace":"movie","value":"603"}],
      "display":{"title":"The Matrix","original_title":"The Matrix","year":1999,"image_id":"`+testImageID+`"},
      "data":{"titles":[{"value":"The Matrix","language":"en","type":"display"}],"overviews":[{"value":"Wake up.","language":"en","type":"overview"}],
      "classification":{"genres":["Science Fiction"],"original_language":"en"},"release":{"normalized_status":"released","release_events":[{"country":"US","type":"theatrical","date":"1999-03-31","certification":"R"}]},
      "measurements":{"runtime_minutes":136},"ratings":[{"provider":"tmdb","value":8.2,"scale_min":0,"scale_max":10,"votes":100}],
      "images":[{"id":"`+testImageID+`","class":"backdrop","provider":"tmdb"}],
      "credits":[{"person_entity_id":"`+testPersonID+`","provider":"tmdb","provider_person_id":"6384","display_name":"Keanu Reeves","credit_type":"cast","character":"Neo","order":0}],
	      "recommendations":[{"entity_id":"`+testEpisodeID+`","provider":"tmdb","provider_target_id":"604","title":"The Matrix Reloaded","year":2003,"image_id":"`+testImageID+`","provider_score":7.0}]}}`)
	if movie.CanonicalID != testMovieID || movie.SchemaVersion != 1 || movie.ProjectionVersion != 9 || movie.ExternalIDs["tmdb"] != "603" {
		t.Fatalf("movie identity mapping: %#v", movie)
	}
	if len(movie.Cast) != 1 || movie.Cast[0].CanonicalID != testPersonID || len(movie.Recommendations) != 1 || movie.Recommendations[0].CanonicalID != testEpisodeID {
		t.Fatalf("movie relationship mapping: cast=%#v recommendations=%#v", movie.Cast, movie.Recommendations)
	}
	if movie.PosterURL != "http://metadata.test/api/v2/images/"+testImageID {
		t.Fatalf("movie image URL = %q", movie.PosterURL)
	}

	episodic := mustMapDocument(t, provider, `{
      "schema_version":1,"projection_version":5,"id":"`+testMovieID+`","kind":"anime","external_ids":[],
      "display":{"title":"Example Anime","year":2024,"image_id":"`+testImageID+`"},
      "data":{"classification":{"status":"ended","language":"ja"},"episode_count":1,"season_count":1,
      "seasons":[{"id":"`+testSeasonID+`","number":0,"name":"Specials","episode_count":1,"episode_ids":["`+testEpisodeID+`"]}],
      "episodes":[{"id":"`+testEpisodeID+`","season_id":"`+testSeasonID+`","titles":[{"value":"OVA","language":"en","type":"display"}],
	      "numbers":[{"scheme":"aired","season":0,"number":1,"provider":"anidb"},{"scheme":"absolute","number":13}],"is_special":true,"episode_type":"ova","runtime_minutes":24}]}}`)
	if len(episodic.Seasons) != 1 || episodic.Seasons[0].CanonicalID != testSeasonID || len(episodic.Seasons[0].Episodes) != 1 {
		t.Fatalf("episodic structure mapping: %#v", episodic.Seasons)
	}
	episode := episodic.Seasons[0].Episodes[0]
	if episode.CanonicalID != testEpisodeID || episode.Number != 1 || episode.AbsoluteNumber != 13 || !episode.IsSpecial || episode.EpisodeType != 3 {
		t.Fatalf("episode mapping: %#v", episode)
	}

	book := mustMapDocument(t, provider, `{
      "schema_version":1,"projection_version":3,"id":"`+testMovieID+`","kind":"book_work","external_ids":[{"provider":"openlibrary","namespace":"work","value":"OL1W"}],
      "display":{"title":"A Book","year":2020,"image_id":"`+testImageID+`"},
      "data":{"description":"Description","authors":[{"id":"`+testAuthorID+`","name":"An Author","external_ids":[{"provider":"openlibrary","namespace":"author","value":"OL1A"}]}],
      "subjects":["Fiction"],"languages":["eng"],"first_publish_year":2020,"isbn_13":["9780000000000"],
	      "series":[{"entity_id":"`+testSeriesID+`","name":"A Series","position":"2","provider":"openlibrary"}]}}`)
	if book.AuthorCanonicalID != testAuthorID || book.AuthorExternalIDs["openlibrary"] != "OL1A" || book.SeriesName != "A Series" || book.SeriesNum != 2 {
		t.Fatalf("book identity/series mapping: %#v", book)
	}

	release := mustMapDocument(t, provider, `{
      "schema_version":1,"projection_version":4,"id":"`+testMovieID+`","kind":"release","external_ids":[],"display":{"title":"An Album","year":2022},
      "data":{"date":"2022-01-01","country":"JP","barcode":"123","labels":[{"name":"Label","catalog_number":"CAT-1"}],
      "media":[{"position":1,"tracks":[{"recording_entity_id":"`+testRecordID+`","lyrics_available":true,"sequence":1,"title":"Song","duration_ms":123000,
	      "recording":{"provider":"musicbrainz","provider_id":"recording-mbid","isrcs":["JPXXX0000001"]}}]}]}}`)
	if len(release.Tracks) != 1 || release.Tracks[0].CanonicalID != testRecordID || release.Tracks[0].Duration != 123 || release.Tracks[0].ISRC != "JPXXX0000001" || !release.Tracks[0].LyricsAvailable {
		t.Fatalf("release recording mapping: %#v", release.Tracks)
	}
}

func TestCanonicalImageSelectionsDrivePrimaryArtwork(t *testing.T) {
	client, err := NewClient("http://metadata.test", "")
	if err != nil {
		t.Fatal(err)
	}
	provider := NewHeyaProvider(client)
	detail := &metadata.MediaDetail{CanonicalKind: "artist", PosterURL: "stale"}
	provider.applyCanonicalImages(detail, &gen.EntityImagesOutputBody{
		Selections: map[string]string{"profile": testImageID},
	})
	if detail.PosterURL != "http://metadata.test/api/v2/images/"+testImageID {
		t.Fatalf("selected profile URL = %q", detail.PosterURL)
	}
	if detail.Artwork == nil || detail.ArtistImages == nil {
		t.Fatalf("authoritative empty artwork was not applied: %#v", detail)
	}
}

func TestArtistTopTracksUseCanonicalRecordingIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/top-tracks"):
			_, _ = w.Write([]byte(`{"artist_id":"` + testMovieID + `","results":[{"rank":1,"title":"Song","provider":"lastfm","external_ids":[{"provider":"musicbrainz","namespace":"recording","value":"mbid"}],"recording_entity_id":"` + testRecordID + `","playcount":20,"listeners":10}],"sources":[],"total":1,"offset":0,"limit":100}`))
		case strings.HasSuffix(r.URL.Path, "/relations"):
			_, _ = w.Write([]byte(`{"relations":[],"total":0,"offset":0,"limit":100}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	provider := NewHeyaProvider(client)
	artist := mustMapDocument(t, provider, `{"schema_version":1,"projection_version":2,"id":"`+testMovieID+`","kind":"artist","external_ids":[],"display":{"name":"Artist"},"data":{"classification":{},"lifecycle":{},"names":[],"images":[]}}`)
	if !artist.ArtistTopTracksLoaded || len(artist.ArtistTopTracks) != 1 || artist.ArtistTopTracks[0].Rank != 1 || artist.ArtistTopTracks[0].RecordingEntityID != testRecordID || artist.ArtistTopTracks[0].MBID != "mbid" {
		t.Fatalf("top tracks mapping: %#v", artist.ArtistTopTracks)
	}
}

func TestRecordingLyricsUsesCanonicalEndpointAndPreservesBothForms(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/recordings/"+testRecordID+"/lyrics" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"recording_id":"` + testRecordID + `","items":[{"id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","provider":"lrclib","provider_record_id":"1","track_name":"Song","artist_name":"Artist","instrumental":false,"plain_lyrics":"plain line","synced_lyrics":"[00:01.00]synced line","content_checksum":"sum","source_observation_id":"observation","observed_at":"2026-07-13T23:42:23+02:00"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	provider := NewHeyaProvider(client)
	items, err := provider.RecordingLyrics(context.Background(), testRecordID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].PlainLyrics != "plain line" || items[0].SyncedLyrics != "[00:01.00]synced line" || items[0].Instrumental {
		t.Fatalf("recording lyrics = %#v", items)
	}
}

func TestCutoverFlowUsesOnlyV2Endpoints(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		if r.URL.Path != "/api/v2/search" && r.Header.Get("X-Heya-LastFM-API-Key") != "lastfm-sentinel" {
			t.Errorf("request-scoped provider credential missing on %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/discoveries":
			var request gen.Request
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode discovery request: %v", err)
			}
			if request.Identifiers == nil || len(*request.Identifiers) != 2 {
				t.Errorf("discovery identifiers = %#v", request.Identifiers)
			}
			_, _ = w.Write([]byte(`{"id":"99999999-9999-4999-8999-999999999999","state":"completed","expires_at":"2099-01-01T00:00:00Z","result":{"kind":"movie","query":"The Matrix","entity_id":"` + testMovieID + `","identifier_evidence":[{"scheme":"tmdb","value":"603","outcome":"matched"}],"recommendation":"strong_match","status":"completed","schema_version":1,"observed_at":"2026-01-01T00:00:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/entities/"+testMovieID:
			_, _ = w.Write([]byte(`{"schema_version":1,"projection_version":1,"id":"` + testMovieID + `","kind":"movie","external_ids":[{"provider":"tmdb","namespace":"movie","value":"603"}],"display":{"title":"The Matrix","year":1999},"data":{"classification":{},"release":{},"measurements":{}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/entities/"+testMovieID+"/credits":
			_, _ = w.Write([]byte(`{"results":[],"total":0,"offset":0,"limit":250}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/entities/"+testMovieID+"/images":
			_, _ = w.Write([]byte(`{"language_preferences":[],"selections":{},"results":[]}`))
		default:
			http.Error(w, "unexpected endpoint", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	provider := NewHeyaProvider(client).WithProviderCredentials(ProviderCredentials{LastFMAPIKey: "lastfm-sentinel"})
	results, err := provider.Search(context.Background(), metadata.KindMovie, metadata.SearchQuery{Title: "The Matrix", Year: "1999", Identifiers: map[string]string{"tmdb": "603", "imdb": "tt0133093"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].RequiresReview {
		t.Fatalf("search results: %#v", results)
	}
	detail, err := provider.GetDetail(context.Background(), results[0].ProviderID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if detail.CanonicalID != testMovieID {
		t.Fatalf("canonical detail ID = %q", detail.CanonicalID)
	}
	mu.Lock()
	defer mu.Unlock()
	seenCredits, seenImages := false, false
	for _, path := range paths {
		if !strings.HasPrefix(path, "/api/v2/") || strings.Contains(path, "/api/v1/") {
			t.Fatalf("old metadata endpoint used: %q (all paths: %#v)", path, paths)
		}
		seenCredits = seenCredits || strings.HasSuffix(path, "/credits")
		seenImages = seenImages || strings.HasSuffix(path, "/images")
		if path == "/api/v2/search" || path == "/api/v2/resolutions" {
			t.Fatalf("identifier-first direct hit used unnecessary endpoint %q: %#v", path, paths)
		}
	}
	if !seenCredits || !seenImages {
		t.Fatalf("complete canonical resources were not traversed: %#v", paths)
	}
}

func TestQueryOnlyExactLocalHitDoesNotCreateDiscovery(t *testing.T) {
	var discoveries int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/search":
			_, _ = w.Write([]byte(`{"results":[{"schema_version":1,"projection_version":7,"id":"` + testMovieID + `","kind":"movie","display":{"title":"Avatar: The Way of Water","year":2022},"external_ids":[]}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/discoveries":
			discoveries++
			http.Error(w, "query-only exact hit must remain side-effect free", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	results, err := NewHeyaProvider(client).Search(context.Background(), metadata.KindMovie, metadata.SearchQuery{
		Title: "Avatar The Way of Water", Year: "2022",
	})
	if err != nil {
		t.Fatal(err)
	}
	if discoveries != 0 || len(results) != 1 || results[0].ProviderID != EncodeEntityProviderID(testMovieID) {
		t.Fatalf("discoveries=%d results=%#v", discoveries, results)
	}
}

func TestCreditsReadsEveryPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("offset") {
		case "0":
			_, _ = w.Write([]byte(`{"results":[{"person_entity_id":"` + testPersonID + `","provider":"tmdb","provider_person_id":"1","display_name":"One","credit_type":"cast","character":"Lead","order":0}],"total":2,"offset":0,"limit":1}`))
		case "1":
			_, _ = w.Write([]byte(`{"results":[{"person_entity_id":"` + testAuthorID + `","provider":"tmdb","provider_person_id":"2","display_name":"Two","credit_type":"crew","job":"Director","order":0}],"total":2,"offset":1,"limit":1}`))
		default:
			http.Error(w, "unexpected offset", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	credits, err := client.Credits(context.Background(), testMovieID, ProviderCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	if len(credits) != 2 || credits[0].DisplayName != "One" || credits[1].DisplayName != "Two" {
		t.Fatalf("credits = %#v", credits)
	}
}

func TestPersonCreditsReadEveryPageAndForwardCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Heya-TMDB-API-Key"); got != "tmdb-sentinel" {
			t.Errorf("person credits credential = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		person := `"person":{"entity_id":"` + testPersonID + `","display_name":"Person"}`
		switch r.URL.Query().Get("offset") {
		case "0":
			_, _ = w.Write([]byte(`{"credits":[{"credit_type":"cast","kind":"movie","provider":"tmdb","title":"One"}],"total":2,"offset":0,"limit":1,` + person + `}`))
		case "1":
			_, _ = w.Write([]byte(`{"credits":[{"credit_type":"crew","kind":"movie","provider":"tmdb","title":"Two"}],"total":2,"offset":1,"limit":1,` + person + `}`))
		default:
			http.Error(w, "unexpected offset", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.personCredits(context.Background(), testPersonID, ProviderCredentials{TMDBAPIKey: "tmdb-sentinel"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Credits == nil || len(*page.Credits) != 2 || (*page.Credits)[0].Title != "One" || (*page.Credits)[1].Title != "Two" {
		t.Fatalf("person credits = %#v", page.Credits)
	}
}

func TestPersonByEntityUsesCanonicalReverseFilmography(t *testing.T) {
	var creditRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/persons/" + testPersonID:
			_, _ = w.Write([]byte(`{"schema_version":1,"projection_version":1,"id":"` + testPersonID + `","kind":"person","slug":"person","display":{"title":"Person"},"external_ids":[{"provider":"tvmaze","namespace":"person","value":"42"}],"data":{"names":[],"credits":[{"credit_type":"cast","kind":"tv_show","provider":"tvmaze","title":"Preview"}],"credit_total":2},"freshness":{"state":"fresh"}}`))
		case "/api/v2/persons/" + testPersonID + "/credits":
			creditRequests++
			person := `"person":{"entity_id":"` + testPersonID + `","display_name":"Person"}`
			switch r.URL.Query().Get("offset") {
			case "0":
				_, _ = w.Write([]byte(`{"credits":[{"credit_type":"cast","kind":"tv_show","provider":"tvmaze","title":"One"}],"total":2,"offset":0,"limit":1,` + person + `}`))
			case "1":
				_, _ = w.Write([]byte(`{"credits":[{"credit_type":"crew","kind":"tv_show","provider":"tvmaze","title":"Two"}],"total":2,"offset":1,"limit":1,` + person + `}`))
			default:
				http.Error(w, "unexpected offset", http.StatusBadRequest)
			}
		case "/api/v2/entities/" + testPersonID + "/images":
			_, _ = w.Write([]byte(`{"language_preferences":[],"selections":{},"results":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	person, err := GetPersonByEntityFromHeya(context.Background(), client, testPersonID)
	if err != nil {
		t.Fatal(err)
	}
	if creditRequests != 2 || len(person.Payload.Cast) != 1 || len(person.Payload.Crew) != 1 {
		t.Fatalf("credit requests=%d person=%#v", creditRequests, person.Payload)
	}
}

func TestRatingsReadEveryPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("offset") {
		case "0":
			_, _ = w.Write([]byte(`{"results":[{"system":"tmdb","value":8,"scale_min":0,"scale_max":10}],"total":2,"offset":0,"limit":1}`))
		case "1":
			_, _ = w.Write([]byte(`{"results":[{"system":"imdb","value":7.9,"scale_min":0,"scale_max":10}],"total":2,"offset":1,"limit":1}`))
		default:
			http.Error(w, "unexpected offset", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ratings, err := client.Ratings(context.Background(), testMovieID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ratings) != 2 || ratings[0].System != "tmdb" || ratings[1].System != "imdb" {
		t.Fatalf("ratings = %#v", ratings)
	}
}

func TestTopTracksReadEveryPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("offset") {
		case "0":
			_, _ = w.Write([]byte(`{"artist_id":"` + testMovieID + `","results":[{"rank":1,"title":"One","provider":"lastfm"}],"sources":[],"total":2,"offset":0,"limit":1}`))
		case "1":
			_, _ = w.Write([]byte(`{"artist_id":"` + testMovieID + `","results":[{"rank":1,"title":"Two","provider":"apple"}],"sources":[],"total":2,"offset":1,"limit":1}`))
		default:
			http.Error(w, "unexpected offset", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	tracks, err := client.TopTracks(context.Background(), testMovieID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 || tracks[0].Title != "One" || tracks[1].Title != "Two" {
		t.Fatalf("top tracks = %#v", tracks)
	}
}

func TestFuzzyLocalHitDoesNotSuppressDiscovery(t *testing.T) {
	var discoveries int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/search":
			_, _ = w.Write([]byte(`{"results":[{"schema_version":1,"projection_version":1,"id":"` + testPersonID + `","kind":"movie","display":{"title":"Matrix Resurrections","year":2021},"external_ids":[]}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/discoveries":
			discoveries++
			_, _ = w.Write([]byte(`{"id":"99999999-9999-4999-8999-999999999999","state":"completed","expires_at":"2099-01-01T00:00:00Z","result":{"kind":"movie","query":"The Matrix","recommendation":"strong_match","status":"completed","schema_version":1,"observed_at":"2026-01-01T00:00:00Z","candidates":[{"candidate_ref":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","rank":1,"confidence":1,"match":"strong","display":{"title":"The Matrix","year":1999},"evidence":[]}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	results, err := NewHeyaProvider(client).Search(context.Background(), metadata.KindMovie, metadata.SearchQuery{Title: "The Matrix", Year: "1999"})
	if err != nil {
		t.Fatal(err)
	}
	if discoveries != 1 || len(results) != 2 {
		t.Fatalf("discoveries=%d results=%#v", discoveries, results)
	}
	if !results[0].RequiresReview || results[1].RequiresReview {
		t.Fatalf("local fuzzy/discovered review gates = %#v", results)
	}
}

func TestAsyncDiscoveryAndResolutionArePolled(t *testing.T) {
	const discoveryID = "99999999-9999-4999-8999-999999999999"
	var discoveryPolls, jobPolls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/discoveries":
			if got := r.Header.Get("X-Heya-TMDB-API-Key"); got != "tmdb-sentinel" {
				t.Errorf("discovery provider credential = %q", got)
			}
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"` + discoveryID + `","state":"queued","expires_at":"2099-01-01T00:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/discoveries/"+discoveryID:
			discoveryPolls++
			_, _ = w.Write([]byte(`{"id":"` + discoveryID + `","state":"completed","expires_at":"2099-01-01T00:00:00Z","result":{"kind":"movie","query":"The Matrix","recommendation":"strong_match","status":"completed","schema_version":1,"observed_at":"2026-01-01T00:00:00Z","providers":["tmdb"],"candidates":[]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/resolutions":
			if got := r.Header.Get("X-Heya-Discogs-API-Key"); got != "discogs-sentinel" {
				t.Errorf("resolution provider credential = %q", got)
			}
			var input map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Errorf("decode resolution input: %v", err)
			}
			if len(input) != 1 || string(input["candidate_ref"]) != `"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"` {
				t.Errorf("resolution leaked non-opaque input: %#v", input)
			}
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"state":"working","job":{"id":42,"kind":"resolve","state":"working"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/jobs/42":
			jobPolls++
			_, _ = w.Write([]byte(`{"id":42,"kind":"resolve","state":"completed","entity_id":"` + testMovieID + `"}`))
		default:
			http.Error(w, "unexpected endpoint", http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := client.Discover(context.Background(), gen.Request{Kind: "movie", Query: strPointer("The Matrix")}, ProviderCredentials{TMDBAPIKey: "tmdb-sentinel"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if discovery.State != gen.DiscoveryResourceStateCompleted || discoveryPolls != 1 {
		t.Fatalf("discovery = %#v, polls = %d", discovery, discoveryPolls)
	}
	entityID, err := client.Resolve(context.Background(), uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"), "artist", ProviderCredentials{DiscogsAPIKey: "discogs-sentinel"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if entityID != testMovieID || jobPolls != 1 {
		t.Fatalf("entity ID = %q, job polls = %d", entityID, jobPolls)
	}
}

func TestAsyncDiscoveryIsDeferredForDurableScannerWork(t *testing.T) {
	const discoveryID = "99999999-9999-4999-8999-999999999999"
	var discoveryPolls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/discoveries":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"` + discoveryID + `","state":"queued","expires_at":"2099-01-01T00:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/discoveries/"+discoveryID:
			discoveryPolls++
			if discoveryPolls == 1 {
				_, _ = w.Write([]byte(`{"id":"` + discoveryID + `","state":"working","expires_at":"2099-01-01T00:00:00Z"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"` + discoveryID + `","state":"completed","expires_at":"2099-01-01T00:00:00Z","result":{"kind":"movie","query":"The Matrix","recommendation":"strong_match","status":"completed","schema_version":1,"observed_at":"2026-01-01T00:00:00Z","candidates":[]}}`))
		default:
			http.Error(w, "unexpected endpoint", http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.WithDeferredRemoteWork(context.Background(), 30*time.Second)

	discovery, err := client.Discover(ctx, gen.Request{Kind: "movie", Query: strPointer("The Matrix")}, ProviderCredentials{}, nil)
	if discovery != nil {
		t.Fatalf("initial asynchronous discovery = %#v, want nil", discovery)
	}
	if retryAfter, ok := metadata.DeferredWorkRetryAfter(err); !ok || retryAfter != 30*time.Second {
		t.Fatalf("initial deferred error = %v, retry_after = %s, ok = %v", err, retryAfter, ok)
	}
	if discoveryPolls != 0 {
		t.Fatalf("initial request polled %d times; durable caller should be released", discoveryPolls)
	}

	id := uuid.MustParse(discoveryID)
	discovery, err = client.checkDiscovery(ctx, id, ProviderCredentials{})
	if discovery != nil {
		t.Fatalf("working discovery = %#v, want nil", discovery)
	}
	if retryAfter, ok := metadata.DeferredWorkRetryAfter(err); !ok || retryAfter != 30*time.Second {
		t.Fatalf("working deferred error = %v, retry_after = %s, ok = %v", err, retryAfter, ok)
	}
	if discoveryPolls != 1 {
		t.Fatalf("deferred retry polls = %d, want exactly one", discoveryPolls)
	}

	discovery, err = client.checkDiscovery(ctx, id, ProviderCredentials{})
	if err != nil || discovery == nil || discovery.State != gen.DiscoveryResourceStateCompleted {
		t.Fatalf("completed deferred retry = %#v, err = %v", discovery, err)
	}
	if discoveryPolls != 2 {
		t.Fatalf("completion retry polls = %d, want two total", discoveryPolls)
	}
}

func TestAsyncResolutionIsDeferredForDurableScannerWork(t *testing.T) {
	var jobPolls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/resolutions":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"state":"accepted","job":{"id":42,"kind":"resolve","state":"working"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/jobs/42":
			jobPolls++
			if jobPolls == 1 {
				_, _ = w.Write([]byte(`{"id":42,"kind":"resolve","state":"working"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":42,"kind":"resolve","state":"completed","entity_id":"` + testMovieID + `"}`))
		default:
			http.Error(w, "unexpected endpoint", http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.WithDeferredRemoteWork(context.Background(), 30*time.Second)
	candidateRef := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")

	entityID, err := client.Resolve(ctx, candidateRef, "movie", ProviderCredentials{}, nil)
	if entityID != "" {
		t.Fatalf("initial asynchronous resolution entity = %q, want empty", entityID)
	}
	if retryAfter, ok := metadata.DeferredWorkRetryAfter(err); !ok || retryAfter != 30*time.Second {
		t.Fatalf("initial deferred error = %v, retry_after = %s, ok = %v", err, retryAfter, ok)
	}
	if jobPolls != 0 {
		t.Fatalf("initial resolution polled %d times; durable caller should be released", jobPolls)
	}

	requestKey := workflowRequestKey("resolution", gen.ResolutionInputBody{CandidateRef: candidateRef})
	entityID, err = client.checkResolutionJob(ctx, requestKey, 42, nil, ProviderCredentials{})
	if entityID != "" {
		t.Fatalf("working resolution entity = %q, want empty", entityID)
	}
	if retryAfter, ok := metadata.DeferredWorkRetryAfter(err); !ok || retryAfter != 30*time.Second {
		t.Fatalf("working deferred error = %v, retry_after = %s, ok = %v", err, retryAfter, ok)
	}
	if jobPolls != 1 {
		t.Fatalf("deferred resolution retry polls = %d, want exactly one", jobPolls)
	}

	entityID, err = client.checkResolutionJob(ctx, requestKey, 42, nil, ProviderCredentials{})
	if err != nil || entityID != testMovieID {
		t.Fatalf("completed deferred resolution entity = %q, err = %v", entityID, err)
	}
	if jobPolls != 2 {
		t.Fatalf("completion retry polls = %d, want two total", jobPolls)
	}
}

func TestTransientDiscoveryCreateIsDeferredForDurableScannerWork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "45")
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.WithDeferredRemoteWork(context.Background(), 30*time.Second)

	discovery, err := client.Discover(ctx, gen.Request{Kind: "movie", Query: strPointer("Everything Everywhere All at Once")}, ProviderCredentials{}, nil)
	if discovery != nil {
		t.Fatalf("transient discovery = %#v, want nil", discovery)
	}
	if retryAfter, ok := metadata.DeferredWorkRetryAfter(err); !ok || retryAfter != 45*time.Second {
		t.Fatalf("transient create error = %v, retry_after = %s, ok = %v", err, retryAfter, ok)
	}
}

func TestPollingHonorsRetryAfterAndStaysJittered(t *testing.T) {
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	response := &http.Response{Header: make(http.Header)}
	response.Header.Set("Retry-After", "3")
	if got := retryAfterDuration(response, now); got != 3*time.Second {
		t.Fatalf("delta Retry-After = %s", got)
	}
	response.Header.Set("Retry-After", now.Add(5*time.Second).Format(http.TimeFormat))
	if got := retryAfterDuration(response, now); got != 5*time.Second {
		t.Fatalf("date Retry-After = %s", got)
	}
	response.Header.Set("Retry-After", "3")
	for range 20 {
		got := pollDelay(200*time.Millisecond, response)
		if got < 3*time.Second || got > 3600*time.Millisecond {
			t.Fatalf("Retry-After jitter outside allowed range: %s", got)
		}
	}
}

func TestResponseErrorDecodesProblemJSON(t *testing.T) {
	err := responseError("discover metadata", http.StatusUnprocessableEntity, []byte(`{
      "type":"https://metadata.test/problems/invalid-identifiers",
      "title":"Invalid identifier evidence",
      "status":422,
      "detail":"the submitted identifiers conflict"
    }`))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Problem == nil {
		t.Fatalf("problem response was not decoded: %T %#v", err, err)
	}
	if got := stringValue(apiErr.Problem.Type); got != "https://metadata.test/problems/invalid-identifiers" {
		t.Fatalf("problem type = %q", got)
	}
	if !strings.Contains(err.Error(), "the submitted identifiers conflict") {
		t.Fatalf("problem detail missing from error: %v", err)
	}
}

func mustMapDocument(t *testing.T, provider *HeyaProvider, raw string) *metadata.MediaDetail {
	t.Helper()
	if !json.Valid([]byte(raw)) {
		t.Fatalf("invalid test JSON: %s", raw)
	}
	detail, err := provider.mapDocument(context.Background(), []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return detail
}

func strPointer(value string) *string { return &value }
