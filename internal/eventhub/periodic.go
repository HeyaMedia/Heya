package eventhub

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/taskdefs"
)

func (h *Hub) StartPeriodicEmitters(ctx context.Context, db *pgxpool.Pool) {
	go h.queueTelemetryTicker(ctx, db)
	go h.statsTicker(ctx, db)
}

// queueTelemetryTicker makes one grouped pass over live River rows every ten
// seconds and fans that snapshot out to queue status, scheduled-task progress,
// active jobs, scan progress, and the stats ticker. A 650k-row parked backlog
// must not turn each UI surface into its own two-second full-table scan.
func (h *Hub) queueTelemetryTicker(ctx context.Context, db *pgxpool.Pool) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	wasScanning := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !h.HasSubscribers() {
				continue
			}
			live, err := queueops.CountLiveByKindAndTask(ctx, db)
			if err != nil {
				continue
			}

			status, activeUnits, activeKickoffs, globalKickoff := queueSnapshotCounts(live)
			h.setQueueStatus(status)
			h.Emit(EventQueueStatus, status)

			for _, def := range taskdefs.All() {
				taskID := def.ID
				if def.Synthetic {
					taskID = ""
				}
				counts := queueops.RuntimeCountsFor(live, taskdefs.TaskKinds(def.ID), taskID)
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

			// Active rows are a tiny subset of the backlog. Keep this detail
			// query separate from the grouped snapshot so its LIMIT stays cheap.
			rows, err := db.Query(ctx, `
				SELECT rj.id, rj.kind, rj.queue, rj.attempted_at, rj.args::text,
				       COALESCE(l.name, '') AS library_name
				FROM river_job rj
				LEFT JOIN libraries l ON l.id = NULLIF(rj.args->>'library_id', '')::bigint
				WHERE rj.state = 'running'
				  AND rj.kind NOT IN ('debounce_sweep', 'metadata_continuation_sweep')
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

			scanning := globalKickoff || len(activeUnits) > 0 || len(activeKickoffs) > 0
			if scanning || wasScanning {
				h.emitScanProgress(ctx, db, activeUnits, activeKickoffs, globalKickoff)
			}
			wasScanning = scanning
		}
	}
}

func queueSnapshotCounts(live []queueops.TaskKindCounts) (QueueStatusPayload, map[int64]int, map[int64]struct{}, bool) {
	status := QueueStatusPayload{}
	activeUnits := make(map[int64]int)
	activeKickoffs := make(map[int64]struct{})
	globalKickoff := false
	for _, row := range live {
		if row.Kind != "debounce_sweep" && row.Kind != "metadata_continuation_sweep" {
			status.Pending += row.Pending
			status.Running += row.Running
		}
		active := row.Pending + row.Running
		if active == 0 {
			continue
		}
		switch row.Kind {
		case "process_scan", "search_metadata", "fetch_metadata", "apply_metadata":
			if row.LibraryID != 0 {
				activeUnits[row.LibraryID] += active
			}
		case "kickoff_library_scan":
			if row.LibraryID == 0 {
				globalKickoff = true
			} else {
				activeKickoffs[row.LibraryID] = struct{}{}
			}
		}
	}
	return status, activeUnits, activeKickoffs, globalKickoff
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
func (h *Hub) emitScanProgress(ctx context.Context, db *pgxpool.Pool, activeUnits map[int64]int, activeKickoffs map[int64]struct{}, globalKickoff bool) {
	activeLibrarySet := make(map[int64]struct{}, len(activeUnits)+len(activeKickoffs))
	for libraryID := range activeUnits {
		activeLibrarySet[libraryID] = struct{}{}
	}
	for libraryID := range activeKickoffs {
		activeLibrarySet[libraryID] = struct{}{}
	}
	activeLibraryIDs := make([]int64, 0, len(activeLibrarySet))
	for libraryID := range activeLibrarySet {
		activeLibraryIDs = append(activeLibraryIDs, libraryID)
	}
	sort.Slice(activeLibraryIDs, func(i, j int) bool { return activeLibraryIDs[i] < activeLibraryIDs[j] })

	rows, err := db.Query(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (se.library_id, se.identity_key)
				se.library_id, se.identity_key, se.status
			FROM scanner_entities se
			WHERE $2::boolean OR se.library_id = ANY($1::bigint[])
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
			COALESCE(buckets.matched, 0) AS matched,
			COALESCE(buckets.unmatched, 0) AS unmatched,
			COALESCE(buckets.errors, 0) AS errors
		FROM libraries l
		LEFT JOIN library_scan_bursts b ON b.library_id = l.id
		LEFT JOIN buckets ON buckets.library_id = l.id
		WHERE $2::boolean OR l.id = ANY($1::bigint[])
	`, activeLibraryIDs, globalKickoff)
	if err != nil {
		return
	}
	defer rows.Close()

	libs := make([]LibraryScanProgress, 0)
	for rows.Next() {
		var lp LibraryScanProgress
		var total, active int
		if err := rows.Scan(&lp.LibraryID, &lp.Name, &total, &lp.Matched, &lp.Unmatched, &lp.Errors); err != nil {
			continue
		}
		active = activeUnits[lp.LibraryID]
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

			status := h.queueStatusSnapshot()
			s.QueuePending = status.Pending
			s.QueueRunning = status.Running

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
