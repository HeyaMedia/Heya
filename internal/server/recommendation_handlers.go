package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/auth"
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

		recs, err := app.ListTopRecommendations(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		type recItem struct {
			ExternalIDs     map[string]string `json:"external_ids"`
			Title           string            `json:"title"`
			PosterPath      string            `json:"poster_path"`
			MediaType       string            `json:"media_type"`
			VoteAverage     any               `json:"vote_average"`
			ReleaseDate     string            `json:"release_date"`
			LocalMediaID    *int64            `json:"local_media_item_id,omitempty"`
			LocalSlug       *string           `json:"local_slug,omitempty"`
			LocalPosterPath *string           `json:"local_poster_path,omitempty"`
			SourceCount     int32             `json:"source_count"`
		}

		items := make([]recItem, len(recs))
		for i, r := range recs {
			var extIDs map[string]string
			_ = json.Unmarshal(r.ExternalIds, &extIDs)
			items[i] = recItem{
				ExternalIDs: extIDs,
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
