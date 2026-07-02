package worker

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/mediafile"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/scanner"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// WatcherPauser is the subset of *watcher.Manager that
// KickoffLibraryScanWorker needs. Letting fsnotify run during a scan
// would race with the scanner's bulk writes; pause/resume bracketing
// avoids that.
type WatcherPauser interface {
	Pause(libraryID int64)
	Resume(libraryID int64)
}

// ---------------------------------------------------------------------------
// kickoff_library_scan
// ---------------------------------------------------------------------------

// KickoffLibraryScanWorker walks one or all libraries, runs the
// scanner, and enqueues ProcessFile jobs for every pending file. When
// args.LibraryID > 0 it scans that single library; otherwise it walks
// every library in the priority order movies → tv → music → books so a
// fresh DB fills predictably for the user's primary media type first.
type KickoffLibraryScanWorker struct {
	river.WorkerDefaults[KickoffLibraryScanArgs]
	DB       *pgxpool.Pool
	Hub      EventPublisher
	Watcher  WatcherPauser
	Progress *TaskProgressBroadcaster
}

func (w *KickoffLibraryScanWorker) Work(ctx context.Context, job *river.Job[KickoffLibraryScanArgs]) error {
	startedAt := time.Now()
	taskID := job.Args.ScheduledTaskID
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)

	var libs []sqlc.Library
	var err error
	if job.Args.LibraryID > 0 {
		lib, gErr := q.GetLibraryByID(ctx, job.Args.LibraryID)
		if gErr != nil {
			finishKickoff(ctx, q, taskID, startedAt, 0, 0, gErr)
			return gErr
		}
		libs = []sqlc.Library{lib}
	} else {
		libs, err = q.ListLibraries(ctx)
		if err != nil {
			finishKickoff(ctx, q, taskID, startedAt, 0, 0, err)
			return err
		}
		sortLibrariesByMediaPriority(libs)
	}

	s := scanner.New(w.DB)
	enqueued := 0
	failed := 0

	for _, lib := range libs {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, err)
			return err
		}

		w.Progress.Set("scan_libraries", "kickoff_library_scan", lib.Name)

		if w.Watcher != nil {
			w.Watcher.Pause(lib.ID)
		}
		emit(w.Hub, eventhub.EventScanStarted, eventhub.ScanPayload{
			LibraryID:   lib.ID,
			LibraryName: lib.Name,
		})

		result, scanErr := s.ScanLibrary(ctx, lib, scanner.ScanOptions{
			ForceRescan: job.Args.Force,
		})

		if w.Watcher != nil {
			w.Watcher.Resume(lib.ID)
		}

		if scanErr != nil {
			log.Error().Err(scanErr).Int64("library_id", lib.ID).Msg("kickoff_library_scan: scan error")
			failed++
			// A cancelled scan leaves the discovered set incomplete, so don't
			// act on partial results. But a partial-root failure (e.g. one
			// removed root) still ran discovery + deletion detection for the
			// healthy roots — fall through so newly-found files get processed
			// and the soft-deletes still emit their refresh event.
			if ctx.Err() != nil {
				continue
			}
		}

		n, enqueueFailed := enqueuePendingFiles(ctx, q, rc, lib.ID, taskID)
		enqueued += n
		failed += enqueueFailed

		// Self-heal files that were matched but never successfully probed (their
		// first ffprobe failed on a flaky mount, and the size+mtime skip means
		// plain rescans never revisit them). ffprobe jobs are unique-while-active,
		// so this can't stack duplicates against probes still in flight.
		reprobed := enqueueReprobeUnprobed(ctx, q, rc, lib.ID, taskID)
		enqueued += reprobed

		// Self-heal files stranded 'unmatched' by a transient provider search
		// error — the match analogue of the reprobe pass. metadata_match is
		// unique-while-active, so re-drives coalesce.
		rematched := enqueueRematchTransient(ctx, q, rc, lib.ID, taskID)
		enqueued += rematched

		log.Info().
			Int64("library_id", lib.ID).
			Int("discovered", result.Discovered).
			Int("new", result.New).
			Int("deleted", result.Deleted).
			Int("enqueued", n).
			Int("reprobed", reprobed).
			Int("rematched", rematched).
			Msg("kickoff_library_scan: library done")

		emit(w.Hub, eventhub.EventScanCompleted, eventhub.ScanPayload{
			LibraryID:   lib.ID,
			LibraryName: lib.Name,
			Discovered:  result.Discovered,
			New:         result.New,
			Missing:     result.Deleted,
		})
		if result.Deleted > 0 {
			emit(w.Hub, eventhub.EventMediaRemoved, eventhub.MediaPayload{LibraryID: lib.ID})
		}
	}

	finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, nil)
	return nil
}

func sortLibrariesByMediaPriority(libs []sqlc.Library) {
	rank := func(mt sqlc.MediaType) int {
		switch mt {
		case sqlc.MediaTypeMovie:
			return 0
		case sqlc.MediaTypeTv:
			return 1
		case sqlc.MediaTypeMusic:
			return 2
		case sqlc.MediaTypeBook:
			return 3
		}
		return 4
	}
	sort.SliceStable(libs, func(i, j int) bool {
		return rank(libs[i].MediaType) < rank(libs[j].MediaType)
	})
}

// reprobeCap bounds how many stuck-unprobed files one scan re-enqueues per
// library, so a large backlog (the single ffprobe worker drains slowly) can't
// flood the queue in one pass. ffprobe jobs are unique-while-active, so the same
// files simply re-coalesce across scans until they actually drain.
const reprobeCap = 2000

// enqueueReprobeUnprobed re-enqueues ffprobe for probeable files that are known
// (matched) but never got media_info — the "scanned once, probe failed, never
// retried" gap. Files that already carry media_info are left untouched, so a
// probed-and-unchanged file is never needlessly re-probed.
func enqueueReprobeUnprobed(ctx context.Context, q *sqlc.Queries, rc *river.Client[pgx.Tx], libraryID int64, taskID string) int {
	if rc == nil {
		return 0
	}
	files, err := q.ListUnprobedProbeableFiles(ctx, sqlc.ListUnprobedProbeableFilesParams{
		LibraryID: libraryID,
		Limit:     reprobeCap,
	})
	if err != nil {
		log.Error().Err(err).Int64("library_id", libraryID).Msg("kickoff_library_scan: list unprobed failed")
		return 0
	}
	n := 0
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return n
		}
		if !mediafile.IsProbeable(f.Path) {
			continue // sidecars (.nfo/.srt/...) legitimately have no media_info
		}
		if _, err := rc.Insert(ctx, FFProbeArgs{
			LibraryFileID:   f.ID,
			FilePath:        f.Path,
			ScheduledTaskID: taskID,
		}, nil); err != nil {
			log.Warn().Err(err).Int64("file_id", f.ID).Msg("kickoff_library_scan: enqueue reprobe failed")
			continue
		}
		n++
	}
	return n
}

// enqueueRematchTransient re-enqueues metadata match for files stranded
// 'unmatched' by a transient provider search error, so a network/upstream blip
// during matching doesn't leave a file invisible forever. Only files whose
// error_message marks a transient search error are retried — a genuine "no
// results" is left alone. Capped per scan; metadata_match is unique-while-active.
func enqueueRematchTransient(ctx context.Context, q *sqlc.Queries, rc *river.Client[pgx.Tx], libraryID int64, taskID string) int {
	if rc == nil {
		return 0
	}
	files, err := q.ListRetryableUnmatchedFiles(ctx, sqlc.ListRetryableUnmatchedFilesParams{
		LibraryID: libraryID,
		Limit:     reprobeCap,
	})
	if err != nil {
		log.Error().Err(err).Int64("library_id", libraryID).Msg("kickoff_library_scan: list retryable-unmatched failed")
		return 0
	}
	lib, err := q.GetLibraryByID(ctx, libraryID)
	if err != nil {
		return 0
	}
	n := 0
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return n
		}
		if _, err := rc.Insert(ctx, MetadataMatchArgs{
			LibraryFileID:   f.ID,
			LibraryID:       libraryID,
			MediaType:       string(lib.MediaType),
			ScheduledTaskID: taskID,
		}, nil); err != nil {
			log.Warn().Err(err).Int64("file_id", f.ID).Msg("kickoff_library_scan: enqueue rematch failed")
			continue
		}
		n++
	}
	return n
}

func enqueuePendingFiles(ctx context.Context, q *sqlc.Queries, rc *river.Client[pgx.Tx], libraryID int64, taskID string) (int, int) {
	files, err := q.ListLibraryFilesByStatus(ctx, sqlc.ListLibraryFilesByStatusParams{
		LibraryID: libraryID,
		Status:    sqlc.FileStatusPending,
		Limit:     100000,
		Offset:    0,
	})
	if err != nil {
		log.Error().Err(err).Int64("library_id", libraryID).Msg("kickoff_library_scan: list pending failed")
		return 0, 1
	}
	if rc == nil {
		return 0, len(files)
	}
	enqueued, failed := 0, 0
	for i, f := range files {
		if err := ctx.Err(); err != nil {
			return enqueued, failed + len(files) - i
		}
		if _, err := rc.Insert(ctx, ProcessFileArgs{
			LibraryFileID:   f.ID,
			LibraryID:       libraryID,
			FilePath:        f.Path,
			ScheduledTaskID: taskID,
		}, nil); err != nil {
			log.Warn().Err(err).Int64("file_id", f.ID).Msg("kickoff_library_scan: enqueue process_file failed")
			failed++
			continue
		}
		enqueued++
	}
	return enqueued, failed
}

func emit(hub EventPublisher, t eventhub.EventType, p any) {
	if hub == nil {
		return
	}
	hub.Emit(t, p)
}

// ---------------------------------------------------------------------------
// kickoff_refresh_stale
// ---------------------------------------------------------------------------

type KickoffRefreshStaleWorker struct {
	river.WorkerDefaults[KickoffRefreshStaleArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *KickoffRefreshStaleWorker) Work(ctx context.Context, job *river.Job[KickoffRefreshStaleArgs]) error {
	startedAt := time.Now()
	taskID := job.Args.ScheduledTaskID
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)

	rows, err := w.DB.Query(ctx, `
		SELECT mi.id, mi.media_type, mi.title, l.settings, mi.metadata_refreshed_at, mi.enrichment_status
		FROM media_items mi
		JOIN libraries l ON l.id = mi.library_id
		WHERE mi.media_type = 'music' OR mi.external_ids != '{}'
		ORDER BY mi.metadata_refreshed_at ASC NULLS FIRST
	`)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, 0, 0, err)
		return err
	}
	defer rows.Close()

	now := time.Now()
	type stale struct {
		ID        int64
		MediaType sqlc.MediaType
		Title     string
		Force     bool
	}
	var items []stale
	for rows.Next() {
		var id int64
		var mt, title, status string
		var settingsJSON []byte
		var refreshedAt *time.Time
		if err := rows.Scan(&id, &mt, &title, &settingsJSON, &refreshedAt, &status); err != nil {
			continue
		}
		// A previously FAILED enrichment is stranded — River doesn't retry it
		// (markFailed returns nil) and rescans skip the unchanged file. Re-drive
		// it every sweep regardless of the metadata_refresh_days knob so a
		// transient provider blip self-heals. Non-forced is enough (the item
		// isn't 'complete', so the enrich idempotency gate lets it run).
		if status == "failed" {
			items = append(items, stale{ID: id, MediaType: sqlc.MediaType(mt), Title: title})
			continue
		}
		// Everything else here is the staleness path: only 'complete' items past
		// their window, and only when the library opted in (refresh_days > 0).
		// force=true because the enrich worker short-circuits non-forced refreshes
		// of already-'complete' items — without it the sweep would no-op.
		if status != enrichStatusComplete {
			continue
		}
		settings := metadata.ParseSettings(settingsJSON)
		if settings.MetadataRefreshDays <= 0 {
			continue
		}
		if refreshedAt == nil {
			items = append(items, stale{ID: id, MediaType: sqlc.MediaType(mt), Title: title, Force: true})
			continue
		}
		cutoff := now.AddDate(0, 0, -settings.MetadataRefreshDays)
		if refreshedAt.Before(cutoff) {
			items = append(items, stale{ID: id, MediaType: sqlc.MediaType(mt), Title: title, Force: true})
		}
	}

	enqueued := 0
	failed := 0
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, err)
			return err
		}
		w.Progress.Set("refresh_stale_items", "kickoff_refresh_stale", it.Title)
		if err := enqueueEnrich(ctx, rc, it.ID, it.MediaType, EnrichSourceScheduled, it.Force, taskID, 0, 0, 0); err != nil {
			log.Warn().Err(err).Int64("item_id", it.ID).Msg("kickoff_refresh_stale: enqueue failed")
			failed++
			continue
		}
		enqueued++
	}

	if enqueued > 0 {
		log.Info().Int("enqueued", enqueued).Msg("kickoff_refresh_stale: enqueued enrich jobs")
	}
	finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, nil)
	return nil
}

// ---------------------------------------------------------------------------
// kickoff pumps (music loudness + sonic analysis)
// ---------------------------------------------------------------------------

// The loudness and sonic kickoffs are "pumps": instead of fanning out one
// bounded batch and finishing (which stranded the rest of the backlog until
// the next cron window), the kickoff job stays active for the whole run —
// snoozing between wakes, topping the work queue up wave by wave until the
// backlog drains. Consequences that the rest of the design leans on:
//
//   - The kickoff row's uniqueness hold (uniqueWhileActive) spans the run,
//     so a cron firing during an active run coalesces into it — the window
//     is skipped rather than stacking a second run.
//   - The row's created_at is the run's start time and its metadata is the
//     run's memory: sweep cursors, enqueue counters, and the manual/
//     scheduled source marker all survive snoozes and even process
//     restarts (an orphaned 'running' row is rescued on boot and resumes).
//   - Manual runs ("Run Now" → metadata source=manual) drain everything;
//     cron-started runs additionally stop when the task's max-runtime
//     window closes. The pump checks the window itself on every wake, so
//     it winds down gracefully and stamps the scheduled_tasks row.
//   - The pending sets are swept in id order exactly once per run (cursor
//     in metadata), so an item whose work job fails permanently is passed
//     over instead of being re-listed and re-enqueued forever.
const (
	pumpSnoozeInterval = 30 * time.Second
	// pumpMaxErrStreak is how many consecutive failing wakes a run
	// survives before it's declared dead. One-off DB blips shouldn't kill
	// a days-long drain; a persistent fault shouldn't wedge the task.
	pumpMaxErrStreak = 10
)

// pumpState is the pump's cross-wake memory, persisted in the kickoff job
// row's metadata. Loudness uses both cursors; sonic only TrackCursor.
//
// Skipped counts sweep items whose insert coalesced with a job owned by
// another task (unique keys are per-entity, so e.g. a library scan's
// loudness hand-offs occupy the same slot but are invisible to this run's
// scoped counts) or whose insert errored. If any were skipped, the finish
// path re-runs the sweep once from zero (FinalSweep) so work that the
// other owner dropped — a cancelled scan, a max-runtime kill — still gets
// picked up instead of being silently stranded past the cursor.
type pumpState struct {
	Source      string `json:"source,omitempty"`
	Enqueued    int    `json:"enqueued,omitempty"`
	Failed      int    `json:"failed,omitempty"`
	ErrStreak   int    `json:"err_streak,omitempty"`
	Skipped     int    `json:"skipped,omitempty"`
	FinalSweep  bool   `json:"final_sweep,omitempty"`
	TrackCursor int64  `json:"track_cursor,omitempty"`
	AlbumCursor int64  `json:"album_cursor,omitempty"`
}

func readPumpState(metadata []byte) pumpState {
	var st pumpState
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &st)
	}
	return st
}

// patch serializes the keys the pump owns. Source is deliberately absent:
// MarkActiveKickoffManual may flip it mid-run, and the jsonb || merge must
// not undo that upgrade with the stale value read at wake start.
// "finishing" is always reset: it's only meaningful between a
// ClaimKickoffFinish and the completion that follows it (no patch is
// written in that window), so any patched wake is by definition a run
// that continues — including one that aborted a wind-down or resumed
// after a crash mid-finish — and must accept upgrades again.
func (st pumpState) patch() []byte {
	b, err := json.Marshal(map[string]any{
		"enqueued":     st.Enqueued,
		"failed":       st.Failed,
		"err_streak":   st.ErrStreak,
		"skipped":      st.Skipped,
		"final_sweep":  st.FinalSweep,
		"finishing":    false,
		"track_cursor": st.TrackCursor,
		"album_cursor": st.AlbumCursor,
	})
	if err != nil {
		return []byte("{}")
	}
	return b
}

// restartSweep resets the cursors for the one-time verification pass over
// items that were skipped (coalesced with another owner's job or failed to
// insert). Returns false once the final sweep has already run — the pump
// finishes rather than looping.
func (st *pumpState) restartSweep() bool {
	if st.Skipped == 0 || st.FinalSweep {
		return false
	}
	st.FinalSweep = true
	st.Skipped = 0
	st.TrackCursor = 0
	st.AlbumCursor = 0
	return true
}

// continueAsManual reorients an in-flight run after a mid-wake Run-Now
// upgrade beat the completion claim: sweep everything still pending from
// scratch, exactly like a freshly-started manual run would. (The row's
// source is already "manual" — MarkActiveKickoffManual wrote it — so only
// the in-memory copy and the sweep state need resetting; the next state
// patch clears the finishing claim.)
func (st *pumpState) continueAsManual() {
	st.Source = queueops.KickoffSourceManual
	st.Skipped = 0
	st.FinalSweep = false
	st.TrackCursor = 0
	st.AlbumCursor = 0
}

// pumpFinishHandshake claims the finishing marker ahead of ANY pump
// completion — drained, wind-down, disabled, or error give-up. It returns
// proceed=false when the claim reveals a Run-Now upgrade that landed
// mid-wake on a cron run: st has been reoriented as a fresh manual drain
// and the caller must keep the run alive. With proceed=true the caller
// completes, and upgrades arriving from now on are rejected by
// MarkActiveKickoffManual's finishing guard (their TriggerNow starts a
// fresh run instead) — so a click can never land on a completing row and
// be silently swallowed. Runs already manual always proceed: the claim
// still blocks late upgrades, but their own source can't distinguish a
// new click from the old state, and re-aborting on it would loop forever.
func pumpFinishHandshake(ctx context.Context, db *pgxpool.Pool, jobID int64, st *pumpState) (proceed bool, err error) {
	live, err := queueops.ClaimKickoffFinish(ctx, db, jobID)
	if err != nil {
		return false, err
	}
	if st.Source != queueops.KickoffSourceManual && live == queueops.KickoffSourceManual {
		st.continueAsManual()
		return false, nil
	}
	return true, nil
}

// pumpSnooze persists the pump's state and puts the kickoff back to
// sleep. JobSnooze doesn't consume attempts, so a MaxAttempts=1 kickoff
// can wake indefinitely.
func pumpSnooze(ctx context.Context, db *pgxpool.Pool, jobID int64, taskID string, st pumpState) error {
	if err := queueops.MergeJobMetadata(ctx, db, jobID, st.patch()); err != nil {
		log.Warn().Err(err).Str("task", taskID).Msg("kickoff pump: persist state failed")
	}
	return river.JobSnooze(pumpSnoozeInterval)
}

// pumpActiveCount returns how many of the task's own work jobs of one kind
// are still pending or running. Jobs the same kind owes to other owners
// (e.g. a library scan's loudness hand-off) are excluded — the pump only
// waits on work it fanned out.
func pumpActiveCount(ctx context.Context, db *pgxpool.Pool, taskID, kind string) (int, error) {
	if taskID == "" {
		counts, err := queueops.CountByKinds(ctx, db, []string{kind})
		return counts.Pending + counts.Running, err
	}
	counts, err := queueops.CountScheduledTask(ctx, db, taskID, []string{kind})
	return counts.Pending + counts.Running, err
}

// pumpShouldStop reports whether a cron-started run must wind down: the
// task was disabled mid-run, or it outlived its max-runtime window. Manual
// runs never expire — only a user cancel stops them. The task row is
// re-read on every wake so a mid-run settings change takes effect.
func pumpShouldStop(ctx context.Context, q *sqlc.Queries, taskID, source string, runStarted time.Time) (bool, string) {
	if source == queueops.KickoffSourceManual || taskID == "" {
		return false, ""
	}
	task, err := q.GetScheduledTask(ctx, taskID)
	if err != nil {
		return false, ""
	}
	if !task.Enabled {
		return true, "task disabled"
	}
	if task.MaxRuntimeMinutes > 0 && time.Since(runStarted) > time.Duration(task.MaxRuntimeMinutes)*time.Minute {
		return true, "max runtime reached"
	}
	return false, ""
}

// pumpInterrupted handles a context death mid-wake (user cancel, process
// shutdown, job timeout): persist the cursors best-effort and yield with a
// zero snooze. This can't escape a user cancel — River finalizes a
// snoozing job as cancelled when cancel_attempted_at is stamped in its
// metadata — while a plain shutdown parks the row 'available' so the run
// resumes right where it left off on the next boot. Run bookkeeping for
// the cancel case is stamped by service.CancelTask, which reads the
// kickoff row before cancelling it.
func pumpInterrupted(ctx context.Context, db *pgxpool.Pool, jobID int64, taskID string, st pumpState) error {
	_ = ctx // dead by definition here; persist on a short background context
	persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := queueops.MergeJobMetadata(persistCtx, db, jobID, st.patch()); err != nil {
		log.Warn().Err(err).Str("task", taskID).Msg("kickoff pump: persist state on interrupt failed")
	}
	return river.JobSnooze(0)
}

// pumpTransientFailure bumps the run's error streak and snoozes instead
// of failing the single-attempt kickoff. Past pumpMaxErrStreak the run
// fails for real (finishKickoff stamps the error, MaxAttempts=1 discards)
// — through the finishing handshake, so a Run Now that landed mid-wake
// restarts the drain ("user poked it, try again") instead of dying with
// the run, and one arriving later starts a fresh run.
func pumpTransientFailure(ctx context.Context, db *pgxpool.Pool, q *sqlc.Queries, jobID int64, taskID string, st pumpState, runStarted time.Time, cause error) error {
	if ctx.Err() != nil {
		return pumpInterrupted(ctx, db, jobID, taskID, st)
	}
	st.ErrStreak++
	if st.ErrStreak >= pumpMaxErrStreak {
		// A claim error is ignored: the run is already dying of repeated
		// failures, and the claim was best-effort protection on top.
		if proceed, err := pumpFinishHandshake(ctx, db, jobID, &st); err == nil && !proceed {
			log.Info().Str("task", taskID).Msg("kickoff pump: error give-up aborted — run upgraded to manual mid-wake")
			st.ErrStreak = 0
			return pumpSnooze(ctx, db, jobID, taskID, st)
		}
		log.Error().Err(cause).Str("task", taskID).Msg("kickoff pump: giving up after repeated failures")
		finishKickoff(ctx, q, taskID, runStarted, st.Enqueued, st.Failed, cause)
		return cause
	}
	log.Warn().Err(cause).Str("task", taskID).Int("err_streak", st.ErrStreak).Msg("kickoff pump: transient failure, snoozing")
	if err := queueops.MergeJobMetadata(ctx, db, jobID, st.patch()); err != nil {
		log.Warn().Err(err).Str("task", taskID).Msg("kickoff pump: persist state failed")
	}
	return river.JobSnooze(pumpSnoozeInterval)
}

// ---------------------------------------------------------------------------
// kickoff_music_loudness
// ---------------------------------------------------------------------------

// Per-wave caps. The scan_track_loudness queue is MaxWorkers=1 so it'll
// chew through the backlog at ~30s/track regardless. The pump keeps at
// most one wave in River at a time and tops it up as it drains, so the
// job table stays bounded no matter how large the backlog is.
const (
	kickoffLoudnessTrackBatch = 500
	kickoffLoudnessAlbumBatch = 200
)

type KickoffMusicLoudnessWorker struct {
	river.WorkerDefaults[KickoffMusicLoudnessArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *KickoffMusicLoudnessWorker) Work(ctx context.Context, job *river.Job[KickoffMusicLoudnessArgs]) error {
	taskID := job.Args.ScheduledTaskID
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)
	st := readPumpState(job.Metadata)
	trackKind := ScanTrackLoudnessArgs{}.Kind()
	albumKind := ScanAlbumLoudnessArgs{}.Kind()

	if ctx.Err() != nil {
		return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
	}

	if stop, reason := pumpShouldStop(ctx, q, taskID, st.Source, job.CreatedAt); stop {
		switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
		case err != nil:
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		case !proceed:
			log.Info().Str("task", taskID).Msg("kickoff_music_loudness: wind-down aborted — run upgraded to manual mid-wake")
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		cancelled, _ := queueops.CancelPendingByScheduledTask(ctx, w.DB, taskID, []string{trackKind, albumKind})
		log.Info().Str("task", taskID).Str("reason", reason).Int64("cancelled_pending", cancelled).Msg("kickoff_music_loudness: winding down")
		finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
		return nil
	}

	// Track phase: keep one wave of per-track jobs topped up, sweeping the
	// pending set in id order exactly once.
	trackActive, err := pumpActiveCount(ctx, w.DB, taskID, trackKind)
	if err != nil {
		return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
	}
	tracksListed := -1 // -1: wave full, sweep not consulted this wake
	if want := kickoffLoudnessTrackBatch - trackActive; want > 0 {
		rows, err := q.ListTrackFilesPendingLoudness(ctx, sqlc.ListTrackFilesPendingLoudnessParams{
			AfterID:  st.TrackCursor,
			RowLimit: int32(want),
		})
		if err != nil {
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		}
		tracksListed = len(rows)
		for _, row := range rows {
			if ctx.Err() != nil {
				return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
			}
			w.Progress.Set("scan_music_loudness", "kickoff_music_loudness", row.Path)
			res, err := rc.Insert(ctx, ScanTrackLoudnessArgs{TrackFileID: row.ID, ScheduledTaskID: taskID}, nil)
			switch {
			case err != nil:
				log.Warn().Err(err).Int64("track_file_id", row.ID).Msg("kickoff_music_loudness: enqueue track failed")
				st.Failed++
				st.Skipped++
			case res.UniqueSkippedAsDuplicate:
				st.Skipped++
			default:
				st.Enqueued++
			}
			st.TrackCursor = row.ID
		}
	}
	tracksDone := trackActive == 0 && tracksListed == 0

	// Album phase: only starts once the track sweep has drained, so album
	// eligibility (all tracks measured) is stable and one monotonic pass is
	// complete. Albums that finished *during* this run were already
	// enqueued by the track worker's cascade; the unique args make this
	// sweep coalesce with those.
	if tracksDone {
		albumActive, err := pumpActiveCount(ctx, w.DB, taskID, albumKind)
		if err != nil {
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		}
		albumsListed := -1
		if want := kickoffLoudnessAlbumBatch - albumActive; want > 0 {
			rows, err := q.ListAlbumsPendingLoudness(ctx, sqlc.ListAlbumsPendingLoudnessParams{
				AfterID:  st.AlbumCursor,
				RowLimit: int32(want),
			})
			if err != nil {
				return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
			}
			albumsListed = len(rows)
			for _, row := range rows {
				if ctx.Err() != nil {
					return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
				}
				w.Progress.Set("scan_music_loudness", "kickoff_music_loudness", row.Title)
				res, err := rc.Insert(ctx, ScanAlbumLoudnessArgs{AlbumID: row.ID, ScheduledTaskID: taskID}, nil)
				switch {
				case err != nil:
					log.Warn().Err(err).Int64("album_id", row.ID).Msg("kickoff_music_loudness: enqueue album failed")
					st.Failed++
					st.Skipped++
				case res.UniqueSkippedAsDuplicate:
					st.Skipped++
				default:
					st.Enqueued++
				}
				st.AlbumCursor = row.ID
			}
		}
		if albumActive == 0 && albumsListed == 0 {
			if st.restartSweep() {
				log.Info().Str("task", taskID).Msg("kickoff_music_loudness: re-sweeping for items skipped during the run")
				st.ErrStreak = 0
				return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
			}
			switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
			case err != nil:
				return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
			case !proceed:
				log.Info().Str("task", taskID).Msg("kickoff_music_loudness: finish aborted — run upgraded to manual mid-wake")
				st.ErrStreak = 0
				return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
			}
			log.Info().Str("task", taskID).Int("enqueued", st.Enqueued).Int("failed", st.Failed).Msg("kickoff_music_loudness: backlog drained")
			finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
			return nil
		}
	}

	st.ErrStreak = 0
	return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
}

// ---------------------------------------------------------------------------
// kickoff_trickplay
// ---------------------------------------------------------------------------

type KickoffTrickplayWorker struct {
	river.WorkerDefaults[KickoffTrickplayArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *KickoffTrickplayWorker) Work(ctx context.Context, job *river.Job[KickoffTrickplayArgs]) error {
	startedAt := time.Now()
	taskID := job.Args.ScheduledTaskID
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)

	// Eligibility lives in the trickplay_eligible_files view (migration 00035),
	// shared with the Settings counts and task item listings — one predicate,
	// no count-vs-enqueue drift.
	pending, err := q.ListTrickplayPendingKickoff(ctx)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, 0, 0, err)
		return err
	}

	enqueued, failed := 0, 0
	for _, f := range pending {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, err)
			return err
		}
		w.Progress.Set("generate_trickplay", "kickoff_trickplay", filepathBase(f.Path))
		if _, err := rc.Insert(ctx, TrickplayFileArgs{LibraryFileID: f.ID, ScheduledTaskID: taskID}, nil); err != nil {
			log.Warn().Err(err).Int64("library_file_id", f.ID).Msg("kickoff_trickplay: enqueue failed")
			failed++
			continue
		}
		enqueued++
	}

	finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, nil)
	return nil
}

// filepathBase is a local indirection so we can keep the import surface of
// kickoff_workers.go small (no path/filepath import needed elsewhere here).
func filepathBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}

// ---------------------------------------------------------------------------
// kickoff_thumbnails
// ---------------------------------------------------------------------------

type KickoffThumbnailsWorker struct {
	river.WorkerDefaults[KickoffThumbnailsArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *KickoffThumbnailsWorker) Work(ctx context.Context, job *river.Job[KickoffThumbnailsArgs]) error {
	startedAt := time.Now()
	taskID := job.Args.ScheduledTaskID
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)

	// Eligibility lives in the thumbnail_eligible_extras view (migration 00035),
	// shared with the Settings counts and task item listings — one predicate,
	// no count-vs-enqueue drift.
	pending, err := q.ListThumbnailPendingKickoff(ctx)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, 0, 0, err)
		return err
	}

	enqueued, failed := 0, 0
	for _, e := range pending {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, err)
			return err
		}
		label := e.Title
		if label == "" {
			label = filepathBase(e.FilePath)
		}
		w.Progress.Set("generate_thumbnails", "kickoff_thumbnails", label)
		if _, err := rc.Insert(ctx, ThumbnailExtraArgs{ExtraID: e.ID, ScheduledTaskID: taskID}, nil); err != nil {
			log.Warn().Err(err).Int64("extra_id", e.ID).Msg("kickoff_thumbnails: enqueue failed")
			failed++
			continue
		}
		enqueued++
	}

	finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, nil)
	return nil
}

// ---------------------------------------------------------------------------
// kickoff_sonic_analysis
// ---------------------------------------------------------------------------

// SonicEnabledFn is the runtime gate for kickoff_sonic_analysis. Lets
// the worker honour the system_settings toggle without importing the
// service layer. Wired up by the App at startup.
type SonicEnabledFn func(ctx context.Context) bool

// sonicKickoffBatch caps the pump's in-flight wave so a fresh 100k-track
// library doesn't dump 100k jobs into River in one shot. The pump tops
// the wave up as it drains until the whole backlog is analyzed.
const sonicKickoffBatch = 1000

type KickoffSonicAnalysisWorker struct {
	river.WorkerDefaults[KickoffSonicAnalysisArgs]
	DB       *pgxpool.Pool
	Enabled  SonicEnabledFn
	Progress *TaskProgressBroadcaster
}

func (w *KickoffSonicAnalysisWorker) Work(ctx context.Context, job *river.Job[KickoffSonicAnalysisArgs]) error {
	taskID := job.Args.ScheduledTaskID
	q := sqlc.New(w.DB)
	st := readPumpState(job.Metadata)
	kind := AnalyzeTrackFacetsArgs{}.Kind()

	if ctx.Err() != nil {
		return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
	}

	// Checked on every wake, so toggling the setting off mid-run stops the
	// pump; only the in-flight wave (bounded) is left to drain. Goes
	// through the finishing handshake like every completion — a mid-wake
	// upgrade just defers the (inevitable, feature's off) finish by one
	// wake rather than being swallowed by it.
	if w.Enabled != nil && !w.Enabled(ctx) {
		switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
		case err != nil:
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		case !proceed:
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		log.Info().Msg("kickoff_sonic_analysis: disabled in settings — stopping")
		finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
		return nil
	}

	if stop, reason := pumpShouldStop(ctx, q, taskID, st.Source, job.CreatedAt); stop {
		switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
		case err != nil:
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		case !proceed:
			log.Info().Str("task", taskID).Msg("kickoff_sonic_analysis: wind-down aborted — run upgraded to manual mid-wake")
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		// Pending centroid refreshes are left alone: they're quick and keep
		// artist/album centroids consistent with the tracks already analyzed.
		cancelled, _ := queueops.CancelPendingByScheduledTask(ctx, w.DB, taskID, []string{kind})
		log.Info().Str("task", taskID).Str("reason", reason).Int64("cancelled_pending", cancelled).Msg("kickoff_sonic_analysis: winding down")
		finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
		return nil
	}

	rc := river.ClientFromContext[pgx.Tx](ctx)
	active, err := pumpActiveCount(ctx, w.DB, taskID, kind)
	if err != nil {
		return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
	}
	listed := -1 // -1: wave full, sweep not consulted this wake
	if want := sonicKickoffBatch - active; want > 0 {
		ids, err := q.ListPendingAnalysisTracks(ctx, sqlc.ListPendingAnalysisTracksParams{
			AfterID:            st.TrackCursor,
			MaxDurationSeconds: sonicanalysis.MaxAnalysisDurationSeconds,
			AnalyzerVersion:    sonicanalysis.AnalyzerVersion,
			LimitCount:         int32(want),
		})
		if err != nil {
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		}
		listed = len(ids)
		if len(ids) > 0 {
			w.Progress.Set("analyze_music_facets", "kickoff_sonic_analysis", "queueing tracks…")
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
			}
			res, err := rc.Insert(ctx, AnalyzeTrackFacetsArgs{TrackID: id, ScheduledTaskID: taskID}, nil)
			switch {
			case err != nil:
				log.Warn().Err(err).Int64("track_id", id).Msg("kickoff_sonic_analysis: enqueue failed")
				st.Failed++
				st.Skipped++
			case res.UniqueSkippedAsDuplicate:
				st.Skipped++
			default:
				st.Enqueued++
			}
			st.TrackCursor = id
		}
	}
	if active == 0 && listed == 0 {
		if st.restartSweep() {
			log.Info().Str("task", taskID).Msg("kickoff_sonic_analysis: re-sweeping for items skipped during the run")
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
		case err != nil:
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		case !proceed:
			log.Info().Str("task", taskID).Msg("kickoff_sonic_analysis: finish aborted — run upgraded to manual mid-wake")
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		// Centroid refreshes cascade from the per-track jobs and are quick;
		// the run doesn't wait on them.
		log.Info().Str("task", taskID).Int("enqueued", st.Enqueued).Int("failed", st.Failed).Msg("kickoff_sonic_analysis: backlog drained")
		finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
		return nil
	}

	st.ErrStreak = 0
	return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
}
