package worker

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// Kickoff Args / InsertOpts live here so the dispatch table (kind →
// kickoff Args constructor) used by the scheduler trigger loop is a
// quick read.
//
// Each kickoff is on its own queue at MaxWorkers=1 with UniqueByArgs,
// so a second "Run Now" click while one is queued or running is a
// no-op rather than a stacked re-run.

// KickoffLibraryScanArgs replaces scheduler.ScanLibrariesTask. Walks
// every library (or a specific one when LibraryID > 0) via the scanner,
// fans out one ProcessFile job per discovered pending file.
type KickoffLibraryScanArgs struct {
	LibraryID int64 `json:"library_id,omitempty"` // 0 = all libraries
	Force     bool  `json:"force,omitempty"`
}

func (KickoffLibraryScanArgs) Kind() string { return "kickoff_library_scan" }
func (KickoffLibraryScanArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_library_scan",
		MaxAttempts: 1,
		UniqueOpts:  river.UniqueOpts{ByArgs: true},
	}
}

// KickoffRefreshStaleArgs replaces scheduler.RefreshStaleItemsTask.
// Finds every media_item past its library's MetadataRefreshDays window
// and enqueues an enrich_media_item job per item.
type KickoffRefreshStaleArgs struct{}

func (KickoffRefreshStaleArgs) Kind() string { return "kickoff_refresh_stale" }
func (KickoffRefreshStaleArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_refresh_stale",
		MaxAttempts: 1,
		UniqueOpts:  river.UniqueOpts{ByArgs: true},
	}
}

// KickoffMusicLoudnessArgs replaces scheduler.ScanMusicLoudnessTask.
// Enqueues scan_track_loudness for tracks missing LUFS and
// scan_album_loudness for albums whose tracks have all been measured.
type KickoffMusicLoudnessArgs struct{}

func (KickoffMusicLoudnessArgs) Kind() string { return "kickoff_music_loudness" }
func (KickoffMusicLoudnessArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_music_loudness",
		MaxAttempts: 1,
		UniqueOpts:  river.UniqueOpts{ByArgs: true},
	}
}

// KickoffTrickplayArgs replaces scheduler.GenerateTrickplayTask.
// Finds library_files with has_trickplay=false on a library where
// enable_trickplay is set, and enqueues one trickplay_file job per
// candidate.
type KickoffTrickplayArgs struct{}

func (KickoffTrickplayArgs) Kind() string { return "kickoff_trickplay" }
func (KickoffTrickplayArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_trickplay",
		MaxAttempts: 1,
		UniqueOpts:  river.UniqueOpts{ByArgs: true},
	}
}

// KickoffThumbnailsArgs replaces scheduler.GenerateThumbnailsTask.
// Finds media_extras missing thumbnail_path on a library where
// generate_thumbnails is set, and enqueues one thumbnail_extra job
// per extra.
type KickoffThumbnailsArgs struct{}

func (KickoffThumbnailsArgs) Kind() string { return "kickoff_thumbnails" }
func (KickoffThumbnailsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_thumbnails",
		MaxAttempts: 1,
		UniqueOpts:  river.UniqueOpts{ByArgs: true},
	}
}

// KickoffSonicAnalysisArgs replaces scheduler.AnalyzeMusicTask. Finds
// tracks with no facets row (or whose row is below AnalyzerVersion) and
// enqueues one analyze_track_facets job per candidate. The kickoff bails
// fast when sonic analysis is disabled in settings — the per-track jobs
// still run if the user re-enables and re-triggers.
type KickoffSonicAnalysisArgs struct{}

func (KickoffSonicAnalysisArgs) Kind() string { return "kickoff_sonic_analysis" }
func (KickoffSonicAnalysisArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_sonic_analysis",
		MaxAttempts: 1,
		UniqueOpts:  river.UniqueOpts{ByArgs: true},
	}
}

// kickoffTaskIDs maps each kickoff job kind to the scheduled_tasks.id
// it represents. Used by the kickoff workers to stamp last_run_* and by
// the cancel-task handler to derive the set of kinds to cancel.
var kickoffTaskIDs = map[string]string{
	"kickoff_library_scan":   "scan_libraries",
	"kickoff_refresh_stale":  "refresh_stale_items",
	"kickoff_music_loudness": "scan_music_loudness",
	"kickoff_trickplay":      "generate_trickplay",
	"kickoff_thumbnails":     "generate_thumbnails",
	"kickoff_sonic_analysis": "analyze_music_facets",
}

// TaskKinds returns every River kind associated with a task — the
// kickoff plus the work workers it fans out into. Used by
// service.CancelJobsByKind for /api/tasks/{id}/cancel, the tasks-page
// status query, and the progress broadcaster's reverse lookup.
//
// Two groups of task IDs:
//
//   - Scheduled tasks (rows in scheduled_tasks): scan_libraries,
//     refresh_stale_items, scan_music_loudness, generate_trickplay,
//     generate_thumbnails, analyze_music_facets. Driven by the cron
//     trigger. Listed on /api/tasks. Cancellable via the UI.
//   - Synthetic buckets (no DB row): transcoding, artwork, nfo_writes,
//     external_lookups, refresh_actions, cleanup. Group ad-hoc workers
//     (download_image, transcode, etc.) so they show up as labelled
//     cards in the Activity dropdown instead of nameless counts. Not
//     listed on /api/tasks (the page lists scheduled rows only) and
//     not cancellable from the tasks page enum.
func TaskKinds(taskID string) []string {
	switch taskID {
	// Scheduled tasks.
	case "scan_libraries":
		return []string{"kickoff_library_scan", "process_file", "ffprobe", "detect_local_assets", "metadata_match"}
	case "refresh_stale_items":
		return []string{"kickoff_refresh_stale", "enrich_media_item"}
	case "scan_music_loudness":
		return []string{"kickoff_music_loudness", "scan_track_loudness", "scan_album_loudness"}
	case "generate_trickplay":
		return []string{"kickoff_trickplay", "trickplay_file"}
	case "generate_thumbnails":
		return []string{"kickoff_thumbnails", "thumbnail_extra"}
	case "analyze_music_facets":
		return []string{"kickoff_sonic_analysis", "analyze_track_facets", "refresh_artist_centroids", "refresh_album_centroids"}
	// Synthetic buckets.
	case "transcoding":
		return []string{"transcode"}
	case "artwork":
		return []string{"download_image", "fetch_artwork", "save_images"}
	case "nfo_writes":
		return []string{"save_nfo", "save_music_nfo"}
	case "external_lookups":
		return []string{"person_fetch", "ratings_fetch"}
	case "refresh_actions":
		return []string{"force_refresh_metadata", "force_refresh_images"}
	case "cleanup":
		return []string{"soft_delete"}
	}
	return nil
}

// computeNextRun returns the next occurrence of dailyStartTime after
// now. Mirrors the old scheduler.Runner.computeNextRun behaviour so the
// scheduling cadence doesn't change with the kickoff cutover.
func computeNextRun(dailyStartTime string) time.Time {
	now := time.Now()
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

// finishKickoff updates the scheduled_tasks row that owns the given
// kickoff kind. Called by every kickoff worker on completion (success
// or failure) so the tasks page reflects when the kickoff last fired
// and how many work jobs it enqueued.
//
// items is the count of work jobs successfully enqueued; if itemsFailed
// > 0 the result is "partial".
func finishKickoff(ctx context.Context, q *sqlc.Queries, kind string, startedAt time.Time, items, itemsFailed int, runErr error) {
	taskID, ok := kickoffTaskIDs[kind]
	if !ok {
		return
	}
	dbTask, err := q.GetScheduledTask(ctx, taskID)
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

	nextRun := computeNextRun(dbTask.DailyStartTime)
	if err := q.UpdateScheduledTaskRun(ctx, sqlc.UpdateScheduledTaskRunParams{
		ID:                    taskID,
		LastRunAt:             pgtype.Timestamptz{Time: startedAt, Valid: true},
		LastRunResult:         result,
		LastRunDurationSec:    int32(time.Since(startedAt).Seconds()),
		LastRunItemsProcessed: int32(items - itemsFailed),
		LastRunItemsTotal:     int32(items),
		NextRunAt:             pgtype.Timestamptz{Time: nextRun, Valid: true},
	}); err != nil {
		log.Warn().Err(err).Str("task", taskID).Msg("kickoff: update scheduled_tasks row failed")
	}
}
