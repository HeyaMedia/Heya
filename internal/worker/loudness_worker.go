package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// ScanTrackLoudnessWorker runs ffmpeg's ebur128 filter on one audio file and
// writes integrated_lufs / true_peak_db / loudness_range_db / sample_peak_db
// back to its track_files row. After the write, checks whether every other
// track in the album has its own loudness — if so, enqueues the album-level
// worker. CPU-bound, runs on the dedicated `loudness` queue at MaxWorkers=1.
type ScanTrackLoudnessWorker struct {
	river.WorkerDefaults[ScanTrackLoudnessArgs]
	DB       *pgxpool.Pool
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

	// Cap wall-clock for the ffmpeg passes. 20× real-time on a modern CPU for
	// FLAC, so a 10-minute track lands in ~30s; 5 min covers worst-case lossy.
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Loudness (ebur128). Skip when already measured — this worker also runs as
	// a boundary-only backfill, and re-listing loud-but-boundary-less tracks
	// shouldn't pay for a redundant full-rate analysis pass.
	if !tf.IntegratedLufs.Valid {
		result, err := runEBUR128(probeCtx, lf.Path)
		if err != nil {
			return fmt.Errorf("ebur128 %s: %w", lf.Path, err)
		}

		if err := q.UpdateTrackFileLoudness(ctx, sqlc.UpdateTrackFileLoudnessParams{
			ID:              tf.ID,
			IntegratedLufs:  pgNumericFromFloat(result.IntegratedLUFS),
			TruePeakDb:      pgNumericFromFloat(result.TruePeakDB),
			LoudnessRangeDb: pgNumericFromFloat(result.LoudnessRangeDB),
			SamplePeakDb:    pgNumericFromFloat(result.SamplePeakDB),
		}); err != nil {
			return fmt.Errorf("update track_file loudness: %w", err)
		}

		// Cascade: if every track in the album now has loudness, enqueue the
		// album-level analysis. The unique-by-args guard on the worker means
		// concurrent track workers can't double-enqueue.
		track, err := q.GetTrackByID(ctx, tf.TrackID)
		if err == nil {
			done, err := q.AllAlbumTracksHaveLoudness(ctx, track.AlbumID)
			if err == nil && done {
				client := river.ClientFromContext[pgx.Tx](ctx)
				if client != nil {
					if _, err := client.Insert(ctx, ScanAlbumLoudnessArgs{AlbumID: track.AlbumID, ScheduledTaskID: job.Args.ScheduledTaskID}, nil); err != nil {
						return fmt.Errorf("enqueue album loudness: %w", err)
					}
				}
			}
		}
	}

	// Structural boundaries (intro/outro/fade/silence) for smart crossfade — a
	// cheap 8kHz decode. Best-effort and idempotent: skip if already done, and
	// never fail the job on a boundary error (e.g. an SMB path ffmpeg can't open).
	//
	// We ALWAYS stamp boundaries_analyzed_at after an attempt — including when
	// detection errors or yields nothing (too-short / undecodable). Otherwise the
	// widened pending-loudness backfill query would re-list this file every
	// kickoff tick forever. NULL ms columns mean "analyzed, no usable
	// boundaries"; the client falls back to a timed crossfade. A genuine ctx
	// cancellation also fails the stamp write below, so those correctly retry.
	if !tf.BoundariesAnalyzedAt.Valid {
		params := sqlc.UpdateTrackFileBoundariesParams{ID: tf.ID}
		if b, berr := sonicanalysis.DetectBoundaries(probeCtx, lf.Path); berr != nil {
			log.Debug().Err(berr).Str("path", lf.Path).Msg("boundary detection failed; marking analyzed with no boundaries")
		} else if b != nil {
			params.IntroEndMs = pgInt4(b.IntroEndMs)
			params.OutroStartMs = pgInt4(b.OutroStartMs)
			params.FadeStartMs = pgInt4(b.FadeStartMs)
			params.SilenceStartMs = pgInt4(b.SilenceStartMs)
		}
		// Surface a stamp-write failure rather than swallowing it: loudness has
		// already committed independently, so River retries the job, skips the
		// loudness pass (now present), and re-attempts only the boundary stamp.
		if err := q.UpdateTrackFileBoundaries(ctx, params); err != nil {
			return fmt.Errorf("update track_file boundaries: %w", err)
		}
	}

	return nil
}

// ScanAlbumLoudnessWorker runs ebur128 over the *concatenation* of every
// track in an album. Averaging per-track LUFS is mathematically wrong (LUFS
// is logarithmic), so we lean on ffmpeg's concat demuxer to virtually stitch
// the files end-to-end and measure the union — same approach as loudgain /
// r128gain.
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

	// Pre-flight: ebur128's concat demuxer can't read SMB paths through our
	// VFS, so if any file is remote bail. Picking the up-front would require
	// streaming through a pipe per file, which defeats the concat-once model.
	// SMB libraries will need album loudness once we have a local-cache step.
	for _, r := range rows {
		if vfs.IsSMBPath(r.Path) {
			log.Debug().Int64("album_id", job.Args.AlbumID).Msg("skipping album loudness: SMB file in set")
			return nil
		}
	}

	// Write a temporary concat manifest. ffmpeg's "concat" demuxer reads
	// lines like `file '/abs/path/to/track.flac'`. Single-quoting handles
	// spaces; we escape any single quote inside the path the ffmpeg way
	// (close, escape, reopen).
	dir, err := os.MkdirTemp("", "heya-loudness-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	manifestPath := filepath.Join(dir, "concat.txt")
	var manifest bytes.Buffer
	for _, r := range rows {
		manifest.WriteString("file '")
		manifest.WriteString(ffmpegConcatEscape(r.Path))
		manifest.WriteString("'\n")
	}
	if err := os.WriteFile(manifestPath, manifest.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	// Album measurement can take meaningfully longer than per-track —
	// 60 min album at ~15× real-time is ~4 min. Cap generously.
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, //nolint:gosec // manifestPath is a tempdir we just created, ffmpeg binary is fixed
		"ffmpeg",
		"-nostdin", "-nostats", "-hide_banner",
		"-f", "concat", "-safe", "0",
		"-i", manifestPath,
		"-af", "ebur128=peak=true",
		"-f", "null", "-",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg ebur128 album: %w", err)
	}

	result, err := parseEBUR128(stderr.String())
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
// "Matching work" = any non-final ProcessFile / MetadataMatch /
// MetadataFetch / RefreshMusicArtist job. We don't care about queue
// boundaries here — these run in parallel queues but all compete for the
// same CPU/disk/heya.media bandwidth as loudness scanning.
func snoozeIfMatchingPending(ctx context.Context, db *pgxpool.Pool) error {
	n, err := queueops.CountActiveByKinds(ctx, db, []string{"process_file", "metadata_match", "metadata_fetch", "refresh_music_artist"})
	if err != nil {
		// Don't block loudness work on a transient DB hiccup — better to
		// run loudness than to wedge the queue entirely.
		return nil
	}
	if n == 0 {
		return nil
	}
	// 60s feels like the right tradeoff: long enough that we don't waste
	// loudness's MaxWorkers=1 slot bouncing back and forth, short enough
	// that we resume promptly when matching settles down.
	return river.JobSnooze(60 * time.Second)
}

// ebur128Result holds the four numbers ffmpeg's ebur128 summary reports.
type ebur128Result struct {
	IntegratedLUFS  float64
	TruePeakDB      float64
	LoudnessRangeDB float64
	SamplePeakDB    float64
}

// runEBUR128 invokes ffmpeg's ebur128 filter on a single file and parses its
// summary output. Output lives on stderr; the null muxer eats stdout.
func runEBUR128(ctx context.Context, path string) (ebur128Result, error) {
	cmd := exec.CommandContext(ctx, //nolint:gosec // path comes from library_files we control; ffmpeg binary is fixed
		"ffmpeg",
		"-nostdin", "-nostats", "-hide_banner",
		"-i", path,
		"-af", "ebur128=peak=true",
		"-f", "null", "-",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return ebur128Result{}, fmt.Errorf("ffmpeg ebur128: %w (stderr: %s)", err, truncate(stderr.String(), 300))
	}
	return parseEBUR128(stderr.String())
}

var (
	reIntegrated = regexp.MustCompile(`(?m)^\s*I:\s*(-?\d+(?:\.\d+)?)\s*LUFS`)
	reLRA        = regexp.MustCompile(`(?m)^\s*LRA:\s*(-?\d+(?:\.\d+)?)\s*LU`)
	// "Sample peak:" or "True peak:" line, then optional channel "Peak: <N>
	// dBFS" lines. We grab the first dBFS number that follows the section
	// header — represents either the overall peak or channel 0, which is
	// what loudgain / r128gain use too.
	reSamplePeak = regexp.MustCompile(`(?s)Sample peak:.*?Peak:\s*(-?\d+(?:\.\d+)?)\s*dBFS`)
	reTruePeak   = regexp.MustCompile(`(?s)True peak:.*?Peak:\s*(-?\d+(?:\.\d+)?)\s*dBFS`)
)

func parseEBUR128(output string) (ebur128Result, error) {
	// ffmpeg emits per-second progress lines plus the Summary block at the
	// end. Slice from "Summary:" so we don't catch streaming peaks.
	if idx := bytes.Index([]byte(output), []byte("Summary:")); idx >= 0 {
		output = output[idx:]
	}

	res := ebur128Result{}
	var ok bool

	if m := reIntegrated.FindStringSubmatch(output); len(m) == 2 {
		res.IntegratedLUFS, _ = strconv.ParseFloat(m[1], 64)
		ok = true
	}
	if m := reLRA.FindStringSubmatch(output); len(m) == 2 {
		res.LoudnessRangeDB, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reTruePeak.FindStringSubmatch(output); len(m) == 2 {
		res.TruePeakDB, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reSamplePeak.FindStringSubmatch(output); len(m) == 2 {
		res.SamplePeakDB, _ = strconv.ParseFloat(m[1], 64)
	}

	if !ok {
		return res, errors.New("ebur128 summary not found in output")
	}
	return res, nil
}

// pgNumericFromFloat encodes a float64 into pgtype.Numeric for write-back.
// We round-trip through string so we don't have to deal with the Exp/Int
// pair manually — fast enough for once-per-track work.
func pgNumericFromFloat(v float64) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(strconv.FormatFloat(v, 'f', 2, 64))
	return n
}

// pgInt4 wraps a non-null int32 for nullable INTEGER write-back.
func pgInt4(v int) pgtype.Int4 {
	return pgtype.Int4{Int32: int32(v), Valid: true}
}

// ffmpegConcatEscape escapes a path for use inside single-quoted concat
// manifest entries. Single quotes inside the path become `'\”`.
func ffmpegConcatEscape(path string) string {
	out := make([]byte, 0, len(path))
	for _, r := range path {
		if r == '\'' {
			out = append(out, []byte(`'\''`)...)
			continue
		}
		out = append(out, byte(r))
	}
	return string(out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
