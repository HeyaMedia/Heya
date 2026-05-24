package server

import (
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// userListView is the shared output shape used by /api/me/lists handlers in
// me_huma.go. Kept in a small standalone file so it survives further trimming
// of legacy handlers.
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
