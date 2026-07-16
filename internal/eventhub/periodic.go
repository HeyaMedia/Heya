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
	// wasScanning lets the scan-progress emit turn off cleanly: while scan
	// jobs are active we emit real numbers; on the first tick after they
	// finish we emit once more (empty) so the FE clears, then stay silent —
	// the progress query isn't free and shouldn't run on an idle box.
	wasScanning := false
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
			// grouped by kind, so a high cap is cheap. library_name is
			// resolved here so the UI never renders a raw "library id N".
			rows, err := db.Query(ctx, `
				SELECT rj.id, rj.kind, rj.queue, rj.attempted_at, rj.args::text,
				       COALESCE(l.name, '') AS library_name
				FROM river_job rj
				LEFT JOIN libraries l ON l.id = NULLIF(rj.args->>'library_id', '')::bigint
				WHERE rj.state = 'running'
				  AND rj.kind <> 'debounce_sweep'
				ORDER BY rj.attempted_at DESC
				LIMIT 50
			`)
			if err != nil {
				continue
			}
			jobs := []ActiveJob{}
			for rows.Next() {
				var j ActiveJob
				var startedAt *time.Time
				if err := rows.Scan(&j.ID, &j.Kind, &j.Queue, &startedAt, &j.ArgsJSON, &j.LibraryName); err != nil {
					continue
				}
				if startedAt != nil {
					j.StartedAt = *startedAt
				}
				jobs = append(jobs, j)
			}
			rows.Close()
			h.Emit(EventActiveJobs, ActiveJobsPayload{Jobs: jobs})

			scanning := scanJobsActive(ctx, db)
			if scanning || wasScanning {
				h.emitScanProgress(ctx, db)
			}
			wasScanning = scanning
		}
	}
}

// scanJobsActive is the cheap gate in front of emitScanProgress: one
// indexed river_job existence probe.
func scanJobsActive(ctx context.Context, db *pgxpool.Pool) bool {
	var active bool
	err := db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM river_job
			WHERE state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
			  AND kind IN ('kickoff_library_scan', 'process_scan', 'search_metadata', 'fetch_metadata', 'apply_metadata')
		)`).Scan(&active)
	return err == nil && active
}

// emitScanProgress reports per-library scan state for libraries with scan
// pipeline work in flight. Presence in the payload = the library is
// scanning (it has an active kickoff/process/search/fetch/apply job).
//
// The progress bar is stateless and depends on nothing deletable:
// library_scan_bursts.units_total is a durable count maintained by the
// enqueue helpers (reset when a unit is enqueued for an idle library,
// incremented otherwise), and processed = total − active, where active
// jobs are the one population River never prunes mid-scan. That holds
// across job-cleaner pruning, Cancel-all, server restarts, subscriber
// downtime, and back-to-back bursts — the writers define burst
// boundaries, not this reader. A discovering kickoff (no units enqueued
// yet) reports 0/0, which the FE renders as 0%.
//
// matched/unmatched/errors stay identity-based as review-UI buckets, each
// identity classified by its LATEST entity row only (chunk-era duplicate
// rows counted in-flight rescans as done). Migration 00014 backs that
// scan with a matching index.
func (h *Hub) emitScanProgress(ctx context.Context, db *pgxpool.Pool) {
	rows, err := db.Query(ctx, `
		WITH active_units AS (
			SELECT NULLIF(rj.args->>'library_id', '')::bigint AS library_id, count(*) AS cnt
			FROM river_job rj
			WHERE rj.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
			  AND rj.kind IN ('process_scan', 'search_metadata', 'fetch_metadata', 'apply_metadata')
			GROUP BY 1
		),
		active_kickoffs AS (
			SELECT DISTINCT NULLIF(rj.args->>'library_id', '')::bigint AS library_id
			FROM river_job rj
			WHERE rj.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
			  AND rj.kind = 'kickoff_library_scan'
		),
		latest AS (
			SELECT DISTINCT ON (se.library_id, se.identity_key)
				se.library_id, se.identity_key, se.status
			FROM scanner_entities se
			WHERE EXISTS (SELECT 1 FROM active_kickoffs WHERE library_id IS NULL)
			   OR se.library_id IN (
					SELECT library_id FROM active_units WHERE library_id IS NOT NULL
					UNION
					SELECT library_id FROM active_kickoffs WHERE library_id IS NOT NULL
			   )
			ORDER BY se.library_id, se.identity_key, se.updated_at DESC, se.id DESC
		),
		buckets AS (
			SELECT latest.library_id,
				count(*) FILTER (WHERE latest.status = 'applied') AS matched,
				count(*) FILTER (WHERE latest.status IN ('needs_review', 'unmatched', 'rejected', 'ignored')) AS unmatched,
				count(*) FILTER (WHERE latest.status IN ('apply_error', 'metadata_error', 'error', 'failed')) AS errors
			FROM latest
			GROUP BY latest.library_id
		)
		SELECT l.id, l.name,
			COALESCE(b.units_total, 0) AS total,
			COALESCE(au.cnt, 0) AS active,
			COALESCE(buckets.matched, 0) AS matched,
			COALESCE(buckets.unmatched, 0) AS unmatched,
			COALESCE(buckets.errors, 0) AS errors
		FROM libraries l
		LEFT JOIN library_scan_bursts b ON b.library_id = l.id
		LEFT JOIN active_units au ON au.library_id = l.id
		LEFT JOIN buckets ON buckets.library_id = l.id
		WHERE l.id IN (SELECT library_id FROM active_units WHERE library_id IS NOT NULL)
		   OR l.id IN (SELECT library_id FROM active_kickoffs WHERE library_id IS NOT NULL)
		   OR EXISTS (SELECT 1 FROM active_kickoffs WHERE library_id IS NULL)
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	libs := make([]LibraryScanProgress, 0)
	for rows.Next() {
		var lp LibraryScanProgress
		var total, active int
		if err := rows.Scan(&lp.LibraryID, &lp.Name, &total, &active, &lp.Matched, &lp.Unmatched, &lp.Errors); err != nil {
			continue
		}
		if active == 0 {
			// Discovery (kickoff walking, nothing enqueued yet): totals are
			// unknown; a stale burst row must not read as instantly done.
			lp.Total, lp.Processed = 0, 0
		} else {
			// A missed bump (crash between insert and bump, bypassed site)
			// can leave total < active; clamp so processed never goes
			// negative and the bar reads low rather than lying high.
			lp.Total = max(total, active)
			lp.Processed = lp.Total - active
		}
		libs = append(libs, lp)
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

			_ = db.QueryRow(ctx, "SELECT count(*) FROM libraries").Scan(&s.Libraries)
			if rows, err := db.Query(ctx, `SELECT media_type::text, count(*) FROM media_items GROUP BY media_type`); err == nil {
				for rows.Next() {
					var mediaType string
					var count int
					if rows.Scan(&mediaType, &count) == nil {
						s.MediaCounts[mediaType] = count
						s.TotalMedia += count
					}
				}
				rows.Close()
			}
			s.TotalPeople = estimatedRows(ctx, db, "public.people")
			s.TotalFiles = estimatedRows(ctx, db, "public.library_files")

			if counts, err := queueops.CountActive(ctx, db); err == nil {
				s.QueuePending = counts.Pending
				s.QueueRunning = counts.Running
			}

			h.Emit(EventStatsUpdated, s)
		}
	}
}

func estimatedRows(ctx context.Context, db *pgxpool.Pool, table string) int {
	var count int64
	if err := db.QueryRow(ctx,
		`SELECT GREATEST(COALESCE((SELECT reltuples::bigint FROM pg_class WHERE oid = to_regclass($1)), 0), 0)`,
		table,
	).Scan(&count); err != nil {
		return 0
	}
	return int(count)
}
