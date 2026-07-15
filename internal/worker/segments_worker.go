package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/communitysegments"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// Community skip segments — intro/recap/credits/preview/commercial
// markers fetched directly by Heya from TheIntroDB, SkipMeDB, and AniSkip.
// The communitysegments service normalizes every candidate with per-source
// provenance and the runtime each marker was authored against; this worker
// applies the duration gate against the file's
// actual probed runtime and stores only the winners, so the player and
// the Jellyfin compat layer never see conflicting release cuts.

const (
	// segmentDurationTolerance is how far a candidate's authored runtime
	// may drift from the file's probed runtime and still be trusted.
	// Beyond this it's a different release cut and its timestamps would
	// land mid-scene.
	segmentDurationTolerance = 10_000 // ms

	// segmentUnknownDurationDistance ranks candidates that don't carry an
	// authored runtime (TheIntroDB omits it — its release matching runs
	// server-side from the duration we pass in the request). They beat a
	// far-off explicit runtime but lose to a near-exact one.
	segmentUnknownDurationDistance = 5_000 // ms

	// segmentMinDurationMs drops degenerate markers nobody should skip.
	segmentMinDurationMs = 1_000
)

// segmentSourceRank breaks ties between equally-plausible candidates.
var segmentSourceRank = map[string]int{
	"theintrodb": 0,
	"skipmedb":   1,
	"aniskip":    2,
}

type pickedSegment struct {
	Type    string
	StartMs int64
	EndMs   int64
	Source  string
}

// pickSegments chooses at most one winner per segment type (commercial
// excepted — multiple breaks are legitimate, so every valid commercial
// from the winning source is kept). Open-ended markers ("to end of
// media") are materialized against the file runtime so nothing
// downstream handles nulls.
func pickSegments(cands []communitysegments.Candidate, fileDurationMs int64) []pickedSegment {
	type scored struct {
		c    communitysegments.Candidate
		dist int64
	}
	best := map[string]scored{}
	for _, c := range cands {
		dist, ok := scoreCandidate(c, fileDurationMs)
		if !ok {
			continue
		}
		cur, seen := best[c.Type]
		if !seen || segmentBeats(c, dist, cur.c, cur.dist) {
			best[c.Type] = scored{c: c, dist: dist}
		}
	}

	var out []pickedSegment
	for segType, winner := range best {
		if segType == "commercial" {
			// All valid breaks from the winning source, not just the one
			// that scored best.
			for _, c := range cands {
				if c.Type != "commercial" || c.Source != winner.c.Source {
					continue
				}
				if _, ok := scoreCandidate(c, fileDurationMs); !ok {
					continue
				}
				out = append(out, materializeSegment(c, fileDurationMs))
			}
			continue
		}
		out = append(out, materializeSegment(winner.c, fileDurationMs))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartMs != out[j].StartMs {
			return out[i].StartMs < out[j].StartMs
		}
		return out[i].Type < out[j].Type
	})
	return out
}

// scoreCandidate validates a candidate against the file and returns its
// duration-gate distance (lower = more trusted). ok=false rejects it.
func scoreCandidate(c communitysegments.Candidate, fileDurationMs int64) (int64, bool) {
	if c.StartMs < 0 {
		return 0, false
	}
	end := fileDurationMs
	if c.EndMs != nil {
		end = *c.EndMs
	} else if fileDurationMs <= 0 {
		return 0, false // open-ended marker with no runtime to close it against
	}
	if end-c.StartMs < segmentMinDurationMs {
		return 0, false
	}
	if fileDurationMs > 0 && c.StartMs >= fileDurationMs {
		return 0, false
	}
	if c.DurationMs == 0 {
		return segmentUnknownDurationDistance, true
	}
	if fileDurationMs <= 0 {
		return c.DurationMs, true
	}
	dist := c.DurationMs - fileDurationMs
	if dist < 0 {
		dist = -dist
	}
	if dist > segmentDurationTolerance {
		return 0, false // different release cut — timestamps would land mid-scene
	}
	return dist, true
}

// segmentBeats reports whether candidate a (distance da) outranks b:
// closer authored runtime, then more community submissions, then the
// fixed source order.
func segmentBeats(a communitysegments.Candidate, da int64, b communitysegments.Candidate, db int64) bool {
	if da != db {
		return da < db
	}
	if a.Submissions != b.Submissions {
		return a.Submissions > b.Submissions
	}
	return segmentSourceRank[a.Source] < segmentSourceRank[b.Source]
}

func materializeSegment(c communitysegments.Candidate, fileDurationMs int64) pickedSegment {
	end := fileDurationMs
	if c.EndMs != nil {
		end = *c.EndMs
	}
	if fileDurationMs > 0 && end > fileDurationMs {
		end = fileDurationMs
	}
	return pickedSegment{
		Type:    c.Type,
		StartMs: c.StartMs,
		EndMs:   end,
		Source:  "community:" + c.Source,
	}
}

// externalIDStrings decodes a media_items.external_ids JSONB blob into
// string form regardless of whether individual values were stored as
// JSON strings or numbers.
func externalIDStrings(raw []byte) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		switch t := v.(type) {
		case string:
			out[k] = t
		case float64:
			out[k] = fmt.Sprintf("%.0f", t)
		}
	}
	return out
}

// parseFirstEpisodeRef pulls the first (season, episode) pair out of a
// parse_result blob. Multi-episode files use the first episode's
// markers — the intro is at the top of the file either way.
func parseFirstEpisodeRef(raw []byte) (season, episode int, ok bool) {
	var pr struct {
		Parsed struct {
			Release struct {
				Seasons  []int `json:"seasons"`
				Episodes []int `json:"episodes"`
			} `json:"release"`
		} `json:"parsed"`
	}
	if err := json.Unmarshal(raw, &pr); err != nil {
		return 0, 0, false
	}
	if len(pr.Parsed.Release.Seasons) == 0 || len(pr.Parsed.Release.Episodes) == 0 {
		return 0, 0, false
	}
	return pr.Parsed.Release.Seasons[0], pr.Parsed.Release.Episodes[0], true
}

// ScanMediaSegmentsFileWorker fetches community segments for one file.
// Pure network work against the community providers; runs on its own bounded
// queue so a cold library sweep cannot starve unrelated work.
type ScanMediaSegmentsFileWorker struct {
	river.WorkerDefaults[ScanMediaSegmentsFileArgs]
	DB       *pgxpool.Pool
	Segments *communitysegments.Service
	Progress *TaskProgressBroadcaster
}

type segmentFileSnapshot struct {
	Path       string
	Size       int64
	MTime      time.Time
	DurationMs int64
}

func (snapshot segmentFileSnapshot) Equal(other segmentFileSnapshot) bool {
	return snapshot.Path == other.Path &&
		snapshot.Size == other.Size &&
		snapshot.MTime.Equal(other.MTime) &&
		snapshot.DurationMs == other.DurationMs
}

// currentSegmentFileSnapshot proves that the database row still describes the
// bytes currently at its path. Segment timings are release-cut-specific, so a
// missing/soft-deleted file or a size/mtime mismatch is a benign stale job,
// not permission to write results against the replacement.
func currentSegmentFileSnapshot(file sqlc.LibraryFile) (segmentFileSnapshot, string, error) {
	if file.DeletedAt.Valid {
		return segmentFileSnapshot{}, "file_soft_deleted", nil
	}
	if !file.Mtime.Valid {
		return segmentFileSnapshot{}, "file_mtime_unknown", nil
	}

	var mediaInfo MediaInfo
	if len(file.MediaInfo) == 0 || json.Unmarshal(file.MediaInfo, &mediaInfo) != nil || mediaInfo.Duration <= 0 {
		return segmentFileSnapshot{}, "file_not_probed", nil
	}

	source, err := vfs.Open(vfs.Dir(file.Path))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return segmentFileSnapshot{}, "file_missing", nil
		}
		return segmentFileSnapshot{}, "", err
	}
	defer source.Close() //nolint:errcheck // read-only stat source

	info, err := fs.Stat(source.FS, vfs.Base(file.Path))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return segmentFileSnapshot{}, "file_missing", nil
		}
		return segmentFileSnapshot{}, "", err
	}
	if info.IsDir() {
		return segmentFileSnapshot{}, "path_is_directory", nil
	}

	dbMTime := file.Mtime.Time.Truncate(time.Microsecond)
	diskMTime := info.ModTime().Truncate(time.Microsecond)
	if info.Size() != file.Size || !diskMTime.Equal(dbMTime) {
		return segmentFileSnapshot{}, "file_changed_since_scan", nil
	}

	return segmentFileSnapshot{
		Path:       file.Path,
		Size:       file.Size,
		MTime:      dbMTime,
		DurationMs: int64(mediaInfo.Duration * 1000),
	}, "", nil
}

func (w *ScanMediaSegmentsFileWorker) Work(ctx context.Context, job *river.Job[ScanMediaSegmentsFileArgs]) error {
	if err := snoozeIfScannerPipelinePending(ctx, w.DB); err != nil {
		return err
	}
	if w.Segments == nil {
		return nil
	}

	q := sqlc.New(w.DB)

	lf, err := q.GetLibraryFileByID(ctx, job.Args.LibraryFileID)
	if err != nil {
		return fmt.Errorf("get library_file %d: %w", job.Args.LibraryFileID, err)
	}
	if lf.DeletedAt.Valid {
		return nil
	}
	fileSnapshot, skipReason, err := currentSegmentFileSnapshot(lf)
	if err != nil {
		return fmt.Errorf("validate segment file %d: %w", lf.ID, err)
	}
	if skipReason != "" {
		log.Debug().
			Int64("library_file_id", lf.ID).
			Str("path", vfs.RedactPath(lf.Path)).
			Str("reason", skipReason).
			Msg("community segments: stale file job skipped")
		return nil
	}

	mediaItemID, ok, err := mediaItemIDForSegmentFile(ctx, q, lf)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	mi, err := q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return fmt.Errorf("get media_item %d: %w", mediaItemID, err)
	}

	segmentIDs := communitysegments.IDsFromMap(externalIDStrings(mi.ExternalIds))
	segmentIDs.Anime = mi.MediaType == sqlc.MediaTypeAnime

	durationMs := fileSnapshot.DurationMs

	w.Progress.SetCurrent(ScanMediaSegmentsFileArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(lf.Path))

	fetchCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	var cands []communitysegments.Candidate
	var season, episode int
	switch mi.MediaType {
	case sqlc.MediaTypeMovie:
		cands, _, err = w.Segments.Movie(fetchCtx, segmentIDs, durationMs)
	case sqlc.MediaTypeTv, sqlc.MediaTypeAnime:
		season, episode, ok = parseFirstEpisodeRef(lf.ParseResult)
		if !ok {
			// Extras/specials the parser couldn't address — mark done so the
			// pump doesn't revisit every sweep.
			return q.MarkFileSegmentsAnalyzed(ctx, lf.ID)
		}
		cands, _, err = w.Segments.Episode(fetchCtx, segmentIDs, season, episode, durationMs)
	default:
		return nil
	}
	if err != nil {
		return fmt.Errorf("community segments fetch: %w", err)
	}

	// The provider request is intentionally independent of HeyaMetadata, but
	// the timings are only valid for the exact local release whose duration we
	// sent. Re-read and re-stat before writing so a concurrent scan/replacement,
	// soft-delete, or unlink cannot attach stale timings to a different cut.
	currentFile, err := q.GetLibraryFileByID(ctx, lf.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("revalidate library_file %d: %w", lf.ID, err)
	}
	currentSnapshot, skipReason, err := currentSegmentFileSnapshot(currentFile)
	if err != nil {
		return fmt.Errorf("revalidate segment file %d: %w", lf.ID, err)
	}
	if skipReason != "" || !fileSnapshot.Equal(currentSnapshot) {
		if skipReason == "" {
			skipReason = "file_changed_during_fetch"
		}
		log.Info().
			Int64("library_file_id", lf.ID).
			Str("path", vfs.RedactPath(lf.Path)).
			Str("reason", skipReason).
			Msg("community segments: file changed before write; result discarded")
		return nil
	}
	currentMediaItemID, currentLinked, err := mediaItemIDForSegmentFile(ctx, q, currentFile)
	if err != nil {
		return err
	}
	if !currentLinked || currentMediaItemID != mediaItemID {
		log.Info().Int64("library_file_id", lf.ID).Msg("community segments: media identity changed before write; result discarded")
		return nil
	}
	currentMediaItem, err := q.GetMediaItemByID(ctx, currentMediaItemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("revalidate media_item %d: %w", currentMediaItemID, err)
	}
	currentSegmentIDs := communitysegments.IDsFromMap(externalIDStrings(currentMediaItem.ExternalIds))
	currentSegmentIDs.Anime = currentMediaItem.MediaType == sqlc.MediaTypeAnime
	if currentMediaItem.MediaType != mi.MediaType || currentSegmentIDs != segmentIDs {
		log.Info().Int64("library_file_id", lf.ID).Msg("community segments: provider identity changed before write; result discarded")
		return nil
	}
	if mi.MediaType == sqlc.MediaTypeTv || mi.MediaType == sqlc.MediaTypeAnime {
		currentSeason, currentEpisode, currentAddressable := parseFirstEpisodeRef(currentFile.ParseResult)
		if !currentAddressable || currentSeason != season || currentEpisode != episode {
			log.Info().Int64("library_file_id", lf.ID).Msg("community segments: episode coordinates changed before write; result discarded")
			return nil
		}
	}

	picked := pickSegments(cands, durationMs)

	tx, err := w.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := q.WithTx(tx)

	if err := writeCommunitySegments(ctx, qtx, lf.ID, picked); err != nil {
		return err
	}
	if err := qtx.MarkFileSegmentsAnalyzed(ctx, lf.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func mediaItemIDForSegmentFile(ctx context.Context, q *sqlc.Queries, lf sqlc.LibraryFile) (int64, bool, error) {
	if lf.MediaItemID.Valid {
		return lf.MediaItemID.Int64, true, nil
	}
	links, err := q.ListLibraryFileLinksByFile(ctx, lf.ID)
	if err != nil {
		return 0, false, fmt.Errorf("list library_file_links %d: %w", lf.ID, err)
	}
	for _, link := range links {
		if link.RelationType != "extra" {
			return link.MediaItemID, true, nil
		}
	}
	return 0, false, nil
}

// writeCommunitySegments applies the precedence-guarded insert for one
// file's picked community winners: it clears this worker's own prior
// community rows, then inserts each picked winner unless a manual row or
// an existing chromaprint measurement already covers that type.
//
// Precedence, final: manual beats everything; community and chromaprint
// are peers by arrival order — whichever wrote first for a given type
// wins, so this checks ExistsChromaprintMediaSegment before touching a
// type local detection already filled (mirrored by
// insertChromaprintSegmentIfAbsent's own
// ExistsCommunityMediaSegmentForFileAndType check on the other side).
// blackframe loses to both — the rank-aware upsert overwrites a
// blackframe row in place when writing the community winner. Only this
// worker's own (community:*) rows are cleared up front — the old
// DeleteMediaSegmentsForFile-then-reinsert flow also wiped a manual
// correction or a local-detection result on every re-check, even when
// the community databases had nothing new to say.
//
// ORDERING CONTRACT: the community:% delete MUST run before the inserts,
// in the same transaction. The upsert no-ops on equal rank, and a
// community row meeting its own prior community row is equal rank — the
// weekly re-check would silently fail to update values without the
// delete-first step. That holds here: DeleteCommunityMediaSegmentsForFile
// is the first statement, the inserts follow, one tx (the caller's).
//
// The EXISTS checks are read-committed short-circuits only — a
// concurrent chromaprint write (different queue, different job) can slip
// in between check and insert. UpsertMediaSegmentByRank is the source of
// truth: equal-rank conflict (the chromaprint peer got there first)
// no-ops, keeping the first-arriver. Commercial rows bypass the upsert
// via the plain insert (the unique index excludes them; multiple breaks
// per file are legitimate).
//
// Factored out of ScanMediaSegmentsFileWorker.Work so the precedence
// matrix (manual/chromaprint block, blackframe replace) is testable
// without live community-provider fetches.
func writeCommunitySegments(ctx context.Context, qtx *sqlc.Queries, fileID int64, picked []pickedSegment) error {
	if err := qtx.DeleteCommunityMediaSegmentsForFile(ctx, fileID); err != nil {
		return err
	}

	blockedByType := map[string]bool{}
	for _, p := range picked {
		blocked, checked := blockedByType[p.Type]
		if !checked {
			manual, err := qtx.ExistsManualMediaSegment(ctx, sqlc.ExistsManualMediaSegmentParams{
				LibraryFileID: fileID,
				SegmentType:   p.Type,
			})
			if err != nil {
				return err
			}
			chromaprint, err := qtx.ExistsChromaprintMediaSegment(ctx, sqlc.ExistsChromaprintMediaSegmentParams{
				LibraryFileID: fileID,
				SegmentType:   p.Type,
			})
			if err != nil {
				return err
			}
			blocked = manual || chromaprint
			blockedByType[p.Type] = blocked
		}
		if blocked {
			// A user-authored correction always wins. A chromaprint row
			// that landed first is a peer, not a subordinate — this
			// worker must not clobber it either (see the precedence note
			// above).
			continue
		}
		if p.Type == "commercial" {
			// Multiple commercial breaks per file are legitimate; the
			// unique index excludes them, so the plain insert applies.
			if err := qtx.InsertMediaSegment(ctx, sqlc.InsertMediaSegmentParams{
				LibraryFileID: fileID,
				SegmentType:   p.Type,
				StartMs:       p.StartMs,
				EndMs:         p.EndMs,
				Source:        p.Source,
			}); err != nil {
				return err
			}
			continue
		}
		if err := qtx.UpsertMediaSegmentByRank(ctx, sqlc.UpsertMediaSegmentByRankParams{
			LibraryFileID: fileID,
			SegmentType:   p.Type,
			StartMs:       p.StartMs,
			EndMs:         p.EndMs,
			Source:        p.Source,
		}); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// kickoff_media_segments
// ---------------------------------------------------------------------------

// Per-wave cap. Each job fans out to the applicable community providers, with
// per-source caches; the pump tops the bounded queue back up each wake.
const kickoffSegmentsFileBatch = 500

// KickoffMediaSegmentsWorker is the single-phase fingerprint-pump clone
// for skip segments: snooze-loop sweeping ListFilesPendingSegments with
// a cursor, one wave of scan_media_segments_file jobs in flight at a
// time.
type KickoffMediaSegmentsWorker struct {
	river.WorkerDefaults[KickoffMediaSegmentsArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *KickoffMediaSegmentsWorker) Work(ctx context.Context, job *river.Job[KickoffMediaSegmentsArgs]) error {
	taskID := job.Args.ScheduledTaskID
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)
	st := readPumpState(job.Metadata)
	fileKind := ScanMediaSegmentsFileArgs{}.Kind()

	if ctx.Err() != nil {
		return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
	}

	if stop, reason := pumpShouldStop(ctx, q, taskID, st.Source, job.CreatedAt); stop {
		switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
		case err != nil:
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		case !proceed:
			log.Info().Str("task", taskID).Msg("kickoff_media_segments: wind-down aborted — run upgraded to manual mid-wake")
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		cancelled, _ := queueops.CancelPendingByScheduledTask(ctx, w.DB, taskID, []string{fileKind})
		log.Info().Str("task", taskID).Str("reason", reason).Int64("cancelled_pending", cancelled).Msg("kickoff_media_segments: winding down")
		finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
		return nil
	}

	fileActive, err := pumpActiveCount(ctx, w.DB, taskID, fileKind)
	if err != nil {
		return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
	}
	filesListed := -1 // -1: wave full, sweep not consulted this wake
	if want := kickoffSegmentsFileBatch - fileActive; want > 0 {
		rows, err := q.ListFilesPendingSegments(ctx, sqlc.ListFilesPendingSegmentsParams{
			AfterID:  st.TrackCursor,
			RowLimit: int32(want),
		})
		if err != nil {
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		}
		filesListed = len(rows)
		for _, row := range rows {
			if ctx.Err() != nil {
				return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
			}
			w.Progress.Set("scan_media_segments", "kickoff_media_segments", row.Path)
			res, err := rc.Insert(ctx, ScanMediaSegmentsFileArgs{LibraryFileID: row.ID, ScheduledTaskID: taskID}, scheduledJobInsertOpts(st.Source))
			switch {
			case err != nil:
				log.Warn().Err(err).Int64("library_file_id", row.ID).Msg("kickoff_media_segments: enqueue failed")
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

	if fileActive == 0 && filesListed == 0 {
		if st.restartSweep() {
			log.Info().Str("task", taskID).Msg("kickoff_media_segments: re-sweeping for items skipped during the run")
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
		case err != nil:
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		case !proceed:
			log.Info().Str("task", taskID).Msg("kickoff_media_segments: finish aborted — run upgraded to manual mid-wake")
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		log.Info().Str("task", taskID).Int("enqueued", st.Enqueued).Int("failed", st.Failed).Msg("kickoff_media_segments: backlog drained")
		finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
		return nil
	}

	st.ErrStreak = 0
	return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
}
