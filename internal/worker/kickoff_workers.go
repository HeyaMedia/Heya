package worker

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
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
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)

	var libs []sqlc.Library
	var err error
	if job.Args.LibraryID > 0 {
		lib, gErr := q.GetLibraryByID(ctx, job.Args.LibraryID)
		if gErr != nil {
			finishKickoff(ctx, q, "kickoff_library_scan", startedAt, 0, 0, gErr)
			return gErr
		}
		libs = []sqlc.Library{lib}
	} else {
		libs, err = q.ListLibraries(ctx)
		if err != nil {
			finishKickoff(ctx, q, "kickoff_library_scan", startedAt, 0, 0, err)
			return err
		}
		sortLibrariesByMediaPriority(libs)
	}

	s := scanner.New(w.DB)
	enqueued := 0
	failed := 0

	for _, lib := range libs {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, "kickoff_library_scan", startedAt, enqueued, failed, err)
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
			continue
		}

		n := enqueuePendingFiles(ctx, q, rc, lib.ID)
		enqueued += n

		log.Info().
			Int64("library_id", lib.ID).
			Int("discovered", result.Discovered).
			Int("new", result.New).
			Int("deleted", result.Deleted).
			Int("enqueued", n).
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

	finishKickoff(ctx, q, "kickoff_library_scan", startedAt, enqueued, failed, nil)
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

func enqueuePendingFiles(ctx context.Context, q *sqlc.Queries, rc *river.Client[pgx.Tx], libraryID int64) int {
	files, err := q.ListLibraryFilesByStatus(ctx, sqlc.ListLibraryFilesByStatusParams{
		LibraryID: libraryID,
		Status:    sqlc.FileStatusPending,
		Limit:     100000,
		Offset:    0,
	})
	if err != nil {
		log.Error().Err(err).Int64("library_id", libraryID).Msg("kickoff_library_scan: list pending failed")
		return 0
	}
	if rc == nil {
		return 0
	}
	for _, f := range files {
		if _, err := rc.Insert(ctx, ProcessFileArgs{
			LibraryFileID: f.ID,
			LibraryID:     libraryID,
			FilePath:      f.Path,
		}, nil); err != nil {
			log.Warn().Err(err).Int64("file_id", f.ID).Msg("kickoff_library_scan: enqueue process_file failed")
		}
	}
	return len(files)
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
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)

	rows, err := w.DB.Query(ctx, `
		SELECT mi.id, mi.media_type, mi.title, l.settings, mi.metadata_refreshed_at
		FROM media_items mi
		JOIN libraries l ON l.id = mi.library_id
		WHERE mi.media_type = 'music' OR mi.external_ids != '{}'
		ORDER BY mi.metadata_refreshed_at ASC NULLS FIRST
	`)
	if err != nil {
		finishKickoff(ctx, q, "kickoff_refresh_stale", startedAt, 0, 0, err)
		return err
	}
	defer rows.Close()

	now := time.Now()
	type stale struct {
		ID        int64
		MediaType sqlc.MediaType
		Title     string
	}
	var items []stale
	for rows.Next() {
		var id int64
		var mt, title string
		var settingsJSON []byte
		var refreshedAt *time.Time
		if err := rows.Scan(&id, &mt, &title, &settingsJSON, &refreshedAt); err != nil {
			continue
		}
		settings := metadata.ParseSettings(settingsJSON)
		if settings.MetadataRefreshDays <= 0 {
			continue
		}
		if refreshedAt == nil {
			items = append(items, stale{ID: id, MediaType: sqlc.MediaType(mt), Title: title})
			continue
		}
		cutoff := now.AddDate(0, 0, -settings.MetadataRefreshDays)
		if refreshedAt.Before(cutoff) {
			items = append(items, stale{ID: id, MediaType: sqlc.MediaType(mt), Title: title})
		}
	}

	enqueued := 0
	failed := 0
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, "kickoff_refresh_stale", startedAt, enqueued, failed, err)
			return err
		}
		w.Progress.Set("refresh_stale_items", "kickoff_refresh_stale", it.Title)
		if err := EnqueueEnrich(ctx, rc, it.ID, it.MediaType, EnrichSourceScheduled); err != nil {
			log.Warn().Err(err).Int64("item_id", it.ID).Msg("kickoff_refresh_stale: enqueue failed")
			failed++
			continue
		}
		enqueued++
	}

	if enqueued > 0 {
		log.Info().Int("enqueued", enqueued).Msg("kickoff_refresh_stale: enqueued enrich jobs")
	}
	finishKickoff(ctx, q, "kickoff_refresh_stale", startedAt, enqueued, failed, nil)
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
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)

	tracks, err := q.ListTrackFilesPendingLoudness(ctx, kickoffLoudnessTrackBatch)
	if err != nil {
		finishKickoff(ctx, q, "kickoff_music_loudness", startedAt, 0, 0, err)
		return err
	}
	albums, err := q.ListAlbumsPendingLoudness(ctx, kickoffLoudnessAlbumBatch)
	if err != nil {
		finishKickoff(ctx, q, "kickoff_music_loudness", startedAt, 0, 0, err)
		return err
	}

	enqueued, failed := 0, 0
	for _, row := range tracks {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, "kickoff_music_loudness", startedAt, enqueued, failed, err)
			return err
		}
		w.Progress.Set("scan_music_loudness", "kickoff_music_loudness", row.Path)
		if _, err := rc.Insert(ctx, ScanTrackLoudnessArgs{TrackFileID: row.ID}, nil); err != nil {
			log.Warn().Err(err).Int64("track_file_id", row.ID).Msg("kickoff_music_loudness: enqueue track failed")
			failed++
			continue
		}
		enqueued++
	}
	for _, row := range albums {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, "kickoff_music_loudness", startedAt, enqueued, failed, err)
			return err
		}
		w.Progress.Set("scan_music_loudness", "kickoff_music_loudness", row.Title)
		if _, err := rc.Insert(ctx, ScanAlbumLoudnessArgs{AlbumID: row.ID}, nil); err != nil {
			log.Warn().Err(err).Int64("album_id", row.ID).Msg("kickoff_music_loudness: enqueue album failed")
			failed++
			continue
		}
		enqueued++
	}

	if enqueued > 0 {
		log.Info().Int("enqueued", enqueued).Msg("kickoff_music_loudness: jobs enqueued")
	}
	finishKickoff(ctx, q, "kickoff_music_loudness", startedAt, enqueued, failed, nil)
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
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)

	rows, err := w.DB.Query(ctx, `
		SELECT lf.id, lf.path
		FROM library_files lf
		JOIN libraries l ON l.id = lf.library_id
		WHERE lf.deleted_at IS NULL
		  AND lf.status = 'matched'
		  AND lf.has_trickplay = false
		  AND lf.media_info IS NOT NULL
		  AND lf.media_info->'streams' @> '[{"codec_type":"video"}]'
		  AND l.settings->>'enable_trickplay' = 'true'
	`)
	if err != nil {
		finishKickoff(ctx, q, "kickoff_trickplay", startedAt, 0, 0, err)
		return err
	}
	defer rows.Close()

	enqueued, failed := 0, 0
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, "kickoff_trickplay", startedAt, enqueued, failed, err)
			return err
		}
		w.Progress.Set("generate_trickplay", "kickoff_trickplay", filepathBase(path))
		if _, err := rc.Insert(ctx, TrickplayFileArgs{LibraryFileID: id}, nil); err != nil {
			log.Warn().Err(err).Int64("library_file_id", id).Msg("kickoff_trickplay: enqueue failed")
			failed++
			continue
		}
		enqueued++
	}

	finishKickoff(ctx, q, "kickoff_trickplay", startedAt, enqueued, failed, nil)
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
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)

	rows, err := w.DB.Query(ctx, `
		SELECT me.id, me.title, me.file_path
		FROM media_extras me
		JOIN media_items mi ON mi.id = me.media_item_id
		JOIN libraries l ON l.id = mi.library_id
		WHERE me.thumbnail_path = ''
		  AND me.file_path != ''
		  AND l.settings->>'generate_thumbnails' = 'true'
	`)
	if err != nil {
		finishKickoff(ctx, q, "kickoff_thumbnails", startedAt, 0, 0, err)
		return err
	}
	defer rows.Close()

	enqueued, failed := 0, 0
	for rows.Next() {
		var id int64
		var title, fpath string
		if err := rows.Scan(&id, &title, &fpath); err != nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, "kickoff_thumbnails", startedAt, enqueued, failed, err)
			return err
		}
		label := title
		if label == "" {
			label = filepathBase(fpath)
		}
		w.Progress.Set("generate_thumbnails", "kickoff_thumbnails", label)
		if _, err := rc.Insert(ctx, ThumbnailExtraArgs{ExtraID: id}, nil); err != nil {
			log.Warn().Err(err).Int64("extra_id", id).Msg("kickoff_thumbnails: enqueue failed")
			failed++
			continue
		}
		enqueued++
	}

	finishKickoff(ctx, q, "kickoff_thumbnails", startedAt, enqueued, failed, nil)
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
	q := sqlc.New(w.DB)

	if w.Enabled != nil && !w.Enabled(ctx) {
		log.Info().Msg("kickoff_sonic_analysis: skipped — disabled in settings")
		finishKickoff(ctx, q, "kickoff_sonic_analysis", startedAt, 0, 0, nil)
		return nil
	}

	rc := river.ClientFromContext[pgx.Tx](ctx)
	ids, err := q.ListPendingAnalysisTracks(ctx, sqlc.ListPendingAnalysisTracksParams{
		MaxDurationSeconds: sonicanalysis.MaxAnalysisDurationSeconds,
		AnalyzerVersion:    sonicanalysis.AnalyzerVersion,
		LimitCount:         sonicKickoffBatch,
	})
	if err != nil {
		finishKickoff(ctx, q, "kickoff_sonic_analysis", startedAt, 0, 0, err)
		return err
	}

	enqueued, failed := 0, 0
	w.Progress.Set("analyze_music_facets", "kickoff_sonic_analysis", "queueing tracks…")
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			finishKickoff(ctx, q, "kickoff_sonic_analysis", startedAt, enqueued, failed, err)
			return err
		}
		if _, err := rc.Insert(ctx, AnalyzeTrackFacetsArgs{TrackID: id}, nil); err != nil {
			log.Warn().Err(err).Int64("track_id", id).Msg("kickoff_sonic_analysis: enqueue failed")
			failed++
			continue
		}
		enqueued++
	}

	finishKickoff(ctx, q, "kickoff_sonic_analysis", startedAt, enqueued, failed, nil)
	return nil
}
