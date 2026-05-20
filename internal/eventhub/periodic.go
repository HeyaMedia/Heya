package eventhub

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ScanEnqueuer func(ctx context.Context, libraryID int64)

func (h *Hub) StartPeriodicEmitters(ctx context.Context, db *pgxpool.Pool) {
	go h.activityTicker(ctx, db)
	go h.statsTicker(ctx, db)
}

func (h *Hub) StartScheduledScans(ctx context.Context, db *pgxpool.Pool, enqueue ScanEnqueuer) {
	go h.scheduledScanTicker(ctx, db, enqueue)
}

func (h *Hub) scheduledScanTicker(ctx context.Context, db *pgxpool.Pool, enqueue ScanEnqueuer) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	check := func() {
		rows, err := db.Query(ctx, `
			SELECT id, scan_interval, updated_at FROM libraries
			WHERE scan_interval IS NOT NULL AND scan_interval > '0'::interval
		`)
		if err != nil {
			return
		}
		defer rows.Close()

		for rows.Next() {
			var id int64
			var interval time.Duration
			var updatedAt time.Time
			var pgInterval struct {
				Microseconds int64
				Valid        bool
			}
			if err := rows.Scan(&id, &pgInterval, &updatedAt); err != nil {
				continue
			}
			if !pgInterval.Valid || pgInterval.Microseconds <= 0 {
				continue
			}
			interval = time.Duration(pgInterval.Microseconds) * time.Microsecond

			if time.Since(updatedAt) >= interval {
				enqueue(ctx, id)
			}
		}
	}

	check()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

func (h *Hub) activityTicker(ctx context.Context, db *pgxpool.Pool) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !h.HasSubscribers() {
				continue
			}

			var pending, running int
			row := db.QueryRow(ctx, "SELECT count(*) FILTER (WHERE state = 'available' OR state = 'retryable'), count(*) FILTER (WHERE state = 'running') FROM river_job")
			if err := row.Scan(&pending, &running); err != nil {
				continue
			}
			h.Emit(EventQueueStatus, QueueStatusPayload{Pending: pending, Running: running})

			rows, err := db.Query(ctx,
				"SELECT id, kind, queue, attempted_at, args::text FROM river_job WHERE state = 'running' ORDER BY attempted_at DESC LIMIT 10")
			if err != nil {
				continue
			}
			jobs := []ActiveJob{}
			for rows.Next() {
				var j ActiveJob
				var startedAt *time.Time
				if err := rows.Scan(&j.ID, &j.Kind, &j.Queue, &startedAt, &j.ArgsJSON); err != nil {
					continue
				}
				if startedAt != nil {
					j.StartedAt = *startedAt
				}
				jobs = append(jobs, j)
			}
			rows.Close()
			h.Emit(EventActiveJobs, ActiveJobsPayload{Jobs: jobs})

			if pending > 0 || running > 0 {
				h.emitScanProgress(ctx, db)
			}
		}
	}
}

func (h *Hub) emitScanProgress(ctx context.Context, db *pgxpool.Pool) {
	rows, err := db.Query(ctx, `
		SELECT l.id, l.name,
			count(*) AS total,
			count(*) FILTER (WHERE lf.status != 'pending') AS processed,
			count(*) FILTER (WHERE lf.status = 'matched') AS matched,
			count(*) FILTER (WHERE lf.status = 'unmatched') AS unmatched,
			count(*) FILTER (WHERE lf.status = 'error') AS errors
		FROM library_files lf
		JOIN libraries l ON l.id = lf.library_id
		WHERE lf.status = 'pending'
		   OR l.id IN (
			   SELECT DISTINCT (rj.args->>'library_id')::bigint
			   FROM river_job rj
			   WHERE rj.state IN ('available', 'retryable', 'running')
			     AND rj.args->>'library_id' IS NOT NULL
		   )
		GROUP BY l.id, l.name
		HAVING count(*) FILTER (WHERE lf.status = 'pending') > 0
		    OR count(*) < count(*) FILTER (WHERE lf.status IN ('matched','unmatched','error')) + count(*) FILTER (WHERE lf.status = 'pending')
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	var libs []LibraryScanProgress
	for rows.Next() {
		var lp LibraryScanProgress
		if err := rows.Scan(&lp.LibraryID, &lp.Name, &lp.Total, &lp.Processed, &lp.Matched, &lp.Unmatched, &lp.Errors); err != nil {
			continue
		}
		if lp.Processed < lp.Total {
			libs = append(libs, lp)
		}
	}
	h.Emit(EventScanProgress, ScanProgressPayload{Libraries: libs})
}

func (h *Hub) statsTicker(ctx context.Context, db *pgxpool.Pool) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !h.HasSubscribers() {
				continue
			}
			s := StatsPayload{MediaCounts: make(map[string]int)}

			db.QueryRow(ctx, "SELECT count(*) FROM libraries").Scan(&s.Libraries)
			for _, mt := range []string{"movie", "tv", "music", "book"} {
				var c int
				if db.QueryRow(ctx, "SELECT count(*) FROM media_items WHERE media_type = $1", mt).Scan(&c) == nil {
					s.MediaCounts[mt] = c
					s.TotalMedia += c
				}
			}
			db.QueryRow(ctx, "SELECT count(*) FROM people").Scan(&s.TotalPeople)
			db.QueryRow(ctx, "SELECT count(*) FROM library_files").Scan(&s.TotalFiles)

			row := db.QueryRow(ctx, "SELECT count(*) FILTER (WHERE state = 'available' OR state = 'retryable'), count(*) FILTER (WHERE state = 'running') FROM river_job")
			row.Scan(&s.QueuePending, &s.QueueRunning)

			h.Emit(EventStatsUpdated, s)
		}
	}
}
