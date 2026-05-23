package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/service"
)

func handleSearchQuick(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("q"))

		result, err := app.SearchQuick(r.Context(), query)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)
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

		result, err := app.SearchByType(r.Context(), query, typeFilter, limit, offset)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)
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
