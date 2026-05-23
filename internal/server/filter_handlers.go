package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/service"
)

func handleSearchPeople(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			writeJSON(w, http.StatusOK, []any{})
			return
		}

		limit := int32(10)
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.ParseInt(l, 10, 32); err == nil {
				limit = int32(n)
			}
		}

		results, err := app.SearchPeople(r.Context(), query, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, results)
	}
}

func handleSearchStudios(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			writeJSON(w, http.StatusOK, []any{})
			return
		}

		limit := int32(10)
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.ParseInt(l, 10, 32); err == nil {
				limit = int32(n)
			}
		}

		results, err := app.SearchStudios(r.Context(), query, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, results)
	}
}

func handlePeopleMediaIDs(app *service.App) http.HandlerFunc {
	type req struct {
		PersonIDs []int64 `json:"person_ids"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var body req
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		result, err := app.ListMediaIDsByPeople(r.Context(), body.PersonIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func handleStudioMediaIDs(app *service.App) http.HandlerFunc {
	type req struct {
		CompanyIDs []int64 `json:"company_ids"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var body req
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		ids, err := app.ListMediaIDsByStudio(r.Context(), body.CompanyIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, ids)
	}
}

func handleBrowseCollections(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		collections, err := app.BrowseCollections(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, collections)
	}
}
