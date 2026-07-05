package worker

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/taskdefs"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/rs/zerolog/log"
)

// Kickoff Args / InsertOpts live here so the dispatch table (kind →
// kickoff Args constructor) used by the scheduler trigger loop is a
// quick read.
//
// Each kickoff is on its own queue at MaxWorkers=1 with UniqueByArgs,
// so a second "Run Now" click while one is queued or running is a
// no-op rather than a stacked re-run.

// uniqueWhileActive deduplicates a kickoff only while an identical one is
// still queued, scheduled, retrying, or running. River's default ByState
// for ByArgs uniqueness *also* includes JobStateCompleted, which means a
// finished kickoff keeps blocking re-inserts until the job-cleaner
// maintenance process removes the completed row (~24h retention). That
// made a manual "Scan" / "Run Now" silently no-op for hours after a
// successful run. Dropping JobStateCompleted restores the intended
// behavior: coalesce in-flight clicks, but always re-runnable once done.
// River requires Available/Pending/Running/Scheduled; Retryable is kept
// because a scan mid-retry is still in flight.
func uniqueWhileActive() river.UniqueOpts {
	return river.UniqueOpts{
		ByArgs: true,
		ByState: []rivertype.JobState{
			rivertype.JobStateAvailable,
			rivertype.JobStatePending,
			rivertype.JobStateRunning,
			rivertype.JobStateRetryable,
			rivertype.JobStateScheduled,
		},
	}
}

// clearStaleUniqueJobStates rewrites the unique-state bitmask of every job
// inserted under the OLD default ByArgs bitmask (which included the
// `completed` state) to match uniqueWhileActive(), which all unique jobs now
// use.
//
// Without it, a job that completed *before* this build deployed keeps its
// completed row in river_job_unique_idx and blocks an identical re-insert
// until River's job cleaner ages it out (~24h retention) — so the fix
// wouldn't bite until the next day.
//
// The fix is to clear *only* the completed bit (position 5, matching
// River's river_job_state_in_bitmask). The old default bitmask is exactly
// the uniqueWhileActive() bitmask plus that bit, so `set_bit(..., 5, 0)`
// rewrites an old row into precisely what the new code would have inserted:
//   - a still-active pre-fix job keeps its available/pending/running/
//     scheduled bits, so it stays in the index and DOESN'T lose in-flight
//     uniqueness during cleanup (a plain NULL would have — letting a
//     duplicate stack mid-deploy);
//   - a completed pre-fix job drops out of the index and unblocks;
//   - an active job that completes *after* deploy now carries the new
//     bitmask, so it won't re-block either.
//
// Kind-agnostic on purpose: now that no Heya job dedups against `completed`,
// a set completed bit unambiguously marks a pre-fix row. New inserts clear
// it, so this is idempotent and self-limiting — after one pass nothing
// matches. Jobs with no uniqueness have NULL unique_states and are skipped.
func clearStaleUniqueJobStates(ctx context.Context, db *pgxpool.Pool) error {
	tag, err := db.Exec(ctx, `
		UPDATE river_job
		SET unique_states = set_bit(unique_states, 5, 0)
		WHERE unique_states IS NOT NULL
		  AND get_bit(unique_states, 5) = 1`)
	if err != nil {
		return err
	}
	if n := tag.RowsAffected(); n > 0 {
		log.Info().Int64("rows", n).Msg("migrated pre-fix jobs to uniqueWhileActive bitmask")
	}
	return nil
}

// KickoffLibraryScanArgs replaces scheduler.ScanLibrariesTask. Walks
// every library (or a specific one when LibraryID > 0) via the scanner,
// fans out one ProcessFile job per discovered pending file.
type KickoffLibraryScanArgs struct {
	LibraryID       int64  `json:"library_id,omitempty"` // 0 = all libraries
	Force           bool   `json:"force,omitempty"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (KickoffLibraryScanArgs) Kind() string { return "kickoff_library_scan" }
func (KickoffLibraryScanArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_library_scan",
		MaxAttempts: 1,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// KickoffRefreshStaleArgs replaces scheduler.RefreshStaleItemsTask.
// Finds every media_item past its library's MetadataRefreshDays window
// and enqueues an enrich_media_item job per item.
type KickoffRefreshStaleArgs struct {
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (KickoffRefreshStaleArgs) Kind() string { return "kickoff_refresh_stale" }
func (KickoffRefreshStaleArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_refresh_stale",
		MaxAttempts: 1,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// KickoffMusicLoudnessArgs replaces scheduler.ScanMusicLoudnessTask.
// Enqueues scan_track_loudness for tracks missing LUFS and
// scan_album_loudness for albums whose tracks have all been measured.
type KickoffMusicLoudnessArgs struct {
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (KickoffMusicLoudnessArgs) Kind() string { return "kickoff_music_loudness" }
func (KickoffMusicLoudnessArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_music_loudness",
		MaxAttempts: 1,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// KickoffMusicFingerprintArgs enqueues scan_track_fingerprint for music
// files missing a chromaprint. Same snooze-loop pump shape as loudness,
// single phase (no album-level aggregation).
type KickoffMusicFingerprintArgs struct {
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (KickoffMusicFingerprintArgs) Kind() string { return "kickoff_music_fingerprint" }
func (KickoffMusicFingerprintArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_music_fingerprint",
		MaxAttempts: 1,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// KickoffMediaSegmentsArgs enqueues scan_media_segments_file for
// movie/episode files without a segments pass. Same single-phase
// snooze-loop pump shape as the fingerprint kickoff.
type KickoffMediaSegmentsArgs struct {
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (KickoffMediaSegmentsArgs) Kind() string { return "kickoff_media_segments" }
func (KickoffMediaSegmentsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_media_segments",
		MaxAttempts: 1,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// KickoffDetectSegmentsArgs enqueues detect_segments_season /
// detect_segments_movie for files the community pump already checked
// (segments_analyzed_at set) but couldn't resolve on its own
// (segments_detected_at NULL). Two-cursor pump shape like
// KickoffMusicLoudnessArgs: seasons sweep first via TrackCursor (heavier —
// cross-episode audio decode), then movie files via AlbumCursor.
type KickoffDetectSegmentsArgs struct {
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (KickoffDetectSegmentsArgs) Kind() string { return "kickoff_detect_segments" }
func (KickoffDetectSegmentsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_detect_segments",
		MaxAttempts: 1,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// KickoffTrickplayArgs replaces scheduler.GenerateTrickplayTask.
// Finds library_files with has_trickplay=false on a library where
// enable_trickplay is set, and enqueues one trickplay_file job per
// candidate.
type KickoffTrickplayArgs struct {
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (KickoffTrickplayArgs) Kind() string { return "kickoff_trickplay" }
func (KickoffTrickplayArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_trickplay",
		MaxAttempts: 1,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// KickoffThumbnailsArgs replaces scheduler.GenerateThumbnailsTask.
// Finds media_extras missing thumbnail_path on a library where
// generate_thumbnails is set, and enqueues one thumbnail_extra job
// per extra.
type KickoffThumbnailsArgs struct {
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (KickoffThumbnailsArgs) Kind() string { return "kickoff_thumbnails" }
func (KickoffThumbnailsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_thumbnails",
		MaxAttempts: 1,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// KickoffSonicAnalysisArgs replaces scheduler.AnalyzeMusicTask. Finds
// tracks with no facets row (or whose row is below AnalyzerVersion) and
// enqueues one analyze_track_facets job per candidate. The kickoff bails
// fast when sonic analysis is disabled in settings — the per-track jobs
// still run if the user re-enables and re-triggers.
type KickoffSonicAnalysisArgs struct {
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (KickoffSonicAnalysisArgs) Kind() string { return "kickoff_sonic_analysis" }
func (KickoffSonicAnalysisArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_sonic_analysis",
		MaxAttempts: 1,
		UniqueOpts:  uniqueWhileActive(),
	}
}

func TaskKinds(taskID string) []string {
	return taskdefs.TaskKinds(taskID)
}

func computeNextRun(startedAt time.Time, intervalHours int32, dailyStartTime, dailyEndTime string) time.Time {
	if intervalHours < 1 {
		intervalHours = 24
	}
	candidate := startedAt.Add(time.Duration(intervalHours) * time.Hour)
	if inTaskWindow(candidate, dailyStartTime, dailyEndTime) {
		return candidate
	}
	return nextTaskWindowStartAfter(candidate, dailyStartTime)
}

func nextTaskWindowStartAfter(now time.Time, dailyStartTime string) time.Time {
	start, err := time.Parse("15:04", dailyStartTime)
	if err != nil {
		return now.Add(24 * time.Hour)
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), start.Hour(), start.Minute(), 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func inTaskWindow(now time.Time, startStr, endStr string) bool {
	start, err := time.Parse("15:04", startStr)
	if err != nil {
		return false
	}
	end, err := time.Parse("15:04", endStr)
	if err != nil {
		return false
	}
	nowM := now.Hour()*60 + now.Minute()
	startM := start.Hour()*60 + start.Minute()
	endM := end.Hour()*60 + end.Minute()
	if endM > startM {
		return nowM >= startM && nowM < endM
	}
	return nowM >= startM || nowM < endM
}

// finishKickoff updates the scheduled_tasks row that owns the given
// kickoff kind. Called by every kickoff worker on completion (success
// or failure) so the tasks page reflects when the kickoff last fired
// and how many work jobs it enqueued.
//
// items is the count of work jobs successfully enqueued (failures are
// NOT included — callers count those separately in itemsFailed); if
// itemsFailed > 0 the result is "partial".
func finishKickoff(ctx context.Context, q *sqlc.Queries, taskID string, startedAt time.Time, items, itemsFailed int, runErr error) {
	if taskID == "" {
		return
	}
	writeCtx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		writeCtx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}
	completedAt := time.Now()
	dbTask, err := q.GetScheduledTask(writeCtx, taskID)
	if err != nil {
		log.Warn().Err(err).Str("task", taskID).Msg("kickoff: load scheduled_tasks row failed")
		return
	}

	result := "completed"
	switch {
	case runErr != nil:
		result = "error"
	case itemsFailed > 0 && itemsFailed >= items:
		result = "error"
	case itemsFailed > 0:
		result = "partial"
	}

	nextRun := computeNextRun(completedAt, dbTask.IntervalHours, dbTask.DailyStartTime, dbTask.DailyEndTime)
	if err := q.UpdateScheduledTaskRun(writeCtx, sqlc.UpdateScheduledTaskRunParams{
		ID:                    taskID,
		LastRunAt:             pgtype.Timestamptz{Time: startedAt, Valid: true},
		LastRunResult:         result,
		LastRunDurationSec:    int32(completedAt.Sub(startedAt).Seconds()),
		LastRunItemsProcessed: int32(items),
		LastRunItemsTotal:     int32(items + itemsFailed),
		NextRunAt:             pgtype.Timestamptz{Time: nextRun, Valid: true},
	}); err != nil {
		log.Warn().Err(err).Str("task", taskID).Msg("kickoff: update scheduled_tasks row failed")
	}
}
