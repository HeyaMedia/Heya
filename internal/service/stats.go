package service

import (
	"context"
	"time"

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

	stats.MissingCount = a.missingCountCached(ctx)

	pending, running := a.QueueCounts(ctx)
	stats.QueuePending = pending
	stats.QueueRunning = running

	return stats
}

// missingCountCached returns the dashboard missing_count through a short TTL
// cache — the underlying anti-joins cost ~750ms at scale and the value only
// changes on scan/cleanup, so per-render recomputation is wasted work.
func (a *App) missingCountCached(ctx context.Context) int {
	a.missingCountMu.Lock()
	defer a.missingCountMu.Unlock()
	if !a.missingCountAt.IsZero() && time.Since(a.missingCountAt) < 5*time.Minute {
		return a.missingCount
	}

	// Missing = user-facing units with no file left on disk. For music the
	// media_item is the artist, which stays present while it has *any* live
	// file — so missing albums (every track's file gone) and orphan tracks
	// (gone, but their album still has live tracks) must be counted too, or
	// the count reads 0 and the cleanup button greys out. See
	// CleanupMissingMedia for the matching deletes.
	//
	// The music buckets drive from the ~dozens of soft-deleted rows (via the
	// idx_library_files_deleted partial index) instead of joining all of
	// library_files: track_files.library_file_id is NOT NULL + FK CASCADE, so
	// "file is live" ≡ "file is not in the soft-deleted set". The old
	// join-everything shape wasn't just slow (~2.3s) — under default
	// parallelism its hash join overflowed the k8s pod's /dev/shm and the
	// query ERRORED, silently zeroing the count. NOT EXISTS (not NOT IN):
	// a NULL in the anti-join side would silently zero a bucket again.
	var count int
	err := a.db.QueryRow(ctx, `
		WITH live_albums AS MATERIALIZED (
		  SELECT DISTINCT t.album_id
		  FROM track_files tf JOIN tracks t ON t.id = tf.track_id
		  WHERE NOT EXISTS (SELECT 1 FROM library_files d
		                    WHERE d.id = tf.library_file_id AND d.deleted_at IS NOT NULL)
		),
		live_tracks AS MATERIALIZED (
		  SELECT DISTINCT tf.track_id
		  FROM track_files tf
		  WHERE NOT EXISTS (SELECT 1 FROM library_files d
		                    WHERE d.id = tf.library_file_id AND d.deleted_at IS NOT NULL)
		)
		SELECT
		  (SELECT count(*) FROM media_items mi WHERE NOT EXISTS (
		      SELECT 1 FROM library_files lf
		      WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL))
		+ (SELECT count(*) FROM albums a
		   WHERE NOT EXISTS (SELECT 1 FROM live_albums la WHERE la.album_id = a.id))
		+ (SELECT count(*) FROM tracks t
		   WHERE NOT EXISTS (SELECT 1 FROM live_tracks lt WHERE lt.track_id = t.id)
		     AND EXISTS (SELECT 1 FROM live_albums la WHERE la.album_id = t.album_id))
	`).Scan(&count)
	if err != nil {
		log.Warn().Err(err).Msg("dashboard missing_count query failed")
		return a.missingCount // stale beats silently-zero
	}

	a.missingCount = count
	a.missingCountAt = time.Now()
	return count
}

// ListMissingMedia returns the user-facing units with no file left on disk:
// media_items (movies/tv/books, plus fully-gone music artists) and missing
// albums (every track's file removed but the artist still has other live
// content). Album rows carry media_type "album" so the FE can key/route them
// apart from media_items, whose id space overlaps. Orphan tracks inside
// otherwise-present albums are cleaned but not listed individually.
func (a *App) ListMissingMedia(ctx context.Context) ([]MissingMediaItem, error) {
	// The albums arm drives from the soft-deleted set like missingCountCached
	// (same live_albums shape — keep the two consistent so the dashboard count
	// and this list never diverge). The old join-everything form hash-joined
	// all of library_files (~1.3s, and erroring on /dev/shm under default
	// parallelism).
	rows, err := a.db.Query(ctx, `
		WITH live_albums AS MATERIALIZED (
		  SELECT DISTINCT t.album_id
		  FROM track_files tf JOIN tracks t ON t.id = tf.track_id
		  WHERE NOT EXISTS (SELECT 1 FROM library_files d
		                    WHERE d.id = tf.library_file_id AND d.deleted_at IS NOT NULL)
		)
		SELECT mi.id, mi.title, mi.year, mi.media_type::text, mi.poster_path, mi.slug
		FROM media_items mi
		WHERE NOT EXISTS (
			SELECT 1 FROM library_files lf
			WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL
		)
		UNION ALL
		SELECT a.id, a.title, a.year, 'album', '', a.slug
		FROM albums a
		WHERE NOT EXISTS (
			SELECT 1 FROM live_albums la WHERE la.album_id = a.id
		)
		ORDER BY 2
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]MissingMediaItem, 0)
	for rows.Next() {
		var m MissingMediaItem
		rows.Scan(&m.ID, &m.Title, &m.Year, &m.MediaType, &m.PosterPath, &m.Slug)
		items = append(items, m)
	}

	return items, nil
}

// CleanupMissingMedia removes everything with no file left on disk, in one
// transaction, and returns the total rows removed:
//
//  1. music tracks whose every file is gone (no live library_file via
//     track_files),
//  2. albums left with no tracks as a result, and
//  3. media_items (movies/tv/books and fully-gone music artists) with no live
//     library_file.
//
// The track/album passes are what the old media_item-only version missed:
// library_files.media_item_id points at the *artist* media_item, so a
// partially-present artist keeps its media_item while individual albums and
// tracks rot — pass 3 alone would never reach them. ComputeStats' missing_count
// counts the same three buckets.
func (a *App) CleanupMissingMedia(ctx context.Context) (int, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	total := 0

	// 1. Tracks whose every file is gone. Cascades track_files, facets,
	//    play_events, playlist entries, and ratings off the track.
	ct, err := tx.Exec(ctx, `
		DELETE FROM tracks t
		WHERE NOT EXISTS (
			SELECT 1 FROM track_files tf
			JOIN library_files lf ON lf.id = tf.library_file_id
			WHERE tf.track_id = t.id AND lf.deleted_at IS NULL
		)`)
	if err != nil {
		return 0, err
	}
	total += int(ct.RowsAffected())

	// 2. Albums left with no tracks. Cascades album facets and ratings.
	ca, err := tx.Exec(ctx, `
		DELETE FROM albums a
		WHERE NOT EXISTS (SELECT 1 FROM tracks t WHERE t.album_id = a.id)`)
	if err != nil {
		return 0, err
	}
	total += int(ca.RowsAffected())

	// 3. media_items with no live library_file. library_files.media_item_id
	//    is ON DELETE SET NULL, so the soft-deleted file rows are removed
	//    explicitly; the rest of the graph (movies/tv_series/artists/books/
	//    cast/assets/...) is ON DELETE CASCADE from media_items.
	rows, err := tx.Query(ctx, `
		SELECT mi.id FROM media_items mi
		WHERE NOT EXISTS (
			SELECT 1 FROM library_files lf
			WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL
		)
	`)
	if err != nil {
		return 0, err
	}

	var ids []int64
	for rows.Next() {
		var id int64
		if scanErr := rows.Scan(&id); scanErr != nil {
			rows.Close()
			return 0, scanErr
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	if len(ids) > 0 {
		if _, err := tx.Exec(ctx, "DELETE FROM library_files WHERE media_item_id = ANY($1)", ids); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM media_items WHERE id = ANY($1)", ids); err != nil {
			return 0, err
		}
		total += len(ids)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}

	log.Info().Int("count", total).Msg("cleaned up missing media")
	return total, nil
}
