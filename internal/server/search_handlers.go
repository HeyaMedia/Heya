package server

import (
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

// Bucket caps for the quick (navbar dropdown) endpoint. Kept tight so a
// generic name ("Peter") can't drown other entity types in the results.
const (
	quickPerTypeLimit = 6
	quickPeopleLimit  = 6
	quickAlbumsLimit  = 6
	quickTracksLimit  = 6
	quickColLimit     = 4
)

type bucket struct {
	Items any   `json:"items"`
	Total int64 `json:"total"`
}

type quickSearchResponse struct {
	Query   string            `json:"query"`
	Buckets map[string]bucket `json:"buckets"`
}

func handleSearchQuick(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			writeJSON(w, http.StatusOK, quickSearchResponse{Query: "", Buckets: map[string]bucket{}})
			return
		}

		q := sqlc.New(app.DB)
		ctx := r.Context()

		var (
			wg sync.WaitGroup
			mu sync.Mutex
			b  = make(map[string]bucket, 8)
		)

		set := func(key string, items any, total int64) {
			mu.Lock()
			b[key] = bucket{Items: items, Total: total}
			mu.Unlock()
		}

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

		writeJSON(w, http.StatusOK, quickSearchResponse{Query: query, Buckets: b})
	}
}

// handleSearchAll powers /search?q=...&type=...
// type is optional and selects which bucket to paginate. Without type, returns
// a wider snapshot of every bucket (used as a fallback when the user lands on
// /search directly without picking a type).
func handleSearchAll(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			writeError(w, http.StatusBadRequest, "?q= parameter is required")
			return
		}
		typeFilter := r.URL.Query().Get("type")
		limit := parseInt32(r.URL.Query().Get("limit"), 60, 200)
		offset := parseInt32(r.URL.Query().Get("offset"), 0, 0)

		q := sqlc.New(app.DB)
		ctx := r.Context()

		switch typeFilter {
		case "":
			// Backward-compatible behaviour: scan media_items across types.
			items, err := q.SearchAllMedia(ctx, sqlc.SearchAllMediaParams{
				Lower: query, Limit: limit, Offset: offset,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, items)
		case "movie", "tv", "music", "book":
			items, err := q.SearchMediaByType(ctx, sqlc.SearchMediaByTypeParams{
				Lower:     query,
				MediaType: sqlc.MediaType(typeFilter),
				Limit:     limit,
				Offset:    offset,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			total, _ := q.SearchMediaByTypeCount(ctx, sqlc.SearchMediaByTypeCountParams{
				Lower: query, MediaType: sqlc.MediaType(typeFilter),
			})
			writeJSON(w, http.StatusOK, bucket{Items: items, Total: total})
		case "people":
			items, err := q.SearchPeople(ctx, sqlc.SearchPeopleParams{
				Lower: query, Limit: limit, Offset: offset,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			total, _ := q.SearchPeopleCount(ctx, query)
			writeJSON(w, http.StatusOK, bucket{Items: items, Total: total})
		case "albums":
			items, err := q.SearchAlbums(ctx, sqlc.SearchAlbumsParams{
				Lower: query, Limit: limit, Offset: offset,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			total, _ := q.SearchAlbumsCount(ctx, query)
			writeJSON(w, http.StatusOK, bucket{Items: items, Total: total})
		case "tracks":
			items, err := q.SearchTracks(ctx, sqlc.SearchTracksParams{
				Lower: query, Limit: limit, Offset: offset,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			total, _ := q.SearchTracksCount(ctx, query)
			writeJSON(w, http.StatusOK, bucket{Items: items, Total: total})
		case "collections":
			items, err := q.SearchCollections(ctx, sqlc.SearchCollectionsParams{
				Lower: query, Limit: limit, Offset: offset,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			total, _ := q.SearchCollectionsCount(ctx, query)
			writeJSON(w, http.StatusOK, bucket{Items: items, Total: total})
		default:
			writeError(w, http.StatusBadRequest, "unknown type: "+typeFilter)
		}
	}
}

func parseInt32(s string, def, max int32) int32 {
	if s == "" {
		return def
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return def
	}
	v := int32(n)
	if max > 0 && v > max {
		return max
	}
	if v < 0 {
		return 0
	}
	return v
}

