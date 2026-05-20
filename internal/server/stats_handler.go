package server

import (
	"net/http"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/rs/zerolog/log"
)

type dashboardStats struct {
	Libraries    int            `json:"libraries"`
	MediaCounts  map[string]int `json:"media_counts"`
	TotalMedia   int            `json:"total_media"`
	TotalPeople  int            `json:"total_people"`
	TotalFiles   int            `json:"total_files"`
	MissingCount int            `json:"missing_count"`
	QueuePending int            `json:"queue_pending"`
	QueueRunning int            `json:"queue_running"`
}

func handleDashboardStats(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		stats := dashboardStats{
			MediaCounts: make(map[string]int),
		}

		libs, err := app.ListLibraries(ctx)
		if err == nil {
			stats.Libraries = len(libs)
		}

		for _, mt := range []string{"movie", "tv", "music", "book"} {
			var count int
			err := app.DB.QueryRow(ctx, "SELECT count(*) FROM media_items WHERE media_type = $1", mt).Scan(&count)
			if err == nil {
				stats.MediaCounts[mt] = count
				stats.TotalMedia += count
			}
		}

		app.DB.QueryRow(ctx, "SELECT count(*) FROM people").Scan(&stats.TotalPeople)
		app.DB.QueryRow(ctx, "SELECT count(*) FROM library_files").Scan(&stats.TotalFiles)

		app.DB.QueryRow(ctx, `
			SELECT count(DISTINCT mi.id) FROM media_items mi
			WHERE NOT EXISTS (
				SELECT 1 FROM library_files lf
				WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL
			)
		`).Scan(&stats.MissingCount)

		pending, running := app.QueueCounts(ctx)
		stats.QueuePending = pending
		stats.QueueRunning = running

		writeJSON(w, http.StatusOK, stats)
	}
}

func handleListMissing(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		q := sqlc.New(app.DB)

		rows, err := app.DB.Query(ctx, `
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
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		defer rows.Close()
		_ = q

		type missingItem struct {
			ID         int64  `json:"id"`
			Title      string `json:"title"`
			Year       string `json:"year"`
			MediaType  string `json:"media_type"`
			PosterPath string `json:"poster_path"`
			Slug       string `json:"slug"`
		}

		var items []missingItem
		for rows.Next() {
			var m missingItem
			rows.Scan(&m.ID, &m.Title, &m.Year, &m.MediaType, &m.PosterPath, &m.Slug)
			items = append(items, m)
		}

		writeJSON(w, http.StatusOK, items)
	}
}

func handleCleanupMissing(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		rows, err := app.DB.Query(ctx, `
			SELECT mi.id FROM media_items mi
			WHERE NOT EXISTS (
				SELECT 1 FROM library_files lf
				WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL
			)
		`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to find missing items")
			return
		}
		defer rows.Close()

		var ids []int64
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			ids = append(ids, id)
		}

		if len(ids) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"deleted": 0})
			return
		}

		for _, id := range ids {
			app.DB.Exec(ctx, "DELETE FROM library_files WHERE media_item_id = $1", id)
			app.DB.Exec(ctx, "DELETE FROM media_items WHERE id = $1", id)
		}

		log.Info().Int("count", len(ids)).Msg("cleaned up missing media items")
		writeJSON(w, http.StatusOK, map[string]any{"deleted": len(ids)})
	}
}
