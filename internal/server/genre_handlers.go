package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

func handleListGenres(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := sqlc.New(app.DB)
		genres, err := q.ListAllGenres(r.Context())
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

		q := sqlc.New(app.DB)
		limit := parseInt32(r.URL.Query().Get("limit"), 60, 200)
		offset := parseInt32(r.URL.Query().Get("offset"), 0, 0)

		items, err := q.ListMediaByGenre(r.Context(), sqlc.ListMediaByGenreParams{
			Column1: name,
			Limit:   limit,
			Offset:  offset,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		total, _ := q.CountMediaByGenre(r.Context(), name)

		writeJSON(w, http.StatusOK, map[string]any{
			"genre": name,
			"items": items,
			"total": total,
		})
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

		q := sqlc.New(app.DB)
		limit := parseInt32(r.URL.Query().Get("limit"), 60, 200)
		offset := parseInt32(r.URL.Query().Get("offset"), 0, 0)

		items, err := q.ListMediaByKeyword(r.Context(), sqlc.ListMediaByKeywordParams{
			Column1: name,
			Limit:   limit,
			Offset:  offset,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		total, _ := q.CountMediaByKeyword(r.Context(), name)

		writeJSON(w, http.StatusOK, map[string]any{
			"keyword": name,
			"items":   items,
			"total":   total,
		})
	}
}

func handleGetCollection(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid collection id")
			return
		}

		q := sqlc.New(app.DB)
		col, err := q.GetCollectionByID(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "collection not found")
			return
		}

		movies, _ := q.ListCollectionMovies(r.Context(), pgtype.Int8{Int64: col.ID, Valid: true})

		writeJSON(w, http.StatusOK, map[string]any{
			"collection": col,
			"movies":     movies,
		})
	}
}

func handleListCollections(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := sqlc.New(app.DB)
		limit := parseInt32(r.URL.Query().Get("limit"), 60, 200)
		offset := parseInt32(r.URL.Query().Get("offset"), 0, 0)

		items, err := q.ListAllCollections(r.Context(), sqlc.ListAllCollectionsParams{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		total, _ := q.CountAllCollections(r.Context())

		writeJSON(w, http.StatusOK, map[string]any{
			"items": items,
			"total": total,
		})
	}
}
