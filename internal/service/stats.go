package service

import (
	"context"

	"github.com/rs/zerolog/log"
)

// DashboardStats holds aggregate counts for the dashboard overview.
type DashboardStats struct {
	Libraries    int            `json:"libraries"`
	MediaCounts  map[string]int `json:"media_counts"`
	TotalMedia   int            `json:"total_media"`
	TotalPeople  int            `json:"total_people"`
	TotalFiles   int            `json:"total_files"`
	MissingCount int            `json:"missing_count"`
	QueuePending int            `json:"queue_pending"`
	QueueRunning int            `json:"queue_running"`
}

// MissingMediaItem represents a media item with no active library files.
type MissingMediaItem struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Year       string `json:"year"`
	MediaType  string `json:"media_type"`
	PosterPath string `json:"poster_path"`
	Slug       string `json:"slug"`
}

// GetDashboardStats collects aggregate counts for the dashboard.
func (a *App) GetDashboardStats(ctx context.Context) DashboardStats {
	stats := DashboardStats{
		MediaCounts: make(map[string]int),
	}

	libs, err := a.ListLibraries(ctx)
	if err == nil {
		stats.Libraries = len(libs)
	}

	for _, mt := range []string{"movie", "tv", "music", "book"} {
		var count int
		err := a.db.QueryRow(ctx, "SELECT count(*) FROM media_items WHERE media_type = $1", mt).Scan(&count)
		if err == nil {
			stats.MediaCounts[mt] = count
			stats.TotalMedia += count
		}
	}

	a.db.QueryRow(ctx, "SELECT count(*) FROM people").Scan(&stats.TotalPeople)
	a.db.QueryRow(ctx, "SELECT count(*) FROM library_files").Scan(&stats.TotalFiles)

	a.db.QueryRow(ctx, `
		SELECT count(DISTINCT mi.id) FROM media_items mi
		WHERE NOT EXISTS (
			SELECT 1 FROM library_files lf
			WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL
		)
	`).Scan(&stats.MissingCount)

	pending, running := a.QueueCounts(ctx)
	stats.QueuePending = pending
	stats.QueueRunning = running

	return stats
}

// ListMissingMedia returns media items that have no active library files.
func (a *App) ListMissingMedia(ctx context.Context) ([]MissingMediaItem, error) {
	rows, err := a.db.Query(ctx, `
		SELECT mi.id, mi.title, mi.year, mi.media_type, mi.poster_path, mi.slug
		FROM media_items mi
		WHERE NOT EXISTS (
			SELECT 1 FROM library_files lf
			WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL
		)
		ORDER BY mi.title
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []MissingMediaItem
	for rows.Next() {
		var m MissingMediaItem
		rows.Scan(&m.ID, &m.Title, &m.Year, &m.MediaType, &m.PosterPath, &m.Slug)
		items = append(items, m)
	}

	return items, nil
}

// CleanupMissingMedia deletes media items that have no active library files.
// Returns the number of deleted items.
func (a *App) CleanupMissingMedia(ctx context.Context) (int, error) {
	rows, err := a.db.Query(ctx, `
		SELECT mi.id FROM media_items mi
		WHERE NOT EXISTS (
			SELECT 1 FROM library_files lf
			WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL
		)
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		return 0, nil
	}

	for _, id := range ids {
		a.db.Exec(ctx, "DELETE FROM library_files WHERE media_item_id = $1", id)
		a.db.Exec(ctx, "DELETE FROM media_items WHERE id = $1", id)
	}

	log.Info().Int("count", len(ids)).Msg("cleaned up missing media items")
	return len(ids), nil
}
