package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/service"
)

func handleListGenres(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		genres, err := app.ListGenres(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, genres)
	}
}

func handleGetGenre(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			writeError(w, http.StatusBadRequest, "genre name is required")
			return
		}
		name = strings.ReplaceAll(name, "-", " ")

		limit := parseInt32(r.URL.Query().Get("limit"), 60, 200)
		offset := parseInt32(r.URL.Query().Get("offset"), 0, 0)

		result, err := app.GetGenre(r.Context(), name, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func handleGetKeyword(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			writeError(w, http.StatusBadRequest, "keyword name is required")
			return
		}
		name = strings.ReplaceAll(name, "-", " ")

		limit := parseInt32(r.URL.Query().Get("limit"), 60, 200)
		offset := parseInt32(r.URL.Query().Get("offset"), 0, 0)

		result, err := app.GetKeyword(r.Context(), name, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func handleGetCollection(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid collection id")
			return
		}

		result, err := app.GetCollection(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "collection not found")
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func handleListCollections(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := parseInt32(r.URL.Query().Get("limit"), 60, 200)
		offset := parseInt32(r.URL.Query().Get("offset"), 0, 0)

		result, err := app.ListCollections(r.Context(), limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}
