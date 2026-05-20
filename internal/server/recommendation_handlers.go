package server

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

func handleListTopRecommendations(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		limit := int32(20)
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.ParseInt(l, 10, 32); err == nil && n > 0 && n <= 50 {
				limit = int32(n)
			}
		}

		q := sqlc.New(app.DB)
		recs, err := q.ListTopRecommendations(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		type recItem struct {
			TmdbID          int32   `json:"tmdb_id"`
			Title           string  `json:"title"`
			PosterPath      string  `json:"poster_path"`
			MediaType       string  `json:"media_type"`
			VoteAverage     any     `json:"vote_average"`
			ReleaseDate     string  `json:"release_date"`
			LocalMediaID    *int64  `json:"local_media_item_id,omitempty"`
			LocalSlug       *string `json:"local_slug,omitempty"`
			LocalPosterPath *string `json:"local_poster_path,omitempty"`
			SourceCount     int32   `json:"source_count"`
		}

		items := make([]recItem, len(recs))
		for i, r := range recs {
			items[i] = recItem{
				TmdbID:      r.RecommendedTmdbID,
				Title:       r.Title,
				PosterPath:  r.PosterPath,
				MediaType:   r.MediaType,
				VoteAverage: r.VoteAverage,
				ReleaseDate: r.ReleaseDate,
				SourceCount: r.SourceCount,
			}
			if r.LocalMediaItemID.Valid {
				items[i].LocalMediaID = &r.LocalMediaItemID.Int64
			}
			if r.LocalSlug.Valid {
				items[i].LocalSlug = &r.LocalSlug.String
			}
			if r.LocalPosterPath.Valid {
				items[i].LocalPosterPath = &r.LocalPosterPath.String
			}
		}

		writeJSON(w, http.StatusOK, items)
	}
}

func handleTMDBImageProxy(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.PathValue("path")
		if path == "" || !strings.HasPrefix(path, "/") {
			writeError(w, http.StatusBadRequest, "invalid path")
			return
		}

		size := r.URL.Query().Get("size")
		if size == "" {
			size = "w342"
		}

		url := fmt.Sprintf("https://image.tmdb.org/t/p/%s%s", size, path)

		resp, err := http.Get(url)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to fetch image")
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			writeError(w, resp.StatusCode, "image not found")
			return
		}

		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.Header().Set("Cache-Control", "public, max-age=604800")
		io.Copy(w, resp.Body)
	}
}
