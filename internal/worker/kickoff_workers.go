package worker

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/mediafile"
	"github.com/karbowiak/heya/internal/metadata"
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
// kickoff_music_loudness
// ---------------------------------------------------------------------------

// Per-tick caps. The scan_track_loudness queue is MaxWorkers=1 so it'll
// chew through the backlog at ~30s/track regardless. Bounding the
// enqueue keeps the River job table from ballooning on a fresh import.
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
	startedAt := time.Now()
	taskID := job.Args.ScheduledTaskID
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)

	tracks, err := q.ListTrackFilesPendingLoudness(ctx, kickoffLoudnessTrackBatch)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, 0, 0, err)
		return err
	}
	albums, err := q.ListAlbumsPendingLoudness(ctx, kickoffLoudnessAlbumBatch)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, 0, 0, err)
		return err
	}

	enqueued, failed := 0, 0
	for _, row := range tracks {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, err)
			return err
		}
		w.Progress.Set("scan_music_loudness", "kickoff_music_loudness", row.Path)
		if _, err := rc.Insert(ctx, ScanTrackLoudnessArgs{TrackFileID: row.ID, ScheduledTaskID: taskID}, nil); err != nil {
			log.Warn().Err(err).Int64("track_file_id", row.ID).Msg("kickoff_music_loudness: enqueue track failed")
			failed++
			continue
		}
		enqueued++
	}
	for _, row := range albums {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, err)
			return err
		}
		w.Progress.Set("scan_music_loudness", "kickoff_music_loudness", row.Title)
		if _, err := rc.Insert(ctx, ScanAlbumLoudnessArgs{AlbumID: row.ID, ScheduledTaskID: taskID}, nil); err != nil {
			log.Warn().Err(err).Int64("album_id", row.ID).Msg("kickoff_music_loudness: enqueue album failed")
			failed++
			continue
		}
		enqueued++
	}

	if enqueued > 0 {
		log.Info().Int("enqueued", enqueued).Msg("kickoff_music_loudness: jobs enqueued")
	}
	finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, nil)
	return nil
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

// sonicKickoffBatch caps how many tracks one kickoff enqueues so a fresh
// 100k-track library doesn't dump 100k jobs into River in one shot.
// Subsequent kickoffs (next cron firing or another Run Now click after
// the batch drains) will pick up the remainder.
const sonicKickoffBatch = 1000

type KickoffSonicAnalysisWorker struct {
	river.WorkerDefaults[KickoffSonicAnalysisArgs]
	DB       *pgxpool.Pool
	Enabled  SonicEnabledFn
	Progress *TaskProgressBroadcaster
}

func (w *KickoffSonicAnalysisWorker) Work(ctx context.Context, job *river.Job[KickoffSonicAnalysisArgs]) error {
	startedAt := time.Now()
	taskID := job.Args.ScheduledTaskID
	q := sqlc.New(w.DB)

	if w.Enabled != nil && !w.Enabled(ctx) {
		log.Info().Msg("kickoff_sonic_analysis: skipped — disabled in settings")
		finishKickoff(ctx, q, taskID, startedAt, 0, 0, nil)
		return nil
	}

	rc := river.ClientFromContext[pgx.Tx](ctx)
	ids, err := q.ListPendingAnalysisTracks(ctx, sqlc.ListPendingAnalysisTracksParams{
		MaxDurationSeconds: sonicanalysis.MaxAnalysisDurationSeconds,
		AnalyzerVersion:    sonicanalysis.AnalyzerVersion,
		LimitCount:         sonicKickoffBatch,
	})
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, 0, 0, err)
		return err
	}

	enqueued, failed := 0, 0
	w.Progress.Set("analyze_music_facets", "kickoff_sonic_analysis", "queueing tracks…")
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, err)
			return err
		}
		if _, err := rc.Insert(ctx, AnalyzeTrackFacetsArgs{TrackID: id, ScheduledTaskID: taskID}, nil); err != nil {
			log.Warn().Err(err).Int64("track_id", id).Msg("kickoff_sonic_analysis: enqueue failed")
			failed++
			continue
		}
		enqueued++
	}

	finishKickoff(ctx, q, taskID, startedAt, enqueued, failed, nil)
	return nil
}
