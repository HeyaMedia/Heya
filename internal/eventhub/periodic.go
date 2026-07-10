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
	// the burst query isn't free and shouldn't run on an idle box.
	wasScanning := false
	// burstMemo holds per-library high-water marks for the current burst.
	// River job history is disposable (the job cleaner prunes completed
	// rows — including the kickoff row a long burst is anchored on — and
	// Cancel-all can purge mid-scan); without the memo the bar would
	// collapse backward when counted rows vanish.
	//
	// The memo's lifecycle runs BEFORE the subscriber gate, every tick:
	// marks are dropped the moment their library has no active scan jobs.
	// That boundary is observed within one tick regardless of whether
	// anyone is watching, so marks can never survive their burst and leak
	// into the next one (e.g. a burst ending and a new one starting during
	// subscriber downtime) — no matter how the SQL's burst window drifts.
	burstMemo := map[int64]*scanBurstMemo{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scanning := scanJobsActive(ctx, db)
			if !scanning {
				clear(burstMemo)
			} else if len(burstMemo) > 0 {
				pruneScanBurstMemo(ctx, db, burstMemo)
			}

			if !h.HasSubscribers() {
				wasScanning = scanning
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

			if scanning || wasScanning {
				h.emitScanProgress(ctx, db, burstMemo)
			}
			wasScanning = scanning
		}
	}
}

// scanBurstMemo is the emitter's in-memory high-water mark for one
// library's current scan burst. SQL counts are trusted when they grow;
// when River's history shrinks under us (job cleaner, Cancel-all, or the
// burst window drifting as anchoring rows are pruned), the memo holds the
// line and processed derives from total − active instead — active jobs
// are the one population River never deletes mid-scan. Burst identity is
// not tracked here: the ticker prunes entries the moment their library
// has no active scan jobs (unconditionally, before the subscriber gate),
// so an entry existing means its burst is still the current one. On
// process restart the memo starts empty and progress degrades gracefully
// to the surviving history.
type scanBurstMemo struct {
	total     int
	processed int
}

// pruneScanBurstMemo drops memo entries for libraries with no active scan
// pipeline jobs — their burst is over. One cheap DISTINCT over active jobs.
func pruneScanBurstMemo(ctx context.Context, db *pgxpool.Pool, memo map[int64]*scanBurstMemo) {
	rows, err := db.Query(ctx, `
		SELECT DISTINCT NULLIF(args->>'library_id', '')::bigint
		FROM river_job
		WHERE state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
		  AND kind IN ('kickoff_library_scan', 'process_scan', 'fetch_metadata', 'apply_metadata')`)
	if err != nil {
		return
	}
	defer rows.Close()
	active := map[int64]bool{}
	allLibraries := false
	for rows.Next() {
		var id *int64
		if err := rows.Scan(&id); err != nil {
			continue
		}
		if id == nil {
			allLibraries = true // a scan-all kickoff keeps every library's burst alive
			continue
		}
		active[*id] = true
	}
	if allLibraries {
		return
	}
	for id := range memo {
		if !active[id] {
			delete(memo, id)
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
			  AND kind IN ('kickoff_library_scan', 'process_scan', 'fetch_metadata', 'apply_metadata')
		)`).Scan(&active)
	return err == nil && active
}

// emitScanProgress reports per-library scan state for libraries with scan
// pipeline work in flight. Presence in the payload = the library is
// scanning (it has an active kickoff/process/fetch/apply job).
//
// The progress bar (processed/total) counts pipeline JOBS in the current
// burst, not entity states: queued units keep their entities' old statuses
// until they actually run, so any entity-derived bar shows stale 100%
// while a fresh fan-out sits queued. The burst is every pipeline job
// created since the anchor — the earliest of (oldest still-active job,
// last kickoff start) — which keeps the total stable through the tail and
// across overlapping scans. A library whose kickoff is still discovering
// has no burst jobs yet and reports 0/0 (FE renders that as 0%).
//
// matched/unmatched/errors stay identity-based (latest entity row per
// identity — duplicate chunk-era rows would otherwise count in-flight
// rescans as done) as informational buckets for the review UI.
//
// memo guards against River's disposable history: counts are clamped to
// per-burst high-water marks, and processed additionally derives from
// total − active so the bar keeps advancing even if completed rows are
// pruned mid-scan.
func (h *Hub) emitScanProgress(ctx context.Context, db *pgxpool.Pool, memo map[int64]*scanBurstMemo) {
	rows, err := db.Query(ctx, `
		WITH active_units AS (
			SELECT NULLIF(rj.args->>'library_id', '')::bigint AS library_id, rj.created_at
			FROM river_job rj
			WHERE rj.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
			  AND rj.kind IN ('process_scan', 'fetch_metadata', 'apply_metadata')
		),
		active_kickoffs AS (
			SELECT DISTINCT NULLIF(rj.args->>'library_id', '')::bigint AS library_id
			FROM river_job rj
			WHERE rj.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
			  AND rj.kind = 'kickoff_library_scan'
		),
		burst_anchor AS (
			-- The burst window opens at the kickoff that STARTED this burst:
			-- the latest kickoff at or before the oldest still-active job.
			-- Later kickoffs (e.g. a scan-all for another library) must not
			-- move the window; if the anchoring kickoff row has been pruned,
			-- fall back to the oldest active job and let the in-memory
			-- high-water marks absorb the shrunken window.
			SELECT au.library_id,
				COALESCE((
					SELECT max(k.attempted_at) FROM river_job k
					WHERE k.kind = 'kickoff_library_scan'
					  AND k.attempted_at > now() - interval '48 hours'
					  AND k.attempted_at <= min(au.created_at)
					  AND (NULLIF(k.args->>'library_id', '')::bigint = au.library_id
					       OR k.args->>'library_id' IS NULL)
				), min(au.created_at)) AS burst_start
			FROM active_units au
			WHERE au.library_id IS NOT NULL
			GROUP BY au.library_id
		),
		burst AS (
			SELECT b.library_id,
				count(*) AS total_units,
				count(*) FILTER (WHERE rj.state IN ('completed', 'discarded', 'cancelled')) AS done_units
			FROM burst_anchor b
			JOIN river_job rj ON rj.kind IN ('process_scan', 'fetch_metadata', 'apply_metadata')
				AND rj.created_at >= b.burst_start
				AND rj.created_at > now() - interval '48 hours'
				AND NULLIF(rj.args->>'library_id', '')::bigint = b.library_id
			GROUP BY b.library_id
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
			COALESCE(burst.total_units, 0) AS total,
			COALESCE(burst.done_units, 0) AS processed,
			(SELECT count(*) FROM active_units au WHERE au.library_id = l.id) AS active,
			COALESCE(buckets.matched, 0) AS matched,
			COALESCE(buckets.unmatched, 0) AS unmatched,
			COALESCE(buckets.errors, 0) AS errors
		FROM libraries l
		LEFT JOIN burst ON burst.library_id = l.id
		LEFT JOIN buckets ON buckets.library_id = l.id
		WHERE l.id IN (SELECT library_id FROM active_units WHERE library_id IS NOT NULL)
		   OR l.id IN (SELECT library_id FROM active_kickoffs WHERE library_id IS NOT NULL)
		   OR EXISTS (SELECT 1 FROM active_kickoffs WHERE library_id IS NULL)
		GROUP BY l.id, l.name, burst.total_units, burst.done_units, buckets.matched, buckets.unmatched, buckets.errors
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	libs := make([]LibraryScanProgress, 0)
	for rows.Next() {
		var lp LibraryScanProgress
		var active int
		if err := rows.Scan(&lp.LibraryID, &lp.Name, &lp.Total, &lp.Processed, &active, &lp.Matched, &lp.Unmatched, &lp.Errors); err != nil {
			continue
		}
		// An existing memo entry always describes the CURRENT burst — the
		// ticker prunes entries unconditionally the moment their library
		// has no active scan jobs, so stale marks cannot reach this point.
		m := memo[lp.LibraryID]
		if m == nil {
			m = &scanBurstMemo{}
			memo[lp.LibraryID] = m
		}
		// Totals only grow within a burst; a shrink means counted rows were
		// pruned (or the window drifted), so the memo holds the line.
		// Active jobs can't be pruned, so total is at least done+active,
		// and once total is pinned, total − active recovers the done work
		// whose rows vanished.
		m.total = max(m.total, lp.Total, lp.Processed+active)
		m.processed = min(m.total, max(m.processed, lp.Processed, m.total-active))
		lp.Total, lp.Processed = m.total, m.processed
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
