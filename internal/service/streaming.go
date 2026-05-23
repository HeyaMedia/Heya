package service

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// GetLibraryFile fetches a single library file by ID.
// Used by all streaming, subtitle, and trickplay handlers.
func (a *App) GetLibraryFile(ctx context.Context, fileID int64) (sqlc.LibraryFile, error) {
	q := sqlc.New(a.db)
	return q.GetLibraryFileByID(ctx, fileID)
}

// GetLibraryFileWithLibrary fetches a library file and its parent library.
// Handlers that need the library for path resolution use this.
func (a *App) GetLibraryFileWithLibrary(ctx context.Context, fileID int64) (sqlc.LibraryFile, sqlc.Library, error) {
	q := sqlc.New(a.db)

	file, err := q.GetLibraryFileByID(ctx, fileID)
	if err != nil {
		return sqlc.LibraryFile{}, sqlc.Library{}, fmt.Errorf("library file %d: %w", fileID, err)
	}

	lib, err := q.GetLibraryByID(ctx, file.LibraryID)
	if err != nil {
		return sqlc.LibraryFile{}, sqlc.Library{}, fmt.Errorf("library %d: %w", file.LibraryID, err)
	}

	return file, lib, nil
}

// GetMediaExtra fetches a single media extra by ID.
// Used by trickplay thumbnail handlers.
func (a *App) GetMediaExtra(ctx context.Context, id int64) (sqlc.MediaExtra, error) {
	q := sqlc.New(a.db)
	return q.GetMediaExtraByID(ctx, id)
}
