package eventhub

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/taskdefs"
)

func (h *Hub) StartPeriodicEmitters(ctx context.Context, db *pgxpool.Pool) {
	go h.activityTicker(ctx, db)
	go h.statsTicker(ctx, db)
	go h.taskProgressTicker(ctx, db)
}

// taskProgressTicker runs every 2s and emits one task.progress event
// per scheduled task — carrying the latest pending+running counts
// from river_job. The per-worker emits (from worker.TaskProgress
// Broadcaster) carry CurrentItem; this carries counts. The FE merges
// both into one per-task state.
//
// When a task has zero pending and zero running, the emit carries
// state="idle" so the FE clears its current_item display for that
// task.
func (h *Hub) taskProgressTicker(ctx context.Context, db *pgxpool.Pool) {
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
			for _, def := range taskdefs.All() {
				var counts queueops.RuntimeCounts
				var err error
				kinds := taskdefs.TaskKinds(def.ID)
				if def.Synthetic {
					counts, err = queueops.CountByKinds(ctx, db, kinds)
				} else {
					counts, err = queueops.CountScheduledTask(ctx, db, def.ID, kinds)
				}
				if err != nil {
					continue
				}
				state := "idle"
				if counts.Pending > 0 || counts.Running > 0 {
					state = "running"
				}
				h.Emit(EventTaskProgress, TaskProgressPayload{
					TaskID:  def.ID,
					State:   state,
					Pending: counts.Pending,
					Running: counts.Running,
				})
			}
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

			counts, err := queueops.CountActiveExcludingKind(ctx, db, "debounce_sweep")
			if err != nil {
				continue
			}
			h.Emit(EventQueueStatus, QueueStatusPayload{Pending: counts.Pending, Running: counts.Running})

			// 50 is enough to cover every queue running at once
			// (~28 distinct queues today, each MaxWorkers=1 except
			// download_image at 4) with headroom. The UI lists them
			// grouped by kind, so a high cap is cheap.
			rows, err := db.Query(ctx, `
				SELECT id, kind, queue, attempted_at, args::text
				FROM river_job
				WHERE state = 'running'
				  AND kind <> 'debounce_sweep'
				ORDER BY attempted_at DESC
				LIMIT 50
			`)
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

			if counts.Pending > 0 || counts.Running > 0 {
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

	libs := make([]LibraryScanProgress, 0)
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

			if counts, err := queueops.CountActive(ctx, db); err == nil {
				s.QueuePending = counts.Pending
				s.QueueRunning = counts.Running
			}

			h.Emit(EventStatsUpdated, s)
		}
	}
}
