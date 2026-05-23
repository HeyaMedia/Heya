package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

func handleListMedia(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := int32(50)
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
		if mediaType == "" {
			writeError(w, http.StatusBadRequest, "?type= parameter is required")
			return
		}

		views, err := app.ListMedia(r.Context(), sqlc.MediaType(mediaType), limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, views)
	}
}

func handleGetMedia(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idOrSlug := r.PathValue("id")

		result, err := app.GetMediaDetail(r.Context(), idOrSlug)
		if err != nil {
			writeError(w, http.StatusNotFound, "media item not found")
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func handleGetPerson(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idOrSlug := r.PathValue("id")

		result, err := app.GetPerson(r.Context(), idOrSlug)
		if err != nil {
			writeError(w, http.StatusNotFound, "person not found")
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func handleRefreshMedia(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		if err := app.RefreshMediaItem(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
	}
}

func handleResolveMatch(app *service.App) http.HandlerFunc {
	type resolveReq struct {
		CandidateID int64 `json:"candidate_id"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}

		var req resolveReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if err := app.ResolveMatch(r.Context(), id, req.CandidateID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "matched"})
	}
}

func handleListUnmatched(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid library id")
			return
		}

		result, err := app.ListUnmatched(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}
