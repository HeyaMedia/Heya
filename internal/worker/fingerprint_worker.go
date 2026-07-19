package worker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// Chromaprint audio fingerprinting — the data layer for duplicate-recording
// detection (the "same song released under a Japanese and an English title"
// problem) and, later, fingerprint submission to heya.media.
//
// Only the first chromaprintWindowSecs of audio are fingerprinted (fpcalc's
// default window): plenty to identify a recording, and it keeps a pass over a
// full library cheap (~1-2s/file vs a full decode). The stored form is
// chromaprint's base64 compressed fingerprint — the AcoustID interchange
// format (URL-safe alphabet, no padding) — normalized to that alphabet no
// matter which extractor produced it.

const (
	// chromaprintAlgorithm is chromaprint's TEST2, the default of both fpcalc
	// and ffmpeg's chromaprint muxer. Recorded per row so a future algorithm
	// bump knows which fingerprints to regenerate.
	chromaprintAlgorithm = 1
	// fpcalc's CLI numbers algorithms from 1 while libchromaprint/FFmpeg use
	// the zero-based enum stored in our database. CLI value 2 is therefore the
	// same TEST2 algorithm as FFmpeg's muxer value 1.
	fpcalcAlgorithm = chromaprintAlgorithm + 1
	// chromaprintWindowSecs caps how much audio is decoded and fingerprinted.
	chromaprintWindowSecs = 120
)

// ScanTrackFingerprintWorker computes a chromaprint for one audio file and
// stores it against library_files so matching can consume it before a track
// exists. Fingerprints are physical-file evidence and have one canonical row.
type ScanTrackFingerprintWorker struct {
	river.WorkerDefaults[ScanTrackFingerprintArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *ScanTrackFingerprintWorker) Work(ctx context.Context, job *river.Job[ScanTrackFingerprintArgs]) error {
	if err := snoozeIfMatchingPending(ctx, w.DB); err != nil {
		return err
	}

	q := sqlc.New(w.DB)

	lf, tf, hasTrack, err := fingerprintJobFile(ctx, q, job.Args)
	if err != nil {
		return err
	}

	// Soft-deleted file? Drop silently. The matcher will requeue once a fresh
	// copy lands.
	if lf.DeletedAt.Valid {
		return nil
	}

	w.Progress.SetCurrent(ScanTrackFingerprintArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(lf.Path))

	// A valid file-level row is authoritative; no decode is needed.
	if stored, storedErr := q.GetLibraryFileFingerprint(ctx, lf.ID); storedErr == nil && libraryFingerprintCurrent(stored, lf) {
		return nil
	} else if storedErr != nil && !errors.Is(storedErr, pgx.ErrNoRows) {
		return fmt.Errorf("get library_file fingerprint %d: %w", lf.ID, storedErr)
	}

	sourceDuration := int32(0)
	if hasTrack {
		sourceDuration = tf.Duration
	}
	_, err = ensureLibraryFileFingerprint(ctx, q, lf, sourceDuration)
	return err
}

func fingerprintJobFile(ctx context.Context, q *sqlc.Queries, args ScanTrackFingerprintArgs) (sqlc.LibraryFile, sqlc.TrackFile, bool, error) {
	if args.LibraryFileID > 0 {
		lf, err := q.GetLibraryFileByID(ctx, args.LibraryFileID)
		if err != nil {
			return sqlc.LibraryFile{}, sqlc.TrackFile{}, false, fmt.Errorf("get library_file %d: %w", args.LibraryFileID, err)
		}
		tf, trackErr := q.GetTrackFileByLibraryFileID(ctx, lf.ID)
		if errors.Is(trackErr, pgx.ErrNoRows) {
			return lf, sqlc.TrackFile{}, false, nil
		}
		if trackErr != nil {
			return sqlc.LibraryFile{}, sqlc.TrackFile{}, false, fmt.Errorf("get track_file for library_file %d: %w", lf.ID, trackErr)
		}
		return lf, tf, true, nil
	}
	if args.TrackFileID <= 0 {
		return sqlc.LibraryFile{}, sqlc.TrackFile{}, false, errors.New("fingerprint job requires library_file_id or track_file_id")
	}
	tf, err := q.GetTrackFileByID(ctx, args.TrackFileID)
	if err != nil {
		return sqlc.LibraryFile{}, sqlc.TrackFile{}, false, fmt.Errorf("get track_file %d: %w", args.TrackFileID, err)
	}
	lf, err := q.GetLibraryFileByID(ctx, tf.LibraryFileID)
	if err != nil {
		return sqlc.LibraryFile{}, sqlc.TrackFile{}, false, fmt.Errorf("get library_file %d: %w", tf.LibraryFileID, err)
	}
	return lf, tf, true, nil
}

func libraryFingerprintCurrent(fp sqlc.LibraryFileFingerprint, lf sqlc.LibraryFile) bool {
	if fp.Algorithm != chromaprintAlgorithm || fp.SourceSize != lf.Size || fp.Fingerprint == "" {
		return false
	}
	if fp.SourceMtime.Valid != lf.Mtime.Valid {
		return false
	}
	return !fp.SourceMtime.Valid || fp.SourceMtime.Time.Equal(lf.Mtime.Time)
}

// ensureLibraryFileFingerprint is shared by the background corpus sweep and
// the matcher's on-demand uncertainty path. The fast name/release matcher never
// calls it; only a candidate headed for review pays the audio decode cost.
func ensureLibraryFileFingerprint(ctx context.Context, q *sqlc.Queries, lf sqlc.LibraryFile, sourceDuration int32) (sqlc.LibraryFileFingerprint, error) {
	stored, err := q.GetLibraryFileFingerprint(ctx, lf.ID)
	if err == nil && libraryFingerprintCurrent(stored, lf) && stored.SourceDurationSecs > 0 {
		return stored, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return sqlc.LibraryFileFingerprint{}, fmt.Errorf("get library_file fingerprint %d: %w", lf.ID, err)
	}

	// Decoding ≤120s of audio is seconds of work; 2 minutes covers a wedged
	// network-mount read without holding a search worker indefinitely.
	fpCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	fingerprint, err := computeChromaprint(fpCtx, lf.Path)
	fpCtxErr := fpCtx.Err()
	cancel()
	if err != nil {
		if fpCtxErr != nil {
			return sqlc.LibraryFileFingerprint{}, fpCtxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return sqlc.LibraryFileFingerprint{}, err
		}
		return sqlc.LibraryFileFingerprint{}, fmt.Errorf("compute chromaprint: %w", err)
	}
	if sourceDuration <= 0 {
		var info mediaprobe.MediaInfo
		if len(lf.MediaInfo) > 2 && json.Unmarshal(lf.MediaInfo, &info) == nil && info.Duration > 0 {
			sourceDuration = int32(math.Round(info.Duration))
		}
	}
	if sourceDuration <= 0 {
		probeCtx, probeCancel := context.WithTimeout(ctx, 2*time.Minute)
		info, probeErr := probeFingerprintMedia(probeCtx, lf.Path)
		probeCtxErr := probeCtx.Err()
		probeCancel()
		if probeErr != nil {
			if probeCtxErr != nil {
				return sqlc.LibraryFileFingerprint{}, probeCtxErr
			}
			if errors.Is(probeErr, context.Canceled) || errors.Is(probeErr, context.DeadlineExceeded) {
				return sqlc.LibraryFileFingerprint{}, probeErr
			}
			return sqlc.LibraryFileFingerprint{}, fmt.Errorf("probe fingerprint source duration: %w", probeErr)
		}
		sourceDuration = int32(math.Round(info.Duration))
	}
	if sourceDuration <= 0 {
		return sqlc.LibraryFileFingerprint{}, errors.New("fingerprint source duration is unavailable")
	}

	stored, err = q.UpsertLibraryFileFingerprint(ctx, sqlc.UpsertLibraryFileFingerprintParams{
		LibraryFileID: lf.ID, Algorithm: chromaprintAlgorithm, Fingerprint: fingerprint,
		FingerprintDurationSecs: min(sourceDuration, int32(chromaprintWindowSecs)),
		SourceDurationSecs:      sourceDuration,
		SourceSize:              lf.Size,
		SourceMtime:             lf.Mtime,
	})
	if err != nil {
		return sqlc.LibraryFileFingerprint{}, fmt.Errorf("update library_file fingerprint: %w", err)
	}
	return stored, nil
}

// fpMethod is how this host can compute chromaprints. Detected once per
// process: prod's jellyfin-ffmpeg ships the chromaprint muxer; dev macOS
// (brew ffmpeg) lacks it but has the standalone fpcalc from `brew install
// chromaprint`. Both default to algorithm TEST2 and the same 120s window, so
// fingerprints are comparable regardless of which produced them.
type fpMethod int

const (
	fpNone fpMethod = iota
	fpFFmpeg
	fpFpcalc
)

var detectFpMethod = sync.OnceValue(func() fpMethod {
	probe := exec.Command("ffmpeg", "-hide_banner", "-h", "muxer=chromaprint")
	var out bytes.Buffer
	probe.Stdout = &out
	probe.Stderr = &out
	if err := probe.Run(); err == nil && strings.Contains(out.String(), "Muxer chromaprint") {
		return fpFFmpeg
	}
	if _, err := exec.LookPath("fpcalc"); err == nil {
		log.Info().Msg("chromaprint: ffmpeg muxer unavailable, using fpcalc")
		return fpFpcalc
	}
	log.Warn().Msg("chromaprint: neither ffmpeg muxer nor fpcalc available; fingerprinting disabled")
	return fpNone
})

// chromaprintFile computes the base64 compressed chromaprint of the first
// chromaprintWindowSecs of an audio file.
func chromaprintFile(ctx context.Context, path string) (string, error) {
	switch detectFpMethod() {
	case fpFFmpeg:
		return chromaprintViaFFmpeg(ctx, path)
	case fpFpcalc:
		return chromaprintViaFpcalc(ctx, path)
	default:
		return "", errors.New("no chromaprint extractor available (need ffmpeg with chromaprint muxer, or fpcalc)")
	}
}

var computeChromaprint = chromaprintFile
var probeFingerprintMedia = mediaprobe.Probe

func chromaprintViaFFmpeg(ctx context.Context, path string) (string, error) {
	if err := vfs.ValidateLocalPath(path); err != nil {
		return "", fmt.Errorf("audio input: %w", err)
	}

	// -t before -i bounds the demux read itself. -vn/-sn/-dn plus an explicit
	// audio map keep an embedded cover-art stream from reaching the
	// audio-only chromaprint muxer.
	cmd := exec.CommandContext(ctx, //nolint:gosec // path comes from library_files we control; ffmpeg binary is fixed
		"ffmpeg",
		"-nostdin", "-nostats", "-hide_banner",
		"-t", fmt.Sprint(chromaprintWindowSecs),
		"-i", path,
		"-vn", "-sn", "-dn", "-map", "0:a:0",
		"-f", "chromaprint",
		"-algorithm", strconv.Itoa(chromaprintAlgorithm),
		"-fp_format", "base64", "-",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg chromaprint: %w (stderr: %s)", err, truncate(stderr.String(), 300))
	}
	return normalizeChromaprint(strings.TrimSpace(stdout.String()))
}

func chromaprintViaFpcalc(ctx context.Context, path string) (string, error) {
	if err := vfs.ValidateLocalPath(path); err != nil {
		return "", fmt.Errorf("audio input: %w", err)
	}
	cmd := exec.CommandContext(ctx, //nolint:gosec // path comes from library_files we control; fpcalc binary is fixed
		"fpcalc",
		"-length", fmt.Sprint(chromaprintWindowSecs),
		"-algorithm", strconv.Itoa(fpcalcAlgorithm),
		path,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("fpcalc: %w (stderr: %s)", err, truncate(stderr.String(), 300))
	}
	// Output is KEY=VALUE lines; we want FINGERPRINT=.
	for _, line := range strings.Split(stdout.String(), "\n") {
		if fp, ok := strings.CutPrefix(strings.TrimSpace(line), "FINGERPRINT="); ok && fp != "" {
			return normalizeChromaprint(fp)
		}
	}
	return "", errors.New("fpcalc: no FINGERPRINT in output")
}

// ---------------------------------------------------------------------------
// kickoff_music_fingerprint
// ---------------------------------------------------------------------------

// Per-wave cap. Fingerprinting is fast (~1-2s/file: only the first 120s are
// decoded), so the MaxWorkers=1 queue chews a 500-wave quickly; the pump tops
// the wave back up each wake, keeping the job table bounded regardless of
// backlog size.
const kickoffFingerprintTrackBatch = 500

// KickoffMusicFingerprintWorker sweeps every physical music file, including
// unmatched files which have no track_files row yet.
type KickoffMusicFingerprintWorker struct {
	river.WorkerDefaults[KickoffMusicFingerprintArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *KickoffMusicFingerprintWorker) Work(ctx context.Context, job *river.Job[KickoffMusicFingerprintArgs]) error {
	taskID := job.Args.ScheduledTaskID
	q := sqlc.New(w.DB)
	rc := river.ClientFromContext[pgx.Tx](ctx)
	st := readPumpState(job.Metadata)
	trackKind := ScanTrackFingerprintArgs{}.Kind()

	if ctx.Err() != nil {
		return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
	}

	if stop, reason := pumpShouldStop(ctx, q, taskID, st.Source, job.CreatedAt); stop {
		switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
		case err != nil:
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		case !proceed:
			log.Info().Str("task", taskID).Msg("kickoff_music_fingerprint: wind-down aborted — run upgraded to manual mid-wake")
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		cancelled, _ := queueops.CancelPendingByScheduledTask(ctx, w.DB, taskID, []string{trackKind})
		log.Info().Str("task", taskID).Str("reason", reason).Int64("cancelled_pending", cancelled).Msg("kickoff_music_fingerprint: winding down")
		finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
		return nil
	}

	// Keep one wave of per-track jobs topped up, sweeping the pending set in
	// id order exactly once.
	trackActive, err := pumpActiveCount(ctx, w.DB, taskID, trackKind)
	if err != nil {
		return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
	}
	tracksListed := -1 // -1: wave full, sweep not consulted this wake
	if want := kickoffFingerprintTrackBatch - trackActive; want > 0 {
		rows, err := q.ListMusicLibraryFilesPendingFingerprint(ctx, sqlc.ListMusicLibraryFilesPendingFingerprintParams{
			AfterID:  st.TrackCursor,
			RowLimit: int32(want),
		})
		if err != nil {
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		}
		tracksListed = len(rows)
		if len(rows) > 0 {
			if ctx.Err() != nil {
				return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
			}
			last := rows[len(rows)-1]
			w.Progress.Set("scan_music_fingerprint", "kickoff_music_fingerprint", last.Path)
			jobs := make([]river.InsertManyParams, len(rows))
			for i, row := range rows {
				jobs[i] = river.InsertManyParams{
					Args:       ScanTrackFingerprintArgs{LibraryFileID: row.ID, ScheduledTaskID: taskID},
					InsertOpts: scheduledJobInsertOpts(st.Source),
				}
			}
			results, err := rc.InsertMany(ctx, jobs)
			if err != nil {
				log.Warn().Err(err).Int("track_count", len(rows)).Msg("kickoff_music_fingerprint: batch enqueue failed")
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
			st.TrackCursor = last.ID
		}
	}

	if trackActive == 0 && tracksListed == 0 {
		if st.restartSweep() {
			log.Info().Str("task", taskID).Msg("kickoff_music_fingerprint: re-sweeping for items skipped during the run")
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		switch proceed, err := pumpFinishHandshake(ctx, w.DB, job.ID, &st); {
		case err != nil:
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		case !proceed:
			log.Info().Str("task", taskID).Msg("kickoff_music_fingerprint: finish aborted — run upgraded to manual mid-wake")
			st.ErrStreak = 0
			return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
		}
		log.Info().Str("task", taskID).Int("enqueued", st.Enqueued).Int("failed", st.Failed).Msg("kickoff_music_fingerprint: backlog drained")
		finishKickoff(ctx, q, taskID, job.CreatedAt, st.Enqueued, st.Failed, nil)
		return nil
	}

	st.ErrStreak = 0
	return pumpSnooze(ctx, w.DB, job.ID, taskID, st)
}

// normalizeChromaprint re-encodes a base64 compressed fingerprint into the
// AcoustID convention: URL-safe alphabet, no padding. Both extractors emit
// that already (ffmpeg's muxer delegates to chromaprint's own encoder —
// verified byte-identical to fpcalc on the same file), so this is normally a
// validating passthrough; the standard-alphabet branches are insurance
// against a future tool change, and any invalid output fails loudly here
// instead of storing garbage.
func normalizeChromaprint(s string) (string, error) {
	if s == "" {
		return "", errors.New("empty fingerprint")
	}
	if raw, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return base64.RawURLEncoding.EncodeToString(raw), nil
	}
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil {
		return base64.RawURLEncoding.EncodeToString(raw), nil
	}
	if raw, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return base64.RawURLEncoding.EncodeToString(raw), nil
	}
	return "", fmt.Errorf("fingerprint is not valid base64 (%d chars)", len(s))
}
