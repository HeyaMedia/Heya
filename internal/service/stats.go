package service

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

	_ = a.db.QueryRow(ctx, "SELECT count(*) FROM libraries").Scan(&stats.Libraries)

	// One grouped pass is substantially cheaper than four independent
	// COUNT(*) index walks as a library grows. These are user-facing catalog
	// counts, so keep them exact; the much larger diagnostic-only tables below
	// use PostgreSQL's planner estimates instead.
	rows, err := a.db.Query(ctx, `SELECT media_type::text, count(*) FROM media_items GROUP BY media_type`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var mediaType string
			var count int
			if rows.Scan(&mediaType, &count) == nil {
				stats.MediaCounts[mediaType] = count
				stats.TotalMedia += count
			}
		}
	}

	stats.TotalPeople = estimatedTableRows(ctx, a.db, "public.people")
	stats.TotalFiles = estimatedTableRows(ctx, a.db, "public.library_files")

	stats.MissingCount = a.missingCountCached(ctx)

	pending, running := a.QueueCounts(ctx)
	stats.QueuePending = pending
	stats.QueueRunning = running

	return stats
}

// estimatedTableRows reads pg_class.reltuples: O(1), maintained by ANALYZE,
// and deliberately approximate. Dashboard no longer displays these two
// diagnostic totals, so a full scan of multi-million-row tables is wasteful.
func estimatedTableRows(ctx context.Context, db *pgxpool.Pool, table string) int {
	var count int64
	if err := db.QueryRow(ctx,
		`SELECT GREATEST(COALESCE((SELECT reltuples::bigint FROM pg_class WHERE oid = to_regclass($1)), 0), 0)`,
		table,
	).Scan(&count); err != nil {
		return 0
	}
	return int(count)
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

	// Missing = user-facing units which had a local file and now have none.
	// Metadata-only catalog rows are not missing. For music the media_item is
	// the artist, which stays present while it has *any* live file — so missing
	// albums (every track's file gone) and orphan tracks (gone, but their album
	// still has live tracks) must be counted too. See CleanupMissingMedia for
	// the matching deletes.
	//
	// track_files is the sole music ownership edge. Drive candidates from the
	// small soft-deleted set, then reject any track which still has a live file.
	// The old join-everything
	// shape wasn't just slow (~2.3s) — under default parallelism its hash join
	// overflowed the k8s pod's /dev/shm and the query ERRORED, silently zeroing
	// the count. NOT EXISTS (not NOT IN): a NULL in the anti-join side would
	// silently zero a bucket again.
	// Serial execution on purpose: parallel workers allocate DSM segments in
	// the postgres pod's /dev/shm (64Mi k8s default), and under concurrent
	// shm pressure even a small resize fails with SQLSTATE 53100 — observed
	// live. A serial plan allocates none, costs ~150ms extra here, and this
	// value sits behind a 5-minute cache. SET LOCAL scopes it to this tx.
	tx, err := a.db.Begin(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("dashboard missing_count begin failed")
		return a.missingCount
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL max_parallel_workers_per_gather = 0"); err != nil {
		log.Warn().Err(err).Msg("dashboard missing_count setup failed")
		return a.missingCount
	}

	var count int
	err = tx.QueryRow(ctx, `
		WITH deleted_files AS MATERIALIZED (
		  SELECT id, media_item_id
		  FROM library_files
		  WHERE deleted_at IS NOT NULL
		),
		candidate_tracks AS MATERIALIZED (
		  SELECT tf.track_id, t.album_id
		  FROM track_files tf
		  JOIN deleted_files d ON d.id = tf.library_file_id
		  JOIN tracks t ON t.id = tf.track_id
		),
		missing_tracks AS MATERIALIZED (
		  SELECT candidate.track_id, candidate.album_id
		  FROM candidate_tracks candidate
		  WHERE NOT EXISTS (
		          SELECT 1
		          FROM track_files tf
		          JOIN library_files lf ON lf.id = tf.library_file_id
		          WHERE tf.track_id = candidate.track_id AND lf.deleted_at IS NULL
		        )
		),
		live_albums AS MATERIALIZED (
		  SELECT DISTINCT t.album_id
		  FROM track_files tf
		  JOIN tracks t ON t.id = tf.track_id
		  JOIN library_files lf ON lf.id = tf.library_file_id
		  WHERE lf.deleted_at IS NULL
		)
		SELECT
		  (SELECT count(DISTINCT d.media_item_id)
		   FROM deleted_files d
		   WHERE d.media_item_id IS NOT NULL
		     AND NOT EXISTS (
		       SELECT 1 FROM library_files live
		       WHERE live.media_item_id = d.media_item_id AND live.deleted_at IS NULL
		     ))
		+ (SELECT count(DISTINCT missing.album_id)
		   FROM missing_tracks missing
		   WHERE NOT EXISTS (
		     SELECT 1 FROM live_albums live WHERE live.album_id = missing.album_id
		   ))
		+ (SELECT count(*)
		   FROM missing_tracks missing
		   WHERE EXISTS (
		     SELECT 1 FROM live_albums live WHERE live.album_id = missing.album_id
		   ))
	`).Scan(&count)
	if err != nil {
		log.Warn().Err(err).Msg("dashboard missing_count query failed")
		return a.missingCount // stale beats silently-zero
	}
	_ = tx.Commit(ctx)

	a.missingCount = count
	a.missingCountAt = time.Now()
	return count
}

// ListMissingMedia returns the user-facing units which previously had a file
// and now have none: media_items (movies/tv/books, plus fully-gone music
// artists) and missing albums (every local track file removed but the artist
// still has other live content). Metadata-only catalog rows are excluded.
// Album rows carry media_type "album" so the FE can key/route them apart from
// media_items, whose id space overlaps. Orphan tracks inside otherwise-present
// albums are cleaned but not listed individually.
func (a *App) ListMissingMedia(ctx context.Context) ([]MissingMediaItem, error) {
	// Keep the candidate/live-link CTEs aligned with missingCountCached and
	// CleanupMissingMedia so the dashboard, list, and delete always agree.
	// Serial execution for the same reason as missingCountCached: parallel DSM
	// segments intermittently fail against the pod's tiny /dev/shm.
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL max_parallel_workers_per_gather = 0"); err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
		WITH deleted_files AS MATERIALIZED (
		  SELECT id, media_item_id
		  FROM library_files
		  WHERE deleted_at IS NOT NULL
		),
		candidate_tracks AS MATERIALIZED (
		  SELECT tf.track_id, t.album_id
		  FROM track_files tf
		  JOIN deleted_files d ON d.id = tf.library_file_id
		  JOIN tracks t ON t.id = tf.track_id
		),
		missing_tracks AS MATERIALIZED (
		  SELECT candidate.track_id, candidate.album_id
		  FROM candidate_tracks candidate
		  WHERE NOT EXISTS (
		          SELECT 1
		          FROM track_files tf
		          JOIN library_files lf ON lf.id = tf.library_file_id
		          WHERE tf.track_id = candidate.track_id AND lf.deleted_at IS NULL
		        )
		),
		live_albums AS MATERIALIZED (
		  SELECT DISTINCT t.album_id
		  FROM track_files tf
		  JOIN tracks t ON t.id = tf.track_id
		  JOIN library_files lf ON lf.id = tf.library_file_id
		  WHERE lf.deleted_at IS NULL
		),
		missing_albums AS MATERIALIZED (
		  SELECT DISTINCT missing.album_id
		  FROM missing_tracks missing
		  WHERE NOT EXISTS (
		    SELECT 1 FROM live_albums live WHERE live.album_id = missing.album_id
		  )
		)
		SELECT mi.id, mi.title, mi.year, mi.media_type::text, mi.poster_path, mi.slug
		FROM media_item_cards mi
		WHERE EXISTS (
			SELECT 1 FROM deleted_files deleted WHERE deleted.media_item_id = mi.id
		)
		AND NOT EXISTS (
			SELECT 1 FROM library_files lf
			WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL
		)
		UNION ALL
		SELECT a.id, a.title, a.year, 'album', '', a.slug
		FROM albums a
		JOIN missing_albums missing ON missing.album_id = a.id
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
		if err := rows.Scan(&m.ID, &m.Title, &m.Year, &m.MediaType, &m.PosterPath, &m.Slug); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return items, nil
}

// CleanupMissingMedia removes everything with no file left on disk, in one
// transaction, and returns the total rows removed:
//
//  1. file-backed music tracks whose every canonical file is gone,
//  2. affected albums left with no tracks as a result, and
//  3. previously file-backed media_items (movies/tv/books and fully-gone
//     music artists) with no live library_file.
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

	// Same /dev/shm insurance as missingCountCached — the anti-join DELETE
	// scans can plan parallel workers too.
	if _, err := tx.Exec(ctx, "SET LOCAL max_parallel_workers_per_gather = 0"); err != nil {
		return 0, err
	}

	total := 0

	// 1. Tracks which had a local file and now have no live link. Starting
	//    from deleted files excludes metadata-only catalog rows. Deleting a
	//    track cascades track_files, facets, play events, playlist entries,
	//    and ratings. Retain the affected album IDs so step 2 never sweeps an
	//    unrelated metadata-only empty album.
	trackRows, err := tx.Query(ctx, `
		WITH deleted_files AS MATERIALIZED (
		  SELECT id FROM library_files WHERE deleted_at IS NOT NULL
		),
		candidate_tracks AS MATERIALIZED (
		  SELECT tf.track_id, t.album_id
		  FROM track_files tf
		  JOIN deleted_files d ON d.id = tf.library_file_id
		  JOIN tracks t ON t.id = tf.track_id
		),
		missing_tracks AS MATERIALIZED (
		  SELECT candidate.track_id, candidate.album_id
		  FROM candidate_tracks candidate
		  WHERE NOT EXISTS (
		          SELECT 1
		          FROM track_files tf
		          JOIN library_files lf ON lf.id = tf.library_file_id
		          WHERE tf.track_id = candidate.track_id AND lf.deleted_at IS NULL
		        )
		)
		DELETE FROM tracks target
		USING missing_tracks missing
		WHERE target.id = missing.track_id
		RETURNING target.album_id
	`)
	if err != nil {
		return 0, err
	}
	affectedAlbumSet := make(map[int64]struct{})
	deletedTracks := 0
	for trackRows.Next() {
		var albumID int64
		if scanErr := trackRows.Scan(&albumID); scanErr != nil {
			trackRows.Close()
			return 0, scanErr
		}
		affectedAlbumSet[albumID] = struct{}{}
		deletedTracks++
	}
	trackRows.Close()
	if err := trackRows.Err(); err != nil {
		return 0, err
	}
	total += deletedTracks

	// 2. Only albums touched above may be removed. This is the guard that
	//    preserves metadata-only catalog albums which never represented a
	//    local file.
	if len(affectedAlbumSet) > 0 {
		affectedAlbumIDs := make([]int64, 0, len(affectedAlbumSet))
		for albumID := range affectedAlbumSet {
			affectedAlbumIDs = append(affectedAlbumIDs, albumID)
		}
		ca, err := tx.Exec(ctx, `
			DELETE FROM albums a
			WHERE a.id = ANY($1::bigint[])
			  AND NOT EXISTS (SELECT 1 FROM tracks t WHERE t.album_id = a.id)`, affectedAlbumIDs)
		if err != nil {
			return 0, err
		}
		total += int(ca.RowsAffected())
	}

	// 3. Previously file-backed media_items with no live library_file.
	//    library_files.media_item_id is ON DELETE SET NULL, so the soft-deleted
	//    file rows are removed explicitly; the rest of the graph
	//    (movies/tv_series/artists/books/cast/assets/...) is ON DELETE CASCADE
	//    from media_items. The EXISTS clause preserves metadata-only rows.
	rows, err := tx.Query(ctx, `
		SELECT mi.id FROM media_item_cards mi
		WHERE EXISTS (
			SELECT 1 FROM library_files deleted
			WHERE deleted.media_item_id = mi.id AND deleted.deleted_at IS NOT NULL
		)
		AND NOT EXISTS (
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

	// Cleanup just changed the answer — drop the cached missing_count so the
	// dashboard's next render recomputes instead of serving the pre-cleanup
	// value for up to the TTL (the FE refetches stats right after cleanup).
	a.missingCountMu.Lock()
	a.missingCountAt = time.Time{}
	a.missingCountMu.Unlock()

	log.Info().Int("count", total).Msg("cleaned up missing media")
	return total, nil
}
