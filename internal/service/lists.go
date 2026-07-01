package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// ReorderItem represents a single item's position in a list reorder operation.
type ReorderItem struct {
	MediaItemID int64 `json:"media_item_id"`
	SortOrder   int32 `json:"sort_order"`
}

func (a *App) ListUserLists(ctx context.Context, userID int64) ([]sqlc.ListUserListsRow, error) {
	q := sqlc.New(a.db)
	return q.ListUserLists(ctx, userID)
}

func (a *App) ListUserListsWithContaining(ctx context.Context, userID, mediaItemID int64) ([]sqlc.ListUserListsRow, []int64, error) {
	q := sqlc.New(a.db)

	lists, err := q.ListUserLists(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("listing user lists: %w", err)
	}

	containing, err := q.ListsContainingMedia(ctx, sqlc.ListsContainingMediaParams{
		UserID:      userID,
		MediaItemID: mediaItemID,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("listing containing lists: %w", err)
	}

	ids := make([]int64, len(containing))
	for i, c := range containing {
		ids[i] = c.ID
	}

	return lists, ids, nil
}

func (a *App) CreateUserList(ctx context.Context, userID int64, name, description, listType, mediaType string, filterJSON []byte) (sqlc.UserList, error) {
	if listType == "" {
		listType = "manual"
	}

	q := sqlc.New(a.db)
	return q.CreateUserList(ctx, sqlc.CreateUserListParams{
		UserID:      userID,
		Name:        name,
		Description: description,
		ListType:    listType,
		FilterJson:  filterJSON,
		MediaType:   mediaType,
	})
}

// GetUserList returns a list and its items, scoped to the owner. A list owned by
// another user (or absent) yields ErrNoRows from the ownership-scoped lookup.
func (a *App) GetUserList(ctx context.Context, listID, userID int64) (sqlc.UserList, []sqlc.MediaItem, error) {
	q := sqlc.New(a.db)

	list, err := q.GetUserListByID(ctx, sqlc.GetUserListByIDParams{ID: listID, UserID: userID})
	if err != nil {
		return sqlc.UserList{}, nil, fmt.Errorf("getting list: %w", err)
	}

	items, err := q.ListItemsInList(ctx, sqlc.ListItemsInListParams{ListID: list.ID, UserID: userID})
	if err != nil {
		return sqlc.UserList{}, nil, fmt.Errorf("listing items in list: %w", err)
	}

	return list, items, nil
}

func (a *App) UpdateUserList(ctx context.Context, listID, userID int64, name, description, icon string, filterJSON []byte) (sqlc.UserList, error) {
	q := sqlc.New(a.db)
	return q.UpdateUserList(ctx, sqlc.UpdateUserListParams{
		ID:          listID,
		Name:        name,
		Description: description,
		FilterJson:  filterJSON,
		Icon:        icon,
		UserID:      userID,
	})
}

func (a *App) DeleteUserList(ctx context.Context, listID, userID int64) error {
	q := sqlc.New(a.db)
	return q.DeleteUserList(ctx, sqlc.DeleteUserListParams{ID: listID, UserID: userID})
}

func (a *App) AddToList(ctx context.Context, listID, mediaItemID, userID int64) (sqlc.UserListItem, error) {
	q := sqlc.New(a.db)
	item, err := q.AddToList(ctx, sqlc.AddToListParams{
		ListID:      listID,
		MediaItemID: mediaItemID,
		UserID:      userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Ownership guard didn't match, or the item is already in the list —
		// either way there's nothing to add and no data was changed.
		return sqlc.UserListItem{}, nil
	}
	return item, err
}

func (a *App) RemoveFromList(ctx context.Context, listID, mediaItemID, userID int64) error {
	q := sqlc.New(a.db)
	return q.RemoveFromList(ctx, sqlc.RemoveFromListParams{
		ListID:      listID,
		MediaItemID: mediaItemID,
		UserID:      userID,
	})
}

func (a *App) ReorderList(ctx context.Context, listID, userID int64, items []ReorderItem) error {
	q := sqlc.New(a.db)
	for _, item := range items {
		if err := q.ReorderListItem(ctx, sqlc.ReorderListItemParams{
			ListID:      listID,
			MediaItemID: item.MediaItemID,
			SortOrder:   item.SortOrder,
			UserID:      userID,
		}); err != nil {
			return fmt.Errorf("reordering item %d: %w", item.MediaItemID, err)
		}
	}
	return nil
}
