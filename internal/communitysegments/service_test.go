package communitysegments

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMovieAggregatesProvidersAndPreservesRuntime(t *testing.T) {
	var introQuery, skipmeBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/media":
			introQuery = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"credits":[{"start_ms":3431000,"end_ms":null}]}`)
		case "/v1/movies":
			body, _ := io.ReadAll(r.Body)
			skipmeBody = string(body)
			w.WriteHeader(http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := New(nil, Options{HTTPClient: server.Client(), TheIntroDBBaseURL: server.URL, SkipMeDBBaseURL: server.URL})
	candidates, found, err := service.Movie(context.Background(), ExternalIDs{TMDB: 603}, 8_160_000)
	if err != nil {
		t.Fatal(err)
	}
	if !found || len(candidates) != 1 {
		t.Fatalf("found=%v candidates=%+v", found, candidates)
	}
	if candidates[0].Source != "theintrodb" || candidates[0].EndMs != nil {
		t.Fatalf("unexpected candidate: %+v", candidates[0])
	}
	if !strings.Contains(introQuery, "duration_ms=8160000") || !strings.Contains(skipmeBody, `"duration_ms":8160000`) {
		t.Fatalf("runtime not forwarded: query=%q body=%q", introQuery, skipmeBody)
	}
}

func TestEpisodeAddsAniSkipWhenMALIdentityExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/media", r.URL.Path == "/v1/movies":
			w.WriteHeader(http.StatusNotFound)
		case strings.HasPrefix(r.URL.Path, "/v2/skip-times/16498/3"):
			_, _ = io.WriteString(w, `{"found":true,"results":[{"interval":{"startTime":75.25,"endTime":165.5},"skipType":"op","episodeLength":1440}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := New(nil, Options{HTTPClient: server.Client(), TheIntroDBBaseURL: server.URL, SkipMeDBBaseURL: server.URL, AniSkipBaseURL: server.URL})
	candidates, found, err := service.Episode(context.Background(), ExternalIDs{AniDB: 9541, MAL: 16498}, 1, 3, 1_440_000)
	if err != nil {
		t.Fatal(err)
	}
	if !found || len(candidates) != 1 {
		t.Fatalf("found=%v candidates=%+v", found, candidates)
	}
	if candidates[0].Source != "aniskip" || candidates[0].StartMs != 75_250 || candidates[0].DurationMs != 1_440_000 {
		t.Fatalf("unexpected candidate: %+v", candidates[0])
	}
}

func TestAnimeEpisodeMapsSplitCourToMALAndAniList(t *testing.T) {
	var skipMeBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/anime-list-mini.json":
			_, _ = io.WriteString(w, `[
				{"anidb_id":101,"mal_id":111,"anilist_id":1001,"tvdb_id":9001,"season":{"tvdb":1},"episode_offset":{"tvdb":0}},
				{"anidb_id":102,"mal_id":222,"anilist_id":2002,"tvdb_id":9001,"season":{"tvdb":1},"episode_offset":{"tvdb":12}}
			]`)
		case r.URL.Path == "/media":
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Path == "/v1/movies":
			body, _ := io.ReadAll(r.Body)
			skipMeBody = string(body)
			w.WriteHeader(http.StatusNotFound)
		case strings.HasPrefix(r.URL.Path, "/v2/skip-times/222/3"):
			_, _ = io.WriteString(w, `{"found":true,"results":[{"interval":{"startTime":60,"endTime":150},"skipType":"op","episodeLength":1440}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := New(nil, Options{
		HTTPClient: server.Client(), TheIntroDBBaseURL: server.URL,
		SkipMeDBBaseURL: server.URL, AniSkipBaseURL: server.URL,
		AnimeListURL: server.URL + "/anime-list-mini.json",
	})
	candidates, found, err := service.Episode(context.Background(), ExternalIDs{TVDB: 9001, Anime: true}, 1, 15, 1_440_000)
	if err != nil {
		t.Fatal(err)
	}
	if !found || len(candidates) != 1 || candidates[0].Source != "aniskip" {
		t.Fatalf("found=%v candidates=%+v", found, candidates)
	}
	if !strings.Contains(skipMeBody, `"anilist_id":2002`) {
		t.Fatalf("mapped AniList ID not forwarded to SkipMe.db: %s", skipMeBody)
	}
}

func TestOrdinaryTVEpisodeDoesNotLoadAnimeMappings(t *testing.T) {
	animeLoads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/anime-list-mini.json" {
			animeLoads++
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	service := New(nil, Options{
		HTTPClient: server.Client(), TheIntroDBBaseURL: server.URL,
		SkipMeDBBaseURL: server.URL, AniSkipBaseURL: server.URL,
		AnimeListURL: server.URL + "/anime-list-mini.json",
	})
	_, _, _ = service.Episode(context.Background(), ExternalIDs{TVDB: 9001}, 1, 1, 1_440_000)
	if animeLoads != 0 {
		t.Fatalf("ordinary TV lookup downloaded anime mappings %d time(s)", animeLoads)
	}
}

func TestAnimeMappingFailureRetriesWhenNoOtherSourceFindsSegments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/anime-list-mini.json" {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	service := New(nil, Options{
		HTTPClient: server.Client(), TheIntroDBBaseURL: server.URL,
		SkipMeDBBaseURL: server.URL, AniSkipBaseURL: server.URL,
		AnimeListURL: server.URL + "/anime-list-mini.json",
	})
	if _, found, err := service.Episode(context.Background(), ExternalIDs{TVDB: 9001, Anime: true}, 1, 1, 1_440_000); err == nil || found {
		t.Fatalf("expected retryable mapping failure, found=%v err=%v", found, err)
	}
}

func TestAllProviderFailuresAreRetryableErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	service := New(nil, Options{HTTPClient: server.Client(), TheIntroDBBaseURL: server.URL, SkipMeDBBaseURL: server.URL})
	if _, found, err := service.Movie(context.Background(), ExternalIDs{IMDB: "tt0133093"}, 0); err == nil || found {
		t.Fatalf("expected provider failure, found=%v err=%v", found, err)
	}
}

func TestCacheHorizons(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		state cachedSource
		age   time.Duration
		want  bool
	}{
		{"hit within 30 days", cachedSource{Exists: true, OK: true, Candidates: []Candidate{{Type: "intro"}}}, 29 * 24 * time.Hour, true},
		{"hit expires", cachedSource{Exists: true, OK: true, Candidates: []Candidate{{Type: "intro"}}}, 31 * 24 * time.Hour, false},
		{"miss within 7 days", cachedSource{Exists: true, OK: true}, 6 * 24 * time.Hour, true},
		{"miss expires", cachedSource{Exists: true, OK: true}, 8 * 24 * time.Hour, false},
		{"error retries after hour", cachedSource{Exists: true, OK: false}, 2 * time.Hour, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			test.state.FetchedAt = now.Add(-test.age)
			if got := fresh(test.state, now); got != test.want {
				t.Fatalf("fresh=%v want %v", got, test.want)
			}
		})
	}
}
