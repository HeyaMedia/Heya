// Package communitysegments owns Heya's direct integration with community
// skip-marker databases. It deliberately has no dependency on either metadata
// service.
package communitysegments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	hitHorizon   = 30 * 24 * time.Hour
	missHorizon  = 7 * 24 * time.Hour
	errorHorizon = time.Hour
)

type Candidate struct {
	Type        string `json:"type"`
	StartMs     int64  `json:"start_ms"`
	EndMs       *int64 `json:"end_ms,omitempty"`
	DurationMs  int64  `json:"duration_ms,omitempty"`
	Submissions int    `json:"submissions,omitempty"`
	Source      string `json:"source"`
}

type ExternalIDs struct {
	TMDB, TVDB, AniDB, MAL, AniList int
	IMDB                            string
	// Anime enables the TVDB/AniDB-to-MAL episode mapping used by AniSkip.
	// It is intentionally explicit so ordinary TV lookups never download the
	// community anime mapping dump.
	Anime bool
}

func IDsFromMap(values map[string]string) ExternalIDs {
	result := ExternalIDs{IMDB: values["imdb"]}
	result.TMDB, _ = strconv.Atoi(values["tmdb"])
	result.TVDB, _ = strconv.Atoi(values["tvdb"])
	result.AniDB, _ = strconv.Atoi(values["anidb"])
	result.MAL, _ = strconv.Atoi(firstNonEmpty(values["mal"], values["myanimelist"]))
	result.AniList, _ = strconv.Atoi(values["anilist"])
	return result
}

func (ids ExternalIDs) usable() bool {
	return ids.TMDB > 0 || ids.TVDB > 0 || ids.IMDB != "" || ids.MAL > 0 || ids.AniList > 0 || (ids.Anime && ids.AniDB > 0)
}

type Options struct {
	HTTPClient        *http.Client
	TheIntroDBAPIKey  string
	TheIntroDBBaseURL string
	SkipMeDBBaseURL   string
	AniSkipBaseURL    string
	AnimeListURL      string
}

type Service struct {
	db                                           *pgxpool.Pool
	http                                         *http.Client
	introKey, introBase, skipmeBase, aniskipBase string
	anime                                        *animeListsIndex
	now                                          func() time.Time
}

func New(db *pgxpool.Pool, options Options) *Service {
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 25 * time.Second}
	}
	service := &Service{db: db, http: httpClient, introKey: strings.TrimSpace(options.TheIntroDBAPIKey),
		introBase:   defaultString(options.TheIntroDBBaseURL, "https://api.theintrodb.org/v3"),
		skipmeBase:  defaultString(options.SkipMeDBBaseURL, "https://db.skipme.workers.dev"),
		aniskipBase: defaultString(options.AniSkipBaseURL, "https://api.aniskip.com"), now: time.Now}
	service.anime = newAnimeListsIndex(db, options.HTTPClient, options.AnimeListURL)
	return service
}

func (s *Service) Movie(ctx context.Context, ids ExternalIDs, durationMs int64) ([]Candidate, bool, error) {
	if !ids.usable() {
		return nil, false, nil
	}
	key := cacheKey("movie", ids, 0, 0, durationMs)
	fetchers := []sourceFetcher{
		{source: "theintrodb", fetch: func(ctx context.Context) ([]Candidate, bool, error) {
			return s.fetchTheIntroDB(ctx, ids, false, 0, 0, durationMs)
		}},
		{source: "skipmedb", fetch: func(ctx context.Context) ([]Candidate, bool, error) {
			return s.fetchSkipMeDB(ctx, ids, -1, -1, durationMs)
		}},
	}
	return s.aggregate(ctx, key, fetchers)
}

func (s *Service) Episode(ctx context.Context, ids ExternalIDs, season, episode int, durationMs int64) ([]Candidate, bool, error) {
	if !ids.usable() {
		return nil, false, nil
	}
	key := cacheKey("episode", ids, season, episode, durationMs)
	mal, mappingErr := s.animeMapping(ctx, ids, season, episode)
	skipMeIDs := ids
	if mal.AniListID > 0 {
		skipMeIDs.AniList = mal.AniListID
	}
	var fetchers []sourceFetcher
	if ids.TMDB > 0 || ids.TVDB > 0 || ids.IMDB != "" {
		fetchers = append(fetchers, sourceFetcher{source: "theintrodb", fetch: func(ctx context.Context) ([]Candidate, bool, error) {
			return s.fetchTheIntroDB(ctx, ids, true, season, episode, durationMs)
		}})
	}
	if skipMeIDs.TMDB > 0 || skipMeIDs.TVDB > 0 || skipMeIDs.IMDB != "" || skipMeIDs.AniList > 0 {
		fetchers = append(fetchers, sourceFetcher{source: "skipmedb", fetch: func(ctx context.Context) ([]Candidate, bool, error) {
			return s.fetchSkipMeDB(ctx, skipMeIDs, season, episode, durationMs)
		}})
	}
	if mal.MALID > 0 {
		fetchers = append(fetchers, sourceFetcher{source: "aniskip", fetch: func(ctx context.Context) ([]Candidate, bool, error) {
			return s.fetchAniSkip(ctx, mal.MALID, mal.Episode, durationMs)
		}})
	}
	if len(fetchers) == 0 {
		return nil, false, mappingErr
	}
	candidates, found, err := s.aggregate(ctx, key, fetchers)
	if err != nil {
		return nil, false, err
	}
	if !found && mappingErr != nil {
		return nil, false, mappingErr
	}
	return candidates, found, nil
}

type malRef struct {
	MALID, Episode, AniListID int
}

func (s *Service) animeMapping(ctx context.Context, ids ExternalIDs, season, episode int) (malRef, error) {
	fallback := malRef{MALID: ids.MAL, Episode: episode, AniListID: ids.AniList}
	if !ids.Anime || s.anime == nil {
		return fallback, nil
	}
	entries, err := s.anime.entries(ctx)
	if err != nil {
		return fallback, fmt.Errorf("load anime episode mappings: %w", err)
	}
	if ids.TVDB > 0 {
		if ref := pickMALEntry(entries.byTVDB[ids.TVDB], season, episode); ref.MALID > 0 {
			return ref, nil
		}
	}
	if ids.AniDB > 0 {
		if entry, ok := entries.byAniDB[ids.AniDB]; ok {
			if ref := pickMALEntry([]animeListEntry{entry}, season, episode); ref.MALID > 0 {
				return ref, nil
			}
		}
	}
	return fallback, nil
}

func pickMALEntry(entries []animeListEntry, season, episode int) malRef {
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if entry.SeasonTVDB != nil && *entry.SeasonTVDB != season {
			continue
		}
		if entry.MALID > 0 && episode > entry.EpisodeOffsetTVDB {
			return malRef{MALID: entry.MALID, Episode: episode - entry.EpisodeOffsetTVDB, AniListID: entry.AniListID}
		}
	}
	return malRef{}
}

type sourceFetcher struct {
	source string
	fetch  func(context.Context) ([]Candidate, bool, error)
}
type cachedSource struct {
	Candidates []Candidate
	OK         bool
	FetchedAt  time.Time
	Exists     bool
}

func (s *Service) aggregate(ctx context.Context, key string, fetchers []sourceFetcher) ([]Candidate, bool, error) {
	var all []Candidate
	var failures []error
	answered := 0
	for _, fetcher := range fetchers {
		state, err := s.load(ctx, key, fetcher.source)
		if err != nil {
			failures = append(failures, err)
		}
		if !fresh(state, s.now()) {
			candidates, found, fetchErr := fetcher.fetch(ctx)
			if fetchErr == nil {
				state = cachedSource{Candidates: candidates, OK: true, FetchedAt: s.now(), Exists: true}
				if storeErr := s.store(ctx, key, fetcher.source, state); storeErr != nil {
					failures = append(failures, storeErr)
				}
				_ = found
			} else {
				failures = append(failures, fmt.Errorf("%s: %w", fetcher.source, fetchErr))
				// Serve known stale candidates, but remember the failure and retry
				// after the short error horizon.
				state.OK, state.FetchedAt, state.Exists = false, s.now(), true
				_ = s.store(ctx, key, fetcher.source, state)
			}
		}
		if state.Exists && (state.OK || len(state.Candidates) > 0) {
			answered++
		}
		all = append(all, state.Candidates...)
	}
	if answered == 0 && len(failures) > 0 {
		return nil, false, errors.Join(failures...)
	}
	return all, len(all) > 0, nil
}

func fresh(state cachedSource, now time.Time) bool {
	if !state.Exists || state.FetchedAt.IsZero() {
		return false
	}
	horizon := errorHorizon
	if state.OK {
		horizon = missHorizon
		if len(state.Candidates) > 0 {
			horizon = hitHorizon
		}
	}
	return now.Before(state.FetchedAt.Add(horizon))
}

func (s *Service) load(ctx context.Context, key, source string) (cachedSource, error) {
	if s.db == nil {
		return cachedSource{}, nil
	}
	var body []byte
	var result cachedSource
	err := s.db.QueryRow(ctx, `SELECT candidates, fetch_ok, fetched_at FROM community_segment_cache WHERE cache_key=$1 AND source=$2`, key, source).Scan(&body, &result.OK, &result.FetchedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cachedSource{}, nil
		}
		return cachedSource{}, fmt.Errorf("load community segment cache: %w", err)
	}
	result.Exists = true
	if err := json.Unmarshal(body, &result.Candidates); err != nil {
		return cachedSource{}, fmt.Errorf("decode community segment cache: %w", err)
	}
	return result, nil
}

func (s *Service) store(ctx context.Context, key, source string, state cachedSource) error {
	if s.db == nil {
		return nil
	}
	body, err := json.Marshal(state.Candidates)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO community_segment_cache (cache_key,source,candidates,fetch_ok,fetched_at) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (cache_key,source) DO UPDATE SET candidates=EXCLUDED.candidates,fetch_ok=EXCLUDED.fetch_ok,fetched_at=EXCLUDED.fetched_at`, key, source, body, state.OK, state.FetchedAt)
	return err
}

func cacheKey(kind string, ids ExternalIDs, season, episode int, durationMs int64) string {
	// Round to a second: runtime remains part of the release-cut identity while
	// sub-second probe noise does not explode the cache.
	durationMs = ((durationMs + 500) / 1000) * 1000
	value := fmt.Sprintf("%s:tmdb%d:tvdb%d:imdb%s:anidb%d:mal%d:al%d:anime%t:s%d:e%d:d%d", kind, ids.TMDB, ids.TVDB, ids.IMDB, ids.AniDB, ids.MAL, ids.AniList, ids.Anime, season, episode, durationMs)
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func (s *Service) fetchTheIntroDB(ctx context.Context, ids ExternalIDs, episodeLookup bool, season, episode int, durationMs int64) ([]Candidate, bool, error) {
	query := url.Values{}
	switch {
	case ids.TMDB > 0:
		query.Set("tmdb_id", strconv.Itoa(ids.TMDB))
	case ids.TVDB > 0:
		query.Set("tvdb_id", strconv.Itoa(ids.TVDB))
	case ids.IMDB != "":
		query.Set("imdb_id", ids.IMDB)
	default:
		return nil, false, nil
	}
	if episodeLookup {
		query.Set("season", strconv.Itoa(season))
		query.Set("episode", strconv.Itoa(episode))
	}
	if durationMs > 0 {
		query.Set("duration_ms", strconv.FormatInt(durationMs, 10))
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.introBase+"/media?"+query.Encode(), nil)
	request.Header.Set("Accept", "application/json")
	if s.introKey != "" {
		request.Header.Set("Authorization", "Bearer "+s.introKey)
	}
	body, found, err := s.do(request)
	if err != nil || !found {
		return nil, false, err
	}
	result, err := normalizeTheIntroDB(body)
	return result, len(result) > 0, err
}

func (s *Service) fetchSkipMeDB(ctx context.Context, ids ExternalIDs, season, episode int, durationMs int64) ([]Candidate, bool, error) {
	type lookup struct {
		TMDB     int    `json:"tmdb_id,omitempty"`
		IMDB     string `json:"imdb_id,omitempty"`
		TVDB     int    `json:"tvdb_id,omitempty"`
		AniList  int    `json:"anilist_id,omitempty"`
		Season   *int   `json:"season,omitempty"`
		Episode  *int   `json:"episode,omitempty"`
		Duration int64  `json:"duration_ms"`
	}
	item := lookup{TMDB: ids.TMDB, IMDB: ids.IMDB, TVDB: ids.TVDB, AniList: ids.AniList, Duration: durationMs}
	if season >= 0 && episode >= 0 {
		item.Season, item.Episode = &season, &episode
	}
	body, _ := json.Marshal([]lookup{item})
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.skipmeBase+"/v1/movies", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "SkipMe.db/0.0")
	responseBody, found, err := s.do(request)
	if err != nil || !found {
		return nil, false, err
	}
	result, err := normalizeSkipMeDB(responseBody)
	return result, len(result) > 0, err
}

func (s *Service) fetchAniSkip(ctx context.Context, malID, episode int, durationMs int64) ([]Candidate, bool, error) {
	query := url.Values{}
	for _, value := range []string{"op", "ed", "mixed-op", "mixed-ed", "recap"} {
		query.Add("types[]", value)
	}
	query.Set("episodeLength", strconv.FormatFloat(float64(durationMs)/1000, 'f', 3, 64))
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/v2/skip-times/%d/%d?%s", s.aniskipBase, malID, episode, query.Encode()), nil)
	body, found, err := s.do(request)
	if err != nil || !found {
		return nil, false, err
	}
	result, err := normalizeAniSkip(body)
	return result, len(result) > 0, err
}

func (s *Service) do(request *http.Request) ([]byte, bool, error) {
	response, err := s.http.Do(request)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = response.Body.Close() }()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if readErr != nil {
		return nil, false, readErr
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return body, true, nil
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 && response.StatusCode != http.StatusTooManyRequests {
		return nil, false, nil
	}
	message := strings.TrimSpace(string(body))
	if len(message) > 512 {
		message = message[:512] + "…"
	}
	return nil, false, &UpstreamError{Status: response.StatusCode, Body: message}
}

type UpstreamError struct {
	Status int
	Body   string
}

func (e *UpstreamError) Error() string {
	// Keep the raw body available to an in-process debugger without copying
	// provider-controlled content into River errors or application logs.
	return fmt.Sprintf("community segment upstream returned %d", e.Status)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimRight(value, "/")
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
