package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaanalysis"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
)

// enqueueTrackLoudnessIfNeeded re-reads the row immediately before insertion.
// A previous scheduled batch may have completed after the pump built its
// candidate snapshot, and must not be resurrected as redundant queued work.
func enqueueTrackLoudnessIfNeeded(ctx context.Context, q *sqlc.Queries, client *river.Client[pgx.Tx], args ScanTrackLoudnessArgs, opts *river.InsertOpts) (enqueued, duplicate bool, err error) {
	tf, err := q.GetTrackFileByID(ctx, args.TrackFileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if !trackFileNeedsLoudness(tf) {
		return false, false, nil
	}
	result, err := client.Insert(ctx, args, opts)
	if err != nil {
		return false, false, err
	}
	return !result.UniqueSkippedAsDuplicate, result.UniqueSkippedAsDuplicate, nil
}

// ScanTrackLoudnessWorker runs ffmpeg's ebur128 filter on one audio file and
// writes integrated_lufs / true_peak_db / loudness_range_db / sample_peak_db
// back to its track_files row. After the write, checks whether every other
// track in the album has its own loudness — if so, enqueues the album-level
// worker. CPU-bound, runs on the dedicated `loudness` queue at MaxWorkers=1.
type ScanTrackLoudnessWorker struct {
	river.WorkerDefaults[ScanTrackLoudnessArgs]
	DB       *pgxpool.Pool
	Analysis *mediaanalysis.Service
	Progress *TaskProgressBroadcaster
}

func (w *ScanTrackLoudnessWorker) Work(ctx context.Context, job *river.Job[ScanTrackLoudnessArgs]) error {
	if err := snoozeIfMatchingPending(ctx, w.DB); err != nil {
		return err
	}

	q := sqlc.New(w.DB)

	tf, err := q.GetTrackFileByID(ctx, job.Args.TrackFileID)
	if err != nil {
		return fmt.Errorf("get track_file %d: %w", job.Args.TrackFileID, err)
	}

	lf, err := q.GetLibraryFileByID(ctx, tf.LibraryFileID)
	if err != nil {
		return fmt.Errorf("get library_file %d: %w", tf.LibraryFileID, err)
	}

	// Soft-deleted file? Drop silently. The matcher will requeue once a fresh
	// copy lands.
	if lf.DeletedAt.Valid {
		return nil
	}

	w.Progress.SetCurrent(ScanTrackLoudnessArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(lf.Path))

	if err := w.Analysis.EnsureTrackLoudness(ctx, tf.ID); err != nil {
		return err
	}

	// Cascade: if every track in the album now has loudness, enqueue the
	// album-level analysis. The unique-by-args guard prevents duplicates.
	track, err := q.GetTrackByID(ctx, tf.TrackID)
	if err == nil {
		done, err := q.AllAlbumTracksHaveLoudness(ctx, track.AlbumID)
		if err == nil && done {
			client := river.ClientFromContext[pgx.Tx](ctx)
			if client != nil {
				if _, err := client.Insert(ctx, ScanAlbumLoudnessArgs{AlbumID: track.AlbumID, ScheduledTaskID: job.Args.ScheduledTaskID}, scheduledJobInsertOpts(scheduledJobSource(job.Metadata))); err != nil {
					return fmt.Errorf("enqueue album loudness: %w", err)
				}
			}
		}
	}

	// Structural boundaries (intro/outro/fade/silence) for smart crossfade — a
	// cheap 8kHz decode. Best-effort and idempotent: skip if already done, and
	// never fail the job on a boundary error (for example, a temporarily
	// unavailable mounted path that ffmpeg cannot open).
	//
	// We ALWAYS stamp boundaries_analyzed_at after an attempt — including when
	// detection errors or yields nothing (too-short / undecodable). Otherwise the
	// widened pending-loudness backfill query would re-list this file every
	// kickoff tick forever. NULL ms columns mean "analyzed, no usable
	// boundaries"; the client falls back to a timed crossfade. A genuine ctx
	// cancellation also fails the stamp write below, so those correctly retry.
	return w.Analysis.EnsureTrackBoundaries(ctx, tf.ID)
}

// ScanAlbumLoudnessWorker runs ebur128 over the *concatenation* of every
// track in an album. Averaging per-track LUFS is mathematically wrong (LUFS
// is logarithmic), so we stitch the files end-to-end with ffmpeg's concat
// *filter* and measure the union — same numbers as loudgain / r128gain.
//
// The concat filter (not the concat demuxer) is load-bearing: the demuxer
// requires every file to share codec/parameters, which a mixed FLAC+MP3
// compilation violates (exit 69 mid-stream). The filter decodes each input
// independently; per-input aresample+aformat normalize rate and layout so
// heterogeneous albums concatenate cleanly. Forcing stereo also matches the
// ReplayGain 2.0 convention of measuring mono as dual-mono.
type ScanAlbumLoudnessWorker struct {
	river.WorkerDefaults[ScanAlbumLoudnessArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *ScanAlbumLoudnessWorker) Work(ctx context.Context, job *river.Job[ScanAlbumLoudnessArgs]) error {
	if err := snoozeIfMatchingPending(ctx, w.DB); err != nil {
		return err
	}

	q := sqlc.New(w.DB)

	rows, err := q.ListAlbumTrackFilesForLoudness(ctx, job.Args.AlbumID)
	if err != nil {
		return fmt.Errorf("list album files: %w", err)
	}
	if len(rows) == 0 {
		// All tracks soft-deleted or no track_files; nothing to measure.
		return nil
	}

	if album, err := q.GetAlbumByID(ctx, job.Args.AlbumID); err == nil {
		w.Progress.SetCurrent(ScanAlbumLoudnessArgs{}.Kind(), job.Args.ScheduledTaskID, album.Title)
	}

	paths := make([]string, len(rows))
	for i, r := range rows {
		if err := vfs.ValidateLocalPath(r.Path); err != nil {
			return fmt.Errorf("album audio input: %w", err)
		}
		paths[i] = r.Path
	}

	// Album measurement can take meaningfully longer than per-track —
	// 60 min album at ~15× real-time is ~4 min. Cap generously.
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, //nolint:gosec // paths come from library_files we control, ffmpeg binary is fixed
		"ffmpeg", albumEBUR128Args(paths)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg ebur128 album: %w", err)
	}

	result, err := mediaanalysis.ParseEBUR128(stderr.String())
	if err != nil {
		return fmt.Errorf("parse ebur128 output for album %d: %w", job.Args.AlbumID, err)
	}

	if err := q.UpdateAlbumLoudness(ctx, sqlc.UpdateAlbumLoudnessParams{
		ID:              job.Args.AlbumID,
		IntegratedLufs:  pgNumericFromFloat(result.IntegratedLUFS),
		TruePeakDb:      pgNumericFromFloat(result.TruePeakDB),
		LoudnessRangeDb: pgNumericFromFloat(result.LoudnessRangeDB),
	}); err != nil {
		return fmt.Errorf("update album loudness: %w", err)
	}

	return nil
}

// snoozeIfMatchingPending returns a snooze error when there's outstanding
// match/scan work, so the loudness queue yields its single worker slot
// (and its CPU appetite) to the critical-path queues. River reschedules
// snoozed jobs after the requested delay without counting it as a failure.
//
// "Matching work" = any non-final scanner/enrich work. We don't care about
// queue boundaries here — these run in parallel queues but all compete for the
// same CPU/disk/heya.media bandwidth as loudness scanning.
func snoozeIfMatchingPending(ctx context.Context, db *pgxpool.Pool) error {
	return snoozeIfKindsPending(ctx, db, []string{"kickoff_library_scan", "process_scan", "search_metadata", "fetch_metadata", "apply_metadata", "enrich_media_item"})
}

// snoozeIfScannerPipelinePending is the lighter readiness gate for work that
// depends on a stable file identity but not on full upstream enrichment.
// Community segment lookup only needs the already-applied provider identity
// and the file's current probed duration, so an unrelated enrich backlog must
// not hold it hostage.
func snoozeIfScannerPipelinePending(ctx context.Context, db *pgxpool.Pool) error {
	return snoozeIfKindsPending(ctx, db, []string{"kickoff_library_scan", "process_scan", "search_metadata", "fetch_metadata", "apply_metadata"})
}

func snoozeIfKindsPending(ctx context.Context, db *pgxpool.Pool, kinds []string) error {
	n, err := queueops.CountActiveByKinds(ctx, db, kinds)
	if err != nil {
		// Don't block background work on a transient DB hiccup — better to
		// continue than to wedge the queue entirely.
		return nil
	}
	if n == 0 {
		return nil
	}
	// Long enough to avoid bouncing queue slots while the scanner is busy,
	// short enough to resume promptly once matching settles down.
	return river.JobSnooze(60 * time.Second)
}

// pgNumericFromFloat encodes a float64 into pgtype.Numeric for write-back.
// We round-trip through string so we don't have to deal with the Exp/Int
// pair manually — fast enough for once-per-track work.
func pgNumericFromFloat(v float64) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(strconv.FormatFloat(v, 'f', 2, 64))
	return n
}

// albumEBUR128Args builds the full ffmpeg argv for a whole-album loudness
// measurement: one -i per file, each normalized to 48kHz float stereo, then
// concat filter → ebur128. Paths ride in argv, so no quoting/escaping layer
// exists to corrupt them (the previous concat-demuxer manifest mangled
// non-ASCII paths and choked on mixed-codec albums).
func albumEBUR128Args(paths []string) []string {
	args := make([]string, 0, 3*len(paths)+8)
	args = append(args, "-nostdin", "-nostats", "-hide_banner")
	var fc strings.Builder
	for i, p := range paths {
		args = append(args, "-i", p)
		fmt.Fprintf(&fc, "[%d:a:0]aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo[a%d];", i, i)
	}
	for i := range paths {
		fmt.Fprintf(&fc, "[a%d]", i)
	}
	fmt.Fprintf(&fc, "concat=n=%d:v=0:a=1,ebur128=peak=true", len(paths))
	return append(args, "-filter_complex", fc.String(), "-f", "null", "-")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
