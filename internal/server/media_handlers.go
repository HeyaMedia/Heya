package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

func handleListMedia(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := sqlc.New(app.DB)

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
		if mediaType != "" {
			items, err := q.ListMediaItemsByType(r.Context(), sqlc.ListMediaItemsByTypeParams{
				MediaType: sqlc.MediaType(mediaType),
				Limit:     limit,
				Offset:    offset,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, items)
			return
		}

		writeError(w, http.StatusBadRequest, "?type= parameter is required")
	}
}

func handleGetMedia(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		q := sqlc.New(app.DB)
		item, err := q.GetMediaItemByID(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "media item not found")
			return
		}

		result := map[string]any{"media_item": item}

		switch item.MediaType {
		case sqlc.MediaTypeMovie:
			movie, err := q.GetMovieByMediaItemID(r.Context(), id)
			if err == nil {
				result["movie"] = movie
			}
		case sqlc.MediaTypeTv:
			series, err := q.GetTVSeriesByMediaItemID(r.Context(), id)
			if err == nil {
				result["tv_series"] = series
				seasons, _ := q.ListTVSeasonsBySeries(r.Context(), series.ID)
				result["seasons"] = seasons
			}
		case sqlc.MediaTypeMusic:
			artist, err := q.GetArtistByMediaItemID(r.Context(), id)
			if err == nil {
				result["artist"] = artist
				albums, _ := q.ListAlbumsByArtist(r.Context(), artist.ID)
				result["albums"] = albums
			}
		case sqlc.MediaTypeBook:
			book, err := q.GetBookByMediaItemID(r.Context(), id)
			if err == nil {
				result["book"] = book
				if book.AuthorID.Valid {
					author, _ := q.GetAuthorByID(r.Context(), book.AuthorID.Int64)
					result["author"] = author
				}
			}
		}

		assets, _ := q.ListMediaAssets(r.Context(), id)
		if assets != nil {
			result["assets"] = assets
		}

		extras, _ := q.ListMediaExtras(r.Context(), id)
		if extras != nil {
			result["extras"] = extras
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func handleSearchMedia(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			writeError(w, http.StatusBadRequest, "?q= parameter is required")
			return
		}

		q := sqlc.New(app.DB)
		items, err := q.SearchMediaItems(r.Context(), sqlc.SearchMediaItemsParams{
			PlaintoTsquery: query,
			Limit:          50,
			Offset:         0,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, items)
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

		q := sqlc.New(app.DB)
		files, err := q.ListLibraryFilesByStatus(r.Context(), sqlc.ListLibraryFilesByStatusParams{
			LibraryID: id,
			Status:    sqlc.FileStatusUnmatched,
			Limit:     100,
			Offset:    0,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		type unmatchedFile struct {
			File       sqlc.LibraryFile     `json:"file"`
			Candidates []sqlc.MatchCandidate `json:"candidates"`
		}

		var result []unmatchedFile
		for _, f := range files {
			candidates, _ := q.ListMatchCandidatesByFile(r.Context(), f.ID)
			result = append(result, unmatchedFile{File: f, Candidates: candidates})
		}

		writeJSON(w, http.StatusOK, result)
	}
}
