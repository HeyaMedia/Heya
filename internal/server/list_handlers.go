package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

type userListView struct {
	ID          int64              `json:"id"`
	UserID      int64              `json:"user_id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	CreatedAt   pgtype.Timestamptz `json:"created_at"`
	UpdatedAt   pgtype.Timestamptz `json:"updated_at"`
	ListType    string             `json:"list_type"`
	FilterJSON  json.RawMessage    `json:"filter_json"`
	MediaType   string             `json:"media_type"`
	Icon        string             `json:"icon"`
	ItemCount   int32              `json:"item_count"`
	Contains    *bool              `json:"contains,omitempty"`
}

func listRowToView(l sqlc.ListUserListsRow) userListView {
	v := userListView{
		ID:          l.ID,
		UserID:      l.UserID,
		Name:        l.Name,
		Description: l.Description,
		CreatedAt:   l.CreatedAt,
		UpdatedAt:   l.UpdatedAt,
		ListType:    l.ListType,
		MediaType:   l.MediaType,
		Icon:        l.Icon,
		ItemCount:   l.ItemCount,
	}
	if len(l.FilterJson) > 0 {
		v.FilterJSON = json.RawMessage(l.FilterJson)
	}
	return v
}

func userListToView(l sqlc.UserList) userListView {
	v := userListView{
		ID:          l.ID,
		UserID:      l.UserID,
		Name:        l.Name,
		Description: l.Description,
		CreatedAt:   l.CreatedAt,
		UpdatedAt:   l.UpdatedAt,
		ListType:    l.ListType,
		MediaType:   l.MediaType,
		Icon:        l.Icon,
	}
	if len(l.FilterJson) > 0 {
		v.FilterJSON = json.RawMessage(l.FilterJson)
	}
	return v
}

func handleListUserLists(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		mediaIDStr := r.URL.Query().Get("media_item_id")
		if mediaIDStr != "" {
			mediaID, _ := strconv.ParseInt(mediaIDStr, 10, 64)
			lists, containingIDs, err := app.ListUserListsWithContaining(r.Context(), user.ID, mediaID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}

			containingMap := make(map[int64]bool, len(containingIDs))
			for _, id := range containingIDs {
				containingMap[id] = true
			}

			views := make([]userListView, len(lists))
			for i, l := range lists {
				views[i] = listRowToView(l)
				c := containingMap[views[i].ID]
				views[i].Contains = &c
			}
			writeJSON(w, http.StatusOK, views)
			return
		}

		lists, err := app.ListUserLists(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		views := make([]userListView, len(lists))
		for i, l := range lists {
			views[i] = listRowToView(l)
		}

		writeJSON(w, http.StatusOK, views)
	}
}

func handleCreateUserList(app *service.App) http.HandlerFunc {
	type createReq struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		ListType    string          `json:"list_type"`
		FilterJSON  json.RawMessage `json:"filter_json"`
		MediaType   string          `json:"media_type"`
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

		list, err := app.CreateUserList(r.Context(), user.ID, req.Name, req.Description, req.ListType, req.MediaType, req.FilterJSON)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, userListToView(list))
	}
}

func handleUpdateUserList(app *service.App) http.HandlerFunc {
	type updateReq struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		FilterJSON  json.RawMessage `json:"filter_json"`
		Icon        string          `json:"icon"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid list id")
			return
		}

		var req updateReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		list, err := app.UpdateUserList(r.Context(), id, req.Name, req.Description, req.Icon, req.FilterJSON)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, userListToView(list))
	}
}

func handleGetUserList(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid list id")
			return
		}

		list, items, err := app.GetUserList(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "list not found")
			return
		}

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

		app.DeleteUserList(r.Context(), id)
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

		item, err := app.AddToList(r.Context(), id, req.MediaItemID)
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

		app.RemoveFromList(r.Context(), listID, mediaID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
	}
}

func handleReorderList(app *service.App) http.HandlerFunc {
	type reorderReq struct {
		Items []service.ReorderItem `json:"items"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid list id")
			return
		}

		var req reorderReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if err := app.ReorderList(r.Context(), id, req.Items); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "reordered"})
	}
}
