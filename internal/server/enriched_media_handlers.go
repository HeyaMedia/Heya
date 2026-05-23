package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/service"
)

func handleListEnrichedMedia(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := int32(2000)
		offset := int32(0)
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.ParseInt(l, 10, 32); err == nil {
				limit = int32(n)
			}
		}
		if o := r.URL.Query().Get("offset"); o != "" {
			if n, err := strconv.ParseInt(o, 10, 32); err == nil {
				offset = int32(n)
			}
		}

		mediaType := r.URL.Query().Get("type")

		switch mediaType {
		case "movie":
			views, err := app.ListEnrichedMovies(r.Context(), limit, offset)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, views)

		case "tv":
			views, err := app.ListEnrichedTVSeries(r.Context(), limit, offset)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, views)

		default:
			writeError(w, http.StatusBadRequest, "?type=movie or ?type=tv is required")
		}
	}
}
