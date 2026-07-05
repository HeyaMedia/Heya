package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// Local skip-segment detection — the Intro-Skipper-style pass that runs
// over every file scan_media_segments has already checked against the
// community databases (segments_analyzed_at set), regardless of whether
// that check found anything (segments_detected_at NULL is the only other
// gate). Two work kinds:
//
//   - detect_segments_season: chromaprint cross-episode matching for a TV
//     (series, season) pair — needs at least two pending episodes to
//     compare. A chromaprint measurement is direct signal from the exact
//     file, so it outranks community data and replaces it when found
//     (see replaceWithChromaprintSegment) — community is a fast
//     placeholder, not the last word.
//   - detect_segments_movie: ffmpeg blackdetect over a movie's tail. A
//     heuristic guess, so it only ever fills a gap (see
//     insertBlackframeSegmentIfAbsent) — it never replaces anything.
//
// Both are heavier than the community fetch (real audio decode, not an
// HTTP round-trip), so River's default per-job context deadline is too
// tight for a 25-episode season or a slow SMB tail read — both workers
// override Timeout() to run unbounded.

// ---------------------------------------------------------------------------
// detect_segments_season
// ---------------------------------------------------------------------------

// DetectSeasonSegmentsWorker fingerprints every pending episode in one
// (series, season) pair, pairs episodes by nearest episode number to find
// shared intro/tail regions, and writes any resolved segments.
type DetectSeasonSegmentsWorker struct {
	river.WorkerDefaults[DetectSeasonSegmentsArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

// Timeout overrides River's default per-job deadline (JobTimeoutDefault is
// 1 minute; the client-level default here is 6h — see queueops.JobTimeout
// — but a season with many long episodes can still exceed that on a slow
// mount). -1 means unbounded: only a process shutdown or explicit cancel
// stops the job.
func (w *DetectSeasonSegmentsWorker) Timeout(*river.Job[DetectSeasonSegmentsArgs]) time.Duration {
	return -1
}

type seasonEpisode struct {
	fileID     int64
	path       string
	epNum      int
	duration   float64
	introPts   []uint32
	tailPts    []uint32
	tailOffset float64
}

func (w *DetectSeasonSegmentsWorker) Work(ctx context.Context, job *river.Job[DetectSeasonSegmentsArgs]) error {
	q := sqlc.New(w.DB)

	if !chromaprintMuxerAvailable() {
		// Logged once by chromaprintMuxerAvailable. Deliberately don't stamp
		// segments_detected_at: the next kickoff run re-lists this season,
		// so a future ffmpeg upgrade picks it back up automatically.
		return nil
	}

	rows, err := q.ListEpisodeFilesForSeasonDetection(ctx, sqlc.ListEpisodeFilesForSeasonDetectionParams{
		MediaItemID: job.Args.MediaItemID,
		Season:      int32(job.Args.Season),
	})
	if err != nil {
		return fmt.Errorf("list episodes for media_item %d season %d: %w", job.Args.MediaItemID, job.Args.Season, err)
	}
	if len(rows) == 0 {
		// Resolved by another run (or a race with the community pump)
		// between the pump's listing and this job actually running.
		return nil
	}

	eps := make([]seasonEpisode, 0, len(rows))
	for _, r := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		var info MediaInfo
		if len(r.MediaInfo) > 0 {
			_ = json.Unmarshal(r.MediaInfo, &info)
		}
		ep := seasonEpisode{fileID: r.ID, path: r.Path, epNum: int(r.EpisodeNumber), duration: info.Duration}

		w.Progress.SetCurrent(DetectSeasonSegmentsArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(r.Path))

		if start, dur, ok := introWindowSecs(info.Duration); ok {
			pts, err := chromaprintWindowRaw(ctx, r.Path, start, dur)
			if err != nil {
				log.Warn().Err(err).Str("path", r.Path).Msg("detect_segments_season: intro fingerprint failed")
			} else {
				ep.introPts = pts
			}
		}
		if start, dur, ok := tailWindowSecs(info.Duration); ok {
			pts, err := chromaprintWindowRaw(ctx, r.Path, start, dur)
			if err != nil {
				log.Warn().Err(err).Str("path", r.Path).Msg("detect_segments_season: tail fingerprint failed")
			} else {
				ep.tailPts = pts
				ep.tailOffset = start
			}
		}
		eps = append(eps, ep)
	}

	introPoints := make([][]uint32, len(eps))
	tailPoints := make([][]uint32, len(eps))
	epNumbers := make([]int, len(eps))
	for i, e := range eps {
		introPoints[i] = e.introPts
		tailPoints[i] = e.tailPts
		epNumbers[i] = e.epNum
	}
	introRegions := pairRegions(introPoints, epNumbers, acceptIntroRegion)
	tailRegions := pairRegions(tailPoints, epNumbers, acceptCreditsRegion)

	processedIDs := make([]int64, 0, len(eps))
	for i, e := range eps {
		if ctx.Err() != nil {
			// Leave unprocessed files unstamped so the next sweep retries
			// them; only mark what we actually attempted this wake.
			if len(processedIDs) > 0 {
				markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = q.MarkFileSegmentsDetected(markCtx, processedIDs)
				cancel()
			}
			return ctx.Err()
		}

		if err := w.writeEpisodeSegments(ctx, q, e, introRegions[i], tailRegions[i]); err != nil {
			log.Warn().Err(err).Int64("library_file_id", e.fileID).Msg("detect_segments_season: write failed")
			continue
		}
		processedIDs = append(processedIDs, e.fileID)
	}

	if len(processedIDs) > 0 {
		if err := q.MarkFileSegmentsDetected(ctx, processedIDs); err != nil {
			return fmt.Errorf("mark segments detected: %w", err)
		}
	}
	return nil
}

// writeEpisodeSegments writes the resolved intro/credits rows (if any) for
// one episode inside a single transaction. A chromaprint measurement
// outranks community data (see replaceWithChromaprintSegment), so this
// replaces rather than merely fills a gap — a manual row is the only thing
// that blocks it.
func (w *DetectSeasonSegmentsWorker) writeEpisodeSegments(ctx context.Context, q *sqlc.Queries, e seasonEpisode, intro, tail *resolvedRegion) error {
	if intro == nil && tail == nil {
		return nil
	}

	tx, err := w.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := q.WithTx(tx)

	if intro != nil {
		if err := replaceWithChromaprintSegment(ctx, qtx, e.fileID, "intro", secsToMs(intro.StartSecs), secsToMs(intro.EndSecs)); err != nil {
			return err
		}
	}
	if tail != nil {
		// tail is relative to the tail window; credits always run from the
		// resolved start to the end of the file, matching the community
		// convention of materializing open-ended markers.
		startMs := secsToMs(e.tailOffset + tail.StartSecs)
		endMs := secsToMs(e.duration)
		if err := replaceWithChromaprintSegment(ctx, qtx, e.fileID, "credits", startMs, endMs); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// replaceWithChromaprintSegment writes a chromaprint-measured segment,
// replacing any community or blackframe row of the same type — the audio
// was measured directly on this exact release, so it outranks crowdsourced
// community data (which is only duration-gated, not release-verified) and
// a blackframe heuristic guess. A manual row still wins outright: the type
// is skipped entirely rather than overwritten. Also clears any stale
// chromaprint row of its own from a prior partial run, since this worker's
// jobs are MaxAttempts 2.
func replaceWithChromaprintSegment(ctx context.Context, qtx *sqlc.Queries, fileID int64, segType string, startMs, endMs int64) error {
	manual, err := qtx.ExistsManualMediaSegment(ctx, sqlc.ExistsManualMediaSegmentParams{
		LibraryFileID: fileID,
		SegmentType:   segType,
	})
	if err != nil {
		return err
	}
	if manual {
		return nil
	}
	if err := qtx.DeleteReplaceableMediaSegmentsForFileAndType(ctx, sqlc.DeleteReplaceableMediaSegmentsForFileAndTypeParams{
		LibraryFileID: fileID,
		SegmentType:   segType,
	}); err != nil {
		return err
	}
	return qtx.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileID,
		SegmentType:   segType,
		StartMs:       startMs,
		EndMs:         endMs,
		Source:        "chromaprint",
	})
}

// insertBlackframeSegmentIfAbsent inserts a blackframe-detected movie
// credits segment only when no row of that type exists yet, from any
// source. Unlike chromaprint (a direct measurement, so it outranks even
// community data), a black-frame heuristic guess only ever fills a gap.
func insertBlackframeSegmentIfAbsent(ctx context.Context, qtx *sqlc.Queries, fileID int64, segType string, startMs, endMs int64) error {
	exists, err := qtx.ExistsMediaSegmentForFileAndType(ctx, sqlc.ExistsMediaSegmentForFileAndTypeParams{
		LibraryFileID: fileID,
		SegmentType:   segType,
	})
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return qtx.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
		LibraryFileID: fileID,
		SegmentType:   segType,
		StartMs:       startMs,
		EndMs:         endMs,
		Source:        "blackframe",
	})
}

func secsToMs(s float64) int64 {
	if s < 0 {
		return 0
	}
	return int64(s * 1000)
}

// ---------------------------------------------------------------------------
// detect_segments_movie
// ---------------------------------------------------------------------------

// DetectMovieCreditsWorker runs ffmpeg blackdetect over one movie's tail
// window and writes a credits row when a qualifying black-frame cut is
// found.
type DetectMovieCreditsWorker struct {
	river.WorkerDefaults[DetectMovieCreditsArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

// Timeout overrides River's default per-job deadline — see
// DetectSeasonSegmentsWorker.Timeout for why.
func (w *DetectMovieCreditsWorker) Timeout(*river.Job[DetectMovieCreditsArgs]) time.Duration {
	return -1
}

func (w *DetectMovieCreditsWorker) Work(ctx context.Context, job *river.Job[DetectMovieCreditsArgs]) error {
	q := sqlc.New(w.DB)

	lf, err := q.GetLibraryFileByID(ctx, job.Args.LibraryFileID)
	if err != nil {
		return fmt.Errorf("get library_file %d: %w", job.Args.LibraryFileID, err)
	}
	if lf.DeletedAt.Valid {
		return nil
	}

	var info MediaInfo
	if len(lf.MediaInfo) > 0 {
		_ = json.Unmarshal(lf.MediaInfo, &info)
	}

	w.Progress.SetCurrent(DetectMovieCreditsArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(lf.Path))

	if info.Duration <= 0 {
		return q.MarkFileSegmentsDetected(ctx, []int64{lf.ID})
	}

	startSecs, ok, err := detectMovieCredits(ctx, lf.Path, info.Duration)
	if err != nil {
		log.Warn().Err(err).Str("path", lf.Path).Msg("detect_segments_movie: blackdetect failed")
		// Stamp anyway — a permanently-undecodable file (bad remux, exotic
		// codec) shouldn't be retried by every future pump sweep. A
		// transient SMB blip is the tradeoff; the community pass and any
		// future re-scan can still fill it later.
		return q.MarkFileSegmentsDetected(ctx, []int64{lf.ID})
	}

	if ok {
		tx, err := w.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		qtx := q.WithTx(tx)
		if err := insertBlackframeSegmentIfAbsent(ctx, qtx, lf.ID, "credits", secsToMs(startSecs), secsToMs(info.Duration)); err != nil {
			return err
		}
		if err := qtx.MarkFileSegmentsDetected(ctx, []int64{lf.ID}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	return q.MarkFileSegmentsDetected(ctx, []int64{lf.ID})
}

// ---------------------------------------------------------------------------
// kickoff_detect_segments
// ---------------------------------------------------------------------------

// Per-wave caps. Season jobs are heavy (multi-episode audio decode), so
// the wave stays small; movie jobs are one file's tail window each and can
// run a larger wave.
const (
	kickoffDetectSeasonBatch = 50
	kickoffDetectMovieBatch  = 200
)

// KickoffDetectSegmentsWorker is the two-cursor pump clone of
// KickoffMusicLoudnessWorker: seasons sweep first via TrackCursor (packed
// media_item_id*100000+season key), then movie files via AlbumCursor, one
// wave of each kind in flight at a time.
type KickoffDetectSegmentsWorker struct {
	river.WorkerDefaults[KickoffDetectSegmentsArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *KickoffDetectSegmentsWorker) Work(ctx context.Context, job *river.Job[KickoffDetectSegmentsArgs]) error {
	taskID := job.Args.ScheduledTaskID
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)
	st := readPumpState(job.Metadata)
	seasonKind := DetectSeasonSegmentsArgs{}.Kind()
	movieKind := DetectMovieCreditsArgs{}.Kind()

	if ctx.Err() != nil {
		return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
	}

	if stop, reason := pumpShouldStop(ctx, q, taskID, st.Source, job.CreatedAt); stop {
		switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
		case err != nil:
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		case !proceed:
			log.Info().Str("task", taskID).Msg("kickoff_detect_segments: wind-down aborted — run upgraded to manual mid-wake")
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		cancelled, _ := queueops.CancelPendingByScheduledTask(ctx, w.DB, taskID, []string{seasonKind, movieKind})
		log.Info().Str("task", taskID).Str("reason", reason).Int64("cancelled_pending", cancelled).Msg("kickoff_detect_segments: winding down")
		finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
		return nil
	}

	// Season phase: keep one wave of per-season jobs topped up, sweeping
	// the pending set in cursor-key order exactly once.
	seasonActive, err := pumpActiveCount(ctx, w.DB, taskID, seasonKind)
	if err != nil {
		return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
	}
	seasonsListed := -1 // -1: wave full, sweep not consulted this wake
	if want := kickoffDetectSeasonBatch - seasonActive; want > 0 {
		rows, err := q.ListSeasonsPendingDetection(ctx, sqlc.ListSeasonsPendingDetectionParams{
			AfterKey: st.TrackCursor,
			RowLimit: int32(want),
		})
		if err != nil {
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		}
		seasonsListed = len(rows)
		for _, row := range rows {
			if ctx.Err() != nil {
				return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
			}
			if !row.MediaItemID.Valid {
				st.TrackCursor = row.CursorKey
				continue
			}
			w.Progress.Set("detect_media_segments", "kickoff_detect_segments", fmt.Sprintf("season %d", row.Season))
			res, err := rc.Insert(ctx, DetectSeasonSegmentsArgs{
				MediaItemID:     row.MediaItemID.Int64,
				Season:          int(row.Season),
				ScheduledTaskID: taskID,
			}, nil)
			switch {
			case err != nil:
				log.Warn().Err(err).Int64("media_item_id", row.MediaItemID.Int64).Int32("season", row.Season).Msg("kickoff_detect_segments: enqueue season failed")
				st.Failed++
				st.Skipped++
			case res.UniqueSkippedAsDuplicate:
				st.Skipped++
			default:
				st.Enqueued++
			}
			st.TrackCursor = row.CursorKey
		}
	}
	seasonsDone := seasonActive == 0 && seasonsListed == 0

	// Movie phase: only starts once the season sweep has drained, mirroring
	// the loudness pump's track-then-album ordering.
	if seasonsDone {
		movieActive, err := pumpActiveCount(ctx, w.DB, taskID, movieKind)
		if err != nil {
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		}
		moviesListed := -1
		if want := kickoffDetectMovieBatch - movieActive; want > 0 {
			rows, err := q.ListMovieFilesPendingDetection(ctx, sqlc.ListMovieFilesPendingDetectionParams{
				AfterID:  st.AlbumCursor,
				RowLimit: int32(want),
			})
			if err != nil {
				return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
			}
			moviesListed = len(rows)
			for _, row := range rows {
				if ctx.Err() != nil {
					return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
				}
				w.Progress.Set("detect_media_segments", "kickoff_detect_segments", filepath.Base(row.Path))
				res, err := rc.Insert(ctx, DetectMovieCreditsArgs{LibraryFileID: row.ID, ScheduledTaskID: taskID}, nil)
				switch {
				case err != nil:
					log.Warn().Err(err).Int64("library_file_id", row.ID).Msg("kickoff_detect_segments: enqueue movie failed")
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
		if movieActive == 0 && moviesListed == 0 {
			if st.restartSweep() {
				log.Info().Str("task", taskID).Msg("kickoff_detect_segments: re-sweeping for items skipped during the run")
				st.ErrStreak = 0
				return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
			}
			switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
			case err != nil:
				return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
			case !proceed:
				log.Info().Str("task", taskID).Msg("kickoff_detect_segments: finish aborted — run upgraded to manual mid-wake")
				st.ErrStreak = 0
				return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
			}
			log.Info().Str("task", taskID).Int("enqueued", st.Enqueued).Int("failed", st.Failed).Msg("kickoff_detect_segments: backlog drained")
			finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
			return nil
		}
	}

	st.ErrStreak = 0
	return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
}
