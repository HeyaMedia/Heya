package communitysegments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	animeListsURL      = "https://raw.githubusercontent.com/Fribb/anime-lists/master/anime-list-mini.json"
	animeListsHorizon  = 7 * 24 * time.Hour
	animeListsMaxBytes = 40 << 20
)

type animeListEntry struct {
	AniDBID           int  `json:"anidb_id"`
	MALID             int  `json:"mal_id"`
	AniListID         int  `json:"anilist_id"`
	TVDBID            int  `json:"tvdb_id"`
	SeasonTVDB        *int `json:"season_tvdb,omitempty"`
	EpisodeOffsetTVDB int  `json:"episode_offset_tvdb"`
}

type animeListEntries struct {
	byTVDB  map[int][]animeListEntry
	byAniDB map[int]animeListEntry
}

type animeListsIndex struct {
	db   *pgxpool.Pool
	http *http.Client
	url  string
	now  func() time.Time

	mu        sync.Mutex
	loaded    bool
	loadedAt  time.Time
	all       []animeListEntry
	formatted animeListEntries
}

func newAnimeListsIndex(db *pgxpool.Pool, client *http.Client, sourceURL string) *animeListsIndex {
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	return &animeListsIndex{
		db: db, http: client, url: defaultString(sourceURL, animeListsURL), now: time.Now,
	}
}

func (index *animeListsIndex) entries(ctx context.Context) (animeListEntries, error) {
	index.mu.Lock()
	defer index.mu.Unlock()

	now := index.now()
	if index.loaded && now.Before(index.loadedAt.Add(animeListsHorizon)) {
		return index.formatted, nil
	}

	if !index.loaded {
		cached, fetchedAt, ok, err := index.load(ctx)
		if err != nil {
			return animeListEntries{}, err
		}
		if ok {
			index.install(cached, fetchedAt)
			if now.Before(fetchedAt.Add(animeListsHorizon)) {
				return index.formatted, nil
			}
		}
	}

	entries, err := index.fetch(ctx)
	if err != nil {
		// A stale mapping is safer than dropping AniSkip support during a
		// GitHub outage. Its age remains unchanged, so a later lookup retries.
		if index.loaded {
			return index.formatted, nil
		}
		return animeListEntries{}, err
	}
	index.install(entries, now)
	// Cache persistence is an optimization. A successful mapping download
	// remains usable even if this write loses a race with shutdown.
	_ = index.store(ctx, entries, now)
	return index.formatted, nil
}

func (index *animeListsIndex) install(entries []animeListEntry, fetchedAt time.Time) {
	formatted := animeListEntries{
		byTVDB: make(map[int][]animeListEntry), byAniDB: make(map[int]animeListEntry, len(entries)),
	}
	for _, entry := range entries {
		if entry.TVDBID > 0 {
			formatted.byTVDB[entry.TVDBID] = append(formatted.byTVDB[entry.TVDBID], entry)
		}
		if entry.AniDBID > 0 {
			formatted.byAniDB[entry.AniDBID] = entry
		}
	}
	for tvdbID := range formatted.byTVDB {
		sort.Slice(formatted.byTVDB[tvdbID], func(left, right int) bool {
			return formatted.byTVDB[tvdbID][left].EpisodeOffsetTVDB < formatted.byTVDB[tvdbID][right].EpisodeOffsetTVDB
		})
	}
	index.all, index.formatted = entries, formatted
	index.loaded, index.loadedAt = true, fetchedAt
}

func (index *animeListsIndex) fetch(ctx context.Context) ([]animeListEntry, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, index.url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "heya/1.0 anime-lists-sync")
	response, err := index.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("anime-lists download: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anime-lists download returned %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, animeListsMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read anime-lists download: %w", err)
	}
	if len(raw) > animeListsMaxBytes {
		return nil, fmt.Errorf("anime-lists download exceeds %d bytes", animeListsMaxBytes)
	}

	var wire []struct {
		AniDBID   int `json:"anidb_id"`
		MALID     int `json:"mal_id"`
		AniListID int `json:"anilist_id"`
		TVDBID    int `json:"tvdb_id"`
		Season    struct {
			TVDB *int `json:"tvdb"`
		} `json:"season"`
		EpisodeOffset struct {
			TVDB int `json:"tvdb"`
		} `json:"episode_offset"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("parse anime-lists download: %w", err)
	}
	entries := make([]animeListEntry, 0, len(wire))
	for _, value := range wire {
		entries = append(entries, animeListEntry{
			AniDBID: value.AniDBID, MALID: value.MALID, AniListID: value.AniListID,
			TVDBID: value.TVDBID, SeasonTVDB: value.Season.TVDB,
			EpisodeOffsetTVDB: value.EpisodeOffset.TVDB,
		})
	}
	return entries, nil
}

func (index *animeListsIndex) load(ctx context.Context) ([]animeListEntry, time.Time, bool, error) {
	if index.db == nil {
		return nil, time.Time{}, false, nil
	}
	var raw []byte
	var fetchedAt time.Time
	err := index.db.QueryRow(ctx, `SELECT entries, fetched_at FROM community_segment_anime_map_cache WHERE cache_id=true`).Scan(&raw, &fetchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, time.Time{}, false, nil
	}
	if err != nil {
		return nil, time.Time{}, false, fmt.Errorf("load anime-lists cache: %w", err)
	}
	var entries []animeListEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, time.Time{}, false, fmt.Errorf("decode anime-lists cache: %w", err)
	}
	return entries, fetchedAt, true, nil
}

func (index *animeListsIndex) store(ctx context.Context, entries []animeListEntry, fetchedAt time.Time) error {
	if index.db == nil {
		return nil
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	_, err = index.db.Exec(ctx, `
		INSERT INTO community_segment_anime_map_cache (cache_id, entries, fetched_at)
		VALUES (true, $1, $2)
		ON CONFLICT (cache_id) DO UPDATE
		SET entries=EXCLUDED.entries, fetched_at=EXCLUDED.fetched_at`, raw, fetchedAt)
	return err
}
