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

type MediaExtraFile struct {
	ID            int64  `json:"id"`
	MediaItemID   int64  `json:"media_item_id"`
	ExtraType     string `json:"extra_type"`
	Title         string `json:"title"`
	FilePath      string `json:"file_path"`
	DurationMs    int32  `json:"duration_ms"`
	FileSize      int64  `json:"file_size"`
	ThumbnailPath string `json:"thumbnail_path"`
}

// GetMediaExtra fetches a local extra from the library_file_links model.
func (a *App) GetMediaExtra(ctx context.Context, id int64) (MediaExtraFile, error) {
	q := sqlc.New(a.db)
	if id <= 0 {
		return MediaExtraFile{}, fmt.Errorf("invalid extra id")
	}
	row, err := q.GetMediaExtraLinkByID(ctx, id)
	if err != nil {
		return MediaExtraFile{}, err
	}
	return MediaExtraFile{
		ID:            row.ID,
		MediaItemID:   row.MediaItemID,
		ExtraType:     row.ExtraType,
		Title:         row.Title,
		FilePath:      row.FilePath,
		DurationMs:    row.DurationMs,
		FileSize:      row.FileSize,
		ThumbnailPath: row.ThumbnailPath,
	}, nil
}
