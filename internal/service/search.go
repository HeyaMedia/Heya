package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Bucket caps for the quick (navbar dropdown) search. Kept tight so a
// generic name ("Peter") can't drown other entity types in the results.
const (
	quickPerTypeLimit int32 = 6
	quickPeopleLimit  int32 = 6
	quickAlbumsLimit  int32 = 6
	quickTracksLimit  int32 = 6
	quickColLimit     int32 = 4
)

// SearchBucket holds the paginated items for a single search category.
type SearchBucket struct {
	Items any   `json:"items"`
	Total int64 `json:"total"`
}

// QuickSearchResult is returned by SearchQuick and groups results by entity type.
type QuickSearchResult struct {
	Query   string                  `json:"query"`
	Buckets map[string]SearchBucket `json:"buckets"`
}

// SearchQuick runs parallel searches across movies, tv, music, books, people,
// albums, tracks, and collections with small per-type limits.  It mirrors the
// goroutine + WaitGroup + mutex pattern used by the quick-search HTTP handler.
func (a *App) SearchQuick(ctx context.Context, query string) (QuickSearchResult, error) {
	if query == "" {
		return QuickSearchResult{Query: "", Buckets: map[string]SearchBucket{}}, nil
	}

	q := sqlc.New(a.db)

	var (
		wg sync.WaitGroup
		mu sync.Mutex
		b  = make(map[string]SearchBucket, 8)
	)

	set := func(key string, items any, total int64) {
		mu.Lock()
		b[key] = SearchBucket{Items: items, Total: total}
		mu.Unlock()
	}

	// Helper: search media_items by type.
	runTyped := func(key string, mt sqlc.MediaType, limit int32) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, err := q.SearchMediaByType(ctx, sqlc.SearchMediaByTypeParams{
				Lower:     query,
				MediaType: mt,
				Limit:     limit,
				Offset:    0,
			})
			if err != nil || len(items) == 0 {
				return
			}
			total, _ := q.SearchMediaByTypeCount(ctx, sqlc.SearchMediaByTypeCountParams{
				Lower:     query,
				MediaType: mt,
			})
			set(key, items, total)
		}()
	}

	runTyped("movies", sqlc.MediaTypeMovie, quickPerTypeLimit)
	runTyped("tv", sqlc.MediaTypeTv, quickPerTypeLimit)
	runTyped("music", sqlc.MediaTypeMusic, quickPerTypeLimit)
	runTyped("books", sqlc.MediaTypeBook, quickPerTypeLimit)

	// People
	wg.Add(1)
	go func() {
		defer wg.Done()
		items, err := q.SearchPeople(ctx, sqlc.SearchPeopleParams{
			Lower: query, Limit: quickPeopleLimit, Offset: 0,
		})
		if err != nil || len(items) == 0 {
			return
		}
		total, _ := q.SearchPeopleCount(ctx, query)
		set("people", items, total)
	}()

	// Albums
	wg.Add(1)
	go func() {
		defer wg.Done()
		items, err := q.SearchAlbums(ctx, sqlc.SearchAlbumsParams{
			Lower: query, Limit: quickAlbumsLimit, Offset: 0,
		})
		if err != nil || len(items) == 0 {
			return
		}
		total, _ := q.SearchAlbumsCount(ctx, query)
		set("albums", items, total)
	}()

	// Tracks
	wg.Add(1)
	go func() {
		defer wg.Done()
		items, err := q.SearchTracks(ctx, sqlc.SearchTracksParams{
			Lower: query, Limit: quickTracksLimit, Offset: 0,
		})
		if err != nil || len(items) == 0 {
			return
		}
		total, _ := q.SearchTracksCount(ctx, query)
		set("tracks", items, total)
	}()

	// Collections
	wg.Add(1)
	go func() {
		defer wg.Done()
		items, err := q.SearchCollections(ctx, sqlc.SearchCollectionsParams{
			Lower: query, Limit: quickColLimit, Offset: 0,
		})
		if err != nil || len(items) == 0 {
			return
		}
		total, _ := q.SearchCollectionsCount(ctx, query)
		set("collections", items, total)
	}()

	wg.Wait()

	return QuickSearchResult{Query: query, Buckets: b}, nil
}

// SearchByType searches a single entity type with pagination. Supported
// mediaType values: "movie", "tv", "music", "book", "people", "albums",
// "tracks", "collections", and "" (all media items regardless of type).
func (a *App) SearchByType(ctx context.Context, query string, mediaType string, limit, offset int32) (SearchBucket, error) {
	q := sqlc.New(a.db)

	switch mediaType {
	case "":
		items, err := q.SearchAllMedia(ctx, sqlc.SearchAllMediaParams{
			Lower: query, Limit: limit, Offset: offset,
		})
		if err != nil {
			return SearchBucket{}, err
		}
		return SearchBucket{Items: items, Total: int64(len(items))}, nil

	case "movie", "tv", "music", "book":
		items, err := q.SearchMediaByType(ctx, sqlc.SearchMediaByTypeParams{
			Lower:     query,
			MediaType: sqlc.MediaType(mediaType),
			Limit:     limit,
			Offset:    offset,
		})
		if err != nil {
			return SearchBucket{}, err
		}
		total, _ := q.SearchMediaByTypeCount(ctx, sqlc.SearchMediaByTypeCountParams{
			Lower:     query,
			MediaType: sqlc.MediaType(mediaType),
		})
		return SearchBucket{Items: items, Total: total}, nil

	case "people":
		items, err := q.SearchPeople(ctx, sqlc.SearchPeopleParams{
			Lower: query, Limit: limit, Offset: offset,
		})
		if err != nil {
			return SearchBucket{}, err
		}
		total, _ := q.SearchPeopleCount(ctx, query)
		return SearchBucket{Items: items, Total: total}, nil

	case "albums":
		items, err := q.SearchAlbums(ctx, sqlc.SearchAlbumsParams{
			Lower: query, Limit: limit, Offset: offset,
		})
		if err != nil {
			return SearchBucket{}, err
		}
		total, _ := q.SearchAlbumsCount(ctx, query)
		return SearchBucket{Items: items, Total: total}, nil

	case "tracks":
		items, err := q.SearchTracks(ctx, sqlc.SearchTracksParams{
			Lower: query, Limit: limit, Offset: offset,
		})
		if err != nil {
			return SearchBucket{}, err
		}
		total, _ := q.SearchTracksCount(ctx, query)
		return SearchBucket{Items: items, Total: total}, nil

	case "collections":
		items, err := q.SearchCollections(ctx, sqlc.SearchCollectionsParams{
			Lower: query, Limit: limit, Offset: offset,
		})
		if err != nil {
			return SearchBucket{}, err
		}
		total, _ := q.SearchCollectionsCount(ctx, query)
		return SearchBucket{Items: items, Total: total}, nil

	default:
		return SearchBucket{}, fmt.Errorf("unknown search type: %s", mediaType)
	}
}
