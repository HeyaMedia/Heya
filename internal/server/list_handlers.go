package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

func handleListUserLists(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		q := sqlc.New(app.DB)
		lists, err := q.ListUserLists(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		mediaIDStr := r.URL.Query().Get("media_item_id")
		if mediaIDStr != "" {
			mediaID, _ := strconv.ParseInt(mediaIDStr, 10, 64)
			containing, _ := q.ListsContainingMedia(r.Context(), sqlc.ListsContainingMediaParams{
				UserID:      user.ID,
				MediaItemID: mediaID,
			})
			containingMap := make(map[int64]bool)
			for _, c := range containing {
				containingMap[c.ID] = true
			}
			type listWithStatus struct {
				sqlc.ListUserListsRow
				Contains bool `json:"contains"`
			}
			result := make([]listWithStatus, len(lists))
			for i, l := range lists {
				result[i] = listWithStatus{ListUserListsRow: l, Contains: containingMap[l.ID]}
			}
			writeJSON(w, http.StatusOK, result)
			return
		}

		writeJSON(w, http.StatusOK, lists)
	}
}

func handleCreateUserList(app *service.App) http.HandlerFunc {
	type createReq struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var req createReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}

		q := sqlc.New(app.DB)
		list, err := q.CreateUserList(r.Context(), sqlc.CreateUserListParams{
			UserID:      user.ID,
			Name:        req.Name,
			Description: req.Description,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, list)
	}
}

func handleGetUserList(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid list id")
			return
		}

		q := sqlc.New(app.DB)
		list, err := q.GetUserListByID(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "list not found")
			return
		}

		items, _ := q.ListItemsInList(r.Context(), list.ID)

		writeJSON(w, http.StatusOK, map[string]any{
			"list":  list,
			"items": items,
		})
	}
}

func handleDeleteUserList(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid list id")
			return
		}

		q := sqlc.New(app.DB)
		q.DeleteUserList(r.Context(), id)
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

func handleAddToList(app *service.App) http.HandlerFunc {
	type addReq struct {
		MediaItemID int64 `json:"media_item_id"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid list id")
			return
		}

		var req addReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		q := sqlc.New(app.DB)
		item, err := q.AddToList(r.Context(), sqlc.AddToListParams{
			ListID:      id,
			MediaItemID: req.MediaItemID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, item)
	}
}

func handleRemoveFromList(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		listID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid list id")
			return
		}

		mediaID, err := strconv.ParseInt(r.PathValue("media_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		q := sqlc.New(app.DB)
		q.RemoveFromList(r.Context(), sqlc.RemoveFromListParams{
			ListID:      listID,
			MediaItemID: mediaID,
		})
		writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
	}
}
