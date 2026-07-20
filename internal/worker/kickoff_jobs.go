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

// scannerQueueMediaTypes is the complete set of library domains that can own
// an independent scanner queue. Some domains do not have a scanner
// implementation yet, but reserving their queues keeps the routing model
// future-proof and lets their kickoff inventory work stay isolated today.
var scannerQueueMediaTypes = []sqlc.MediaType{
	sqlc.MediaTypeMovie,
	sqlc.MediaTypeTv,
	sqlc.MediaTypeAnime,
	sqlc.MediaTypeMusic,
	sqlc.MediaTypeBook,
	sqlc.MediaTypeComic,
	sqlc.MediaTypePodcast,
	sqlc.MediaTypeRadio,
}

var scannerQueueKinds = []string{
	"kickoff_library_scan",
	"process_scan",
	"search_metadata",
	"fetch_metadata",
	"apply_metadata",
	"apply_rich_metadata",
}

// scannerQueueName isolates the scanner pipeline by library media type. The
// unsuffixed queue remains the safe fallback for scan-all coordination,
// malformed/legacy payloads, and media types introduced by a newer database.
func scannerQueueName(kind string, mediaType sqlc.MediaType) string {
	for _, supported := range scannerQueueMediaTypes {
		if mediaType == supported {
			return kind + "_" + string(mediaType)
		}
	}
	return kind
}

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

func renameLegacyScannerJobs(ctx context.Context, db *pgxpool.Pool) error {
	renames := map[string]string{
		"process_library_scan":   "process_scan",
		"fetch_library_metadata": "fetch_metadata",
		"apply_library_scan":     "apply_metadata",
	}
	for oldKind, newKind := range renames {
		tag, err := db.Exec(ctx, `
			UPDATE river_job
			   SET kind = $2,
			       queue = CASE WHEN queue = $1 THEN $2 ELSE queue END
			 WHERE kind = $1 OR queue = $1
		`, oldKind, newKind)
		if err != nil {
			return err
		}
		if n := tag.RowsAffected(); n > 0 {
			log.Info().Str("old", oldKind).Str("new", newKind).Int64("rows", n).Msg("renamed legacy scanner jobs")
		}
	}

	// Jobs already waiting when this version deploys were inserted on the old
	// shared stage queues. Move active work to its media-type queue immediately
	// so a large pre-deploy Music backlog cannot continue starving Anime/TV.
	// Completed history stays untouched.
	tag, err := db.Exec(ctx, `
		UPDATE river_job AS rj
		   SET queue = rj.kind ||
		       CASE
		         WHEN rj.kind IN ('search_metadata', 'fetch_metadata')
		          AND COALESCE(NULLIF(rj.args->>'poll', '')::boolean, false)
		         THEN '_poll_'
		         ELSE '_'
		       END || l.media_type::text
		  FROM libraries AS l
		 WHERE rj.kind = ANY($1::text[])
		   AND rj.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
		   AND NULLIF(rj.args->>'library_id', '')::bigint = l.id
		   AND l.media_type::text = ANY($2::text[])
		   AND rj.queue IS DISTINCT FROM rj.kind ||
		       CASE
		         WHEN rj.kind IN ('search_metadata', 'fetch_metadata')
		          AND COALESCE(NULLIF(rj.args->>'poll', '')::boolean, false)
		         THEN '_poll_'
		         ELSE '_'
		       END || l.media_type::text
	`, scannerQueueKinds, scannerQueueMediaTypeStrings())
	if err != nil {
		return err
	}
	if n := tag.RowsAffected(); n > 0 {
		log.Info().Int64("rows", n).Msg("routed active scanner jobs to media-type queues")
	}
	return nil
}

func scannerQueueMediaTypeStrings() []string {
	values := make([]string, 0, len(scannerQueueMediaTypes))
	for _, mediaType := range scannerQueueMediaTypes {
		values = append(values, string(mediaType))
	}
	return values
}

// KickoffLibraryScanArgs replaces scheduler.ScanLibrariesTask. It is the fast
// inventory/change-detection front door: walk every library (or one library),
// skip unchanged inputs, soft-delete missing inputs, and enqueue
// ProcessLibraryScanArgs for changed scopes.
type KickoffLibraryScanArgs struct {
	LibraryID       int64          `json:"library_id,omitempty"` // 0 = all libraries
	MediaType       sqlc.MediaType `json:"media_type,omitempty"`
	Force           bool           `json:"force,omitempty"`
	ScheduledTaskID string         `json:"scheduled_task_id,omitempty"`
}

func (KickoffLibraryScanArgs) Kind() string { return "kickoff_library_scan" }
func (a KickoffLibraryScanArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       scannerQueueName("kickoff_library_scan", a.MediaType),
		MaxAttempts: 1,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// ProcessLibraryScanArgs runs only local filesystem analysis. It persists one
// narrow analysis artifact per owner entity, then delegates remote canonical
// search to the high-concurrency search_metadata queues.
//
// When ScopePaths is present, each value is treated as a directory scope; the
// scanner still walks the library roots but only analyzes files under those
// scopes so a watcher event can process one movie/show/album folder with its
// sidecars.
type ProcessLibraryScanArgs struct {
	LibraryID       int64          `json:"library_id" river:"unique"`
	MediaType       sqlc.MediaType `json:"media_type,omitempty"`
	ScopePaths      []string       `json:"scope_paths,omitempty" river:"unique"`
	Force           bool           `json:"force,omitempty"`
	ScheduledTaskID string         `json:"scheduled_task_id,omitempty"`
}

func (ProcessLibraryScanArgs) Kind() string { return "process_scan" }
func (a ProcessLibraryScanArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       scannerQueueName("process_scan", a.MediaType),
		MaxAttempts: 3,
		Priority:    PriorityScan,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// SearchLibraryMetadataArgs resumes one persisted local analysis artifact and
// performs canonical index search/discovery. Deferred remote workflows are
// parked in scanner_metadata_continuations; a bounded sweeper promotes only
// due checks onto the poll queues. Poll participates in uniqueness so a lease
// expiry cannot stack a duplicate while an earlier check is still active.
type SearchLibraryMetadataArgs struct {
	LibraryID          int64          `json:"library_id" river:"unique"`
	MediaType          sqlc.MediaType `json:"media_type,omitempty"`
	ScopePaths         []string       `json:"scope_paths,omitempty" river:"unique"`
	ScannerEntityID    int64          `json:"scanner_entity_id" river:"unique"`
	AnalysisArtifactID int64          `json:"analysis_artifact_id" river:"unique"`
	Poll               bool           `json:"poll,omitempty" river:"unique"`
	Force              bool           `json:"force,omitempty"`
	ScheduledTaskID    string         `json:"scheduled_task_id,omitempty"`
}

func (SearchLibraryMetadataArgs) Kind() string { return "search_metadata" }
func (a SearchLibraryMetadataArgs) InsertOpts() river.InsertOpts {
	queue := "search_metadata"
	if a.Poll {
		queue = "search_metadata_poll"
	}
	return river.InsertOpts{
		Queue:       scannerQueueName(queue, a.MediaType),
		MaxAttempts: 3,
		Priority:    PriorityScan,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// FetchLibraryMetadataArgs resumes a persisted search result, fetches remote
// metadata, and persists a fetch artifact for the apply phase. Asynchronous
// waits are parked outside River and promoted to the corresponding
// fetch_metadata_poll queue in bounded batches.
type FetchLibraryMetadataArgs struct {
	LibraryID        int64          `json:"library_id" river:"unique"`
	MediaType        sqlc.MediaType `json:"media_type,omitempty"`
	ScopePaths       []string       `json:"scope_paths,omitempty" river:"unique"`
	ScannerEntityID  int64          `json:"scanner_entity_id" river:"unique"`
	SearchArtifactID int64          `json:"search_artifact_id" river:"unique"`
	Poll             bool           `json:"poll,omitempty" river:"unique"`
	Force            bool           `json:"force,omitempty"`
	ScheduledTaskID  string         `json:"scheduled_task_id,omitempty"`
}

func (FetchLibraryMetadataArgs) Kind() string { return "fetch_metadata" }
func (a FetchLibraryMetadataArgs) InsertOpts() river.InsertOpts {
	queue := "fetch_metadata"
	if a.Poll {
		queue = "fetch_metadata_poll"
	}
	return river.InsertOpts{
		Queue:       scannerQueueName(queue, a.MediaType),
		MaxAttempts: 3,
		Priority:    PriorityScan,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// ApplyLibraryScanArgs resumes one persisted entity metadata artifact,
// materializes it, applies it to the database, then fans out file side effects.
// The queue payload stays intentionally small; the rich scanner state lives in
// scanner_entity_artifacts.
type ApplyLibraryScanArgs struct {
	LibraryID          int64          `json:"library_id" river:"unique"`
	MediaType          sqlc.MediaType `json:"media_type,omitempty"`
	ScopePaths         []string       `json:"scope_paths,omitempty" river:"unique"`
	ScannerEntityID    int64          `json:"scanner_entity_id" river:"unique"`
	MetadataArtifactID int64          `json:"metadata_artifact_id" river:"unique"`
	Force              bool           `json:"force,omitempty"`
	ScheduledTaskID    string         `json:"scheduled_task_id,omitempty"`
}

func (ApplyLibraryScanArgs) Kind() string { return "apply_metadata" }
func (a ApplyLibraryScanArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       scannerQueueName("apply_metadata", a.MediaType),
		MaxAttempts: 3,
		Priority:    PriorityScan,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// ApplyRichMetadataArgs resumes the scanner fetch artifact for one applied
// entity and persists slow side-data (cast, crew, keywords, videos,
// certifications, recommendations, collections) outside the core apply path.
type ApplyRichMetadataArgs struct {
	LibraryID          int64          `json:"library_id" river:"unique"`
	MediaItemID        int64          `json:"media_item_id" river:"unique"`
	ScannerEntityID    int64          `json:"scanner_entity_id,omitempty" river:"unique"`
	MetadataArtifactID int64          `json:"metadata_artifact_id" river:"unique"`
	MediaType          sqlc.MediaType `json:"media_type,omitempty"`
	MediaKind          string         `json:"media_kind" river:"unique"`
	Key                string         `json:"key,omitempty" river:"unique"`
	ScheduledTaskID    string         `json:"scheduled_task_id,omitempty"`
}

func (ApplyRichMetadataArgs) Kind() string { return "apply_rich_metadata" }
func (a ApplyRichMetadataArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       scannerQueueName("apply_rich_metadata", a.MediaType),
		MaxAttempts: 5,
		Priority:    PriorityScan,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// KickoffRefreshStaleArgs remains decodable only to drain jobs queued by a
// pre-V2 binary. The worker is a no-op; HeyaMetadata freshness and the V2
// change cursor own automatic refresh after migration 00031.
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
// Finds extra library-file links missing thumbnail_path and enqueues one
// thumbnail_extra job per extra. Extra thumbnails are cheap fallback metadata,
// so they are no longer gated by a per-library toggle.
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

// CleanupScannerArtifactsArgs compacts scanner handoff blobs after successful
// materialization. Immediate apply/rich-apply paths do the same for fresh work;
// this scheduled backstop clears artifacts from older deployments and CLI runs.
type CleanupScannerArtifactsArgs struct {
	RetentionDays   int32  `json:"retention_days,omitempty"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (CleanupScannerArtifactsArgs) Kind() string { return "cleanup_scanner_artifacts" }
func (CleanupScannerArtifactsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "cleanup_scanner_artifacts",
		MaxAttempts: 1,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// KickoffEmbedRecommendationsArgs sweeps the recommendation embeddings: any
// item, episode, or canonical music recording whose stored doc hash no longer
// matches its recomposed metadata doc re-embeds.
// No-ops quickly when the embedding engine is disabled or nothing changed.
type KickoffEmbedRecommendationsArgs struct {
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (KickoffEmbedRecommendationsArgs) Kind() string { return "kickoff_embed_recommendations" }
func (KickoffEmbedRecommendationsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_embed_recommendations",
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
