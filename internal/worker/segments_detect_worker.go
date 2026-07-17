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
//     (series, season) pair — needs at least one PENDING episode (a gap to
//     fill) and one other eligible episode to compare against; the partner
//     does NOT need to be pending itself (a community-covered episode's
//     audio pairs just as well, so a lone gap in an otherwise-covered
//     season still resolves). It's a gap-filler, not a re-measurement
//     pass: only pending files get segments written and
//     segments_detected_at stamped, and a type the community already
//     covered for a file is left alone (see
//     insertChromaprintSegmentIfAbsent) — community and chromaprint are
//     peers by arrival order, not a strict ranking.
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

// DetectSeasonSegmentsWorker fingerprints the pending episodes of one
// (series, season) pair (plus whichever covered partners the matching
// actually needs), pairs episodes by nearest episode number to find shared
// intro/tail regions, and writes any resolved segments — for pending
// files only.
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
	pending    bool
	hasIntro   bool
	hasCredits bool
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
		// No eligible files at all (deleted/unparsed between the pump's
		// listing and this job actually running).
		return nil
	}

	eps := make([]seasonEpisode, 0, len(rows))
	epNumbers := make([]int, 0, len(rows))
	for _, r := range rows {
		var info MediaInfo
		if len(r.MediaInfo) > 0 {
			_ = json.Unmarshal(r.MediaInfo, &info)
		}
		eps = append(eps, seasonEpisode{
			fileID:     r.ID,
			path:       r.Path,
			epNum:      int(r.EpisodeNumber),
			duration:   info.Duration,
			pending:    r.Pending.Bool,
			hasIntro:   r.HasIntro,
			hasCredits: r.HasCredits,
		})
		epNumbers = append(epNumbers, int(r.EpisodeNumber))
	}

	// Gap-filler targets: only PENDING files missing a type get that type
	// detected and written. Everything else in eps is comparison material
	// — fingerprinted lazily by resolveRegionsForTargets only when a
	// nearby target actually needs it, never written, never stamped. A
	// window no pending file misses is never decoded for anyone (e.g. the
	// community pass covered every intro but missed some credits: the
	// whole intro pass is skipped).
	var introTargets, tailTargets []int
	for i := range eps {
		if !eps[i].pending {
			continue
		}
		if !eps[i].hasIntro {
			introTargets = append(introTargets, i)
		}
		if !eps[i].hasCredits {
			tailTargets = append(tailTargets, i)
		}
	}
	if len(introTargets) == 0 && len(tailTargets) == 0 {
		// Every gap got filled between the pump's listing and this job
		// running (community pump race, or another run). Nothing to fill
		// means nothing to stamp either — partners are never stamped.
		return nil
	}

	decodeIntro := func(i int) []uint32 {
		if ctx.Err() != nil {
			return nil
		}
		start, dur, ok := introWindowSecs(eps[i].duration)
		if !ok {
			return nil
		}
		w.Progress.SetCurrent(DetectSeasonSegmentsArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(eps[i].path))
		pts, err := chromaprintWindowRaw(ctx, eps[i].path, start, dur)
		if err != nil {
			log.Warn().Err(err).Str("path", eps[i].path).Msg("detect_segments_season: intro fingerprint failed")
			return nil
		}
		return pts
	}
	decodeTail := func(i int) []uint32 {
		if ctx.Err() != nil {
			return nil
		}
		start, dur, ok := tailWindowSecs(eps[i].duration)
		if !ok {
			return nil
		}
		w.Progress.SetCurrent(DetectSeasonSegmentsArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(eps[i].path))
		pts, err := chromaprintWindowRaw(ctx, eps[i].path, start, dur)
		if err != nil {
			log.Warn().Err(err).Str("path", eps[i].path).Msg("detect_segments_season: tail fingerprint failed")
			return nil
		}
		eps[i].tailOffset = start
		return pts
	}

	introRegions := resolveRegionsForTargets(len(eps), introTargets, epNumbers, decodeIntro, acceptIntroRegion)
	tailRegions := resolveRegionsForTargets(len(eps), tailTargets, epNumbers, decodeTail, acceptCreditsRegion)
	if ctx.Err() != nil {
		// Cancelled mid-resolution: regions are partial and nothing has
		// been written or stamped yet — leave every file unstamped so the
		// next sweep retries the whole season.
		return ctx.Err()
	}

	processedIDs := make([]int64, 0, len(eps))
	for i := range eps {
		if !eps[i].pending {
			// Partners only lent their audio — never written, never
			// stamped, so the pump's pending conditions stay untouched
			// for them.
			continue
		}
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

		if err := w.writeEpisodeSegments(ctx, q, eps[i], introRegions[i], tailRegions[i]); err != nil {
			log.Warn().Err(err).Int64("library_file_id", eps[i].fileID).Msg("detect_segments_season: write failed")
			continue
		}
		processedIDs = append(processedIDs, eps[i].fileID)
	}

	if len(processedIDs) > 0 {
		if err := q.MarkFileSegmentsDetected(ctx, processedIDs); err != nil {
			return fmt.Errorf("mark segments detected: %w", err)
		}
	}
	return nil
}

// writeEpisodeSegments writes the resolved intro/credits rows (if any) for
// one episode inside a single transaction. chromaprint is a gap-filler
// (see insertChromaprintSegmentIfAbsent): a manual row, a community row, or
// an already-existing chromaprint row for the type all block the write —
// this only ever lands in a hole the community pass left open.
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
		if err := insertChromaprintSegmentIfAbsent(ctx, qtx, e.fileID, "intro", secsToMs(intro.StartSecs), secsToMs(intro.EndSecs)); err != nil {
			return err
		}
	}
	if tail != nil {
		// tail is relative to the tail window; credits always run from the
		// resolved start to the end of the file, matching the community
		// convention of materializing open-ended markers.
		startMs := secsToMs(e.tailOffset + tail.StartSecs)
		endMs := secsToMs(e.duration)
		if err := insertChromaprintSegmentIfAbsent(ctx, qtx, e.fileID, "credits", startMs, endMs); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// insertChromaprintSegmentIfAbsent writes a chromaprint-measured segment
// only when nothing already covers this (file, type): not a manual row,
// not a community row, and not an existing chromaprint row of its own.
// Community and chromaprint are peers by arrival order (see the
// top-of-file precedence note in queries/media_segments.sql) — whichever
// one lands first for a type wins, and this never second-guesses a
// community row that got there first. It still replaces a blackframe row:
// a black-frame heuristic guess is strictly weaker than any direct
// measurement or crowdsourced marker.
//
// The EXISTS checks are read-committed short-circuits only — a concurrent
// community write (different queue, different job) can slip in between
// check and insert. The rank-aware upsert (UpsertMediaSegmentByRank) is
// the source of truth: it overwrites a strictly weaker blackframe row in
// place (no separate delete needed) and no-ops against an equal-rank
// community/chromaprint row or a stronger manual one, so losing the
// commit race silently keeps the first-arriver — the intended policy,
// not an error.
func insertChromaprintSegmentIfAbsent(ctx context.Context, qtx *sqlc.Queries, fileID int64, segType string, startMs, endMs int64) error {
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
	community, err := qtx.ExistsCommunityMediaSegmentForFileAndType(ctx, sqlc.ExistsCommunityMediaSegmentForFileAndTypeParams{
		LibraryFileID: fileID,
		SegmentType:   segType,
	})
	if err != nil {
		return err
	}
	if community {
		return nil
	}
	chromaprint, err := qtx.ExistsChromaprintMediaSegment(ctx, sqlc.ExistsChromaprintMediaSegmentParams{
		LibraryFileID: fileID,
		SegmentType:   segType,
	})
	if err != nil {
		return err
	}
	if chromaprint {
		return nil
	}
	return qtx.UpsertMediaSegmentByRank(ctx, sqlc.UpsertMediaSegmentByRankParams{
		LibraryFileID: fileID,
		SegmentType:   segType,
		StartMs:       startMs,
		EndMs:         endMs,
		Source:        "chromaprint",
	})
}

// insertBlackframeSegmentIfAbsent inserts a blackframe-detected movie
// credits segment only when no row of that type exists yet, from any
// source. A black-frame heuristic guess only ever fills a gap — it never
// replaces a manual, community, or chromaprint row (all of which are
// direct or curated data, unlike a blackdetect cut). The write goes
// through the rank-aware upsert with blackframe's bottom rank, so a
// community or chromaprint row racing in between the EXISTS check and
// the insert wins silently (see insertChromaprintSegmentIfAbsent for the
// full race note).
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
	return qtx.UpsertMediaSegmentByRank(ctx, sqlc.UpsertMediaSegmentByRankParams{
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
		if len(rows) > 0 {
			if ctx.Err() != nil {
				return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
			}
			last := rows[len(rows)-1]
			w.Progress.Set("detect_media_segments", "kickoff_detect_segments", fmt.Sprintf("season %d", last.Season))
			// Rows with no resolved media item don't get a job (their cursor
			// still advances below); only valid rows go into the batch.
			jobs := make([]river.InsertManyParams, 0, len(rows))
			for _, row := range rows {
				if !row.MediaItemID.Valid {
					continue
				}
				jobs = append(jobs, river.InsertManyParams{
					Args: DetectSeasonSegmentsArgs{
						MediaItemID:     row.MediaItemID.Int64,
						Season:          int(row.Season),
						ScheduledTaskID: taskID,
					},
					InsertOpts: scheduledJobInsertOpts(st.Source),
				})
			}
			if len(jobs) > 0 {
				results, err := rc.InsertMany(ctx, jobs)
				if err != nil {
					log.Warn().Err(err).Int("season_count", len(jobs)).Msg("kickoff_detect_segments: batch enqueue season failed")
					st.Failed += len(jobs)
					st.Skipped += len(jobs)
				} else {
					for _, res := range results {
						if res.UniqueSkippedAsDuplicate {
							st.Skipped++
						} else {
							st.Enqueued++
						}
					}
				}
			}
			st.TrackCursor = last.CursorKey
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
			if len(rows) > 0 {
				if ctx.Err() != nil {
					return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
				}
				last := rows[len(rows)-1]
				w.Progress.Set("detect_media_segments", "kickoff_detect_segments", filepath.Base(last.Path))
				jobs := make([]river.InsertManyParams, len(rows))
				for i, row := range rows {
					jobs[i] = river.InsertManyParams{
						Args:       DetectMovieCreditsArgs{LibraryFileID: row.ID, ScheduledTaskID: taskID},
						InsertOpts: scheduledJobInsertOpts(st.Source),
					}
				}
				results, err := rc.InsertMany(ctx, jobs)
				if err != nil {
					log.Warn().Err(err).Int("movie_count", len(rows)).Msg("kickoff_detect_segments: batch enqueue movie failed")
					st.Failed += len(rows)
					st.Skipped += len(rows)
				} else {
					for _, res := range results {
						if res.UniqueSkippedAsDuplicate {
							st.Skipped++
						} else {
							st.Enqueued++
						}
					}
				}
				st.AlbumCursor = last.ID
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
