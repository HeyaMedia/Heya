package service

import (
	"context"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// GetLibraryFile fetches a single library file by ID.
// Used by all streaming, subtitle, and trickplay handlers.
func (a *App) GetLibraryFile(ctx context.Context, fileID int64) (sqlc.LibraryFile, error) {
	q := sqlc.New(a.db)
	return q.GetLibraryFileByID(ctx, fileID)
}

// GetMediaExtra fetches a single media extra by ID.
// Used by trickplay thumbnail handlers.
func (a *App) GetMediaExtra(ctx context.Context, id int64) (sqlc.MediaExtra, error) {
	q := sqlc.New(a.db)
	return q.GetMediaExtraByID(ctx, id)
}
