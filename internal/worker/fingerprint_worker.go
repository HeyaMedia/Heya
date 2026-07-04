package worker

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/queueops"
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
	// chromaprintWindowSecs caps how much audio is decoded and fingerprinted.
	chromaprintWindowSecs = 120
)

// ScanTrackFingerprintWorker computes a chromaprint for one audio file and
// writes it back to its track_files row. CPU-bound but light (decodes ≤120s),
// runs on its own queue at MaxWorkers=1 like the other analysis passes.
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

	tf, err := q.GetTrackFileByID(ctx, job.Args.TrackFileID)
	if err != nil {
		return fmt.Errorf("get track_file %d: %w", job.Args.TrackFileID, err)
	}

	// Already fingerprinted (e.g. re-enqueued by a final sweep)? Done.
	if tf.FingerprintedAt.Valid {
		return nil
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

	w.Progress.SetCurrent(ScanTrackFingerprintArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(lf.Path))

	// Decoding ≤120s of audio is seconds of work; 2 minutes covers a wedged
	// SMB read without holding the queue's single slot hostage.
	fpCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	fp, err := chromaprintFile(fpCtx, lf.Path)
	if err != nil {
		return fmt.Errorf("chromaprint %s: %w", lf.Path, err)
	}

	// Window actually covered: the full file when it's shorter than the cap.
	window := int32(chromaprintWindowSecs)
	if tf.Duration > 0 && tf.Duration < window {
		window = tf.Duration
	}

	if err := q.UpdateTrackFileFingerprint(ctx, sqlc.UpdateTrackFileFingerprintParams{
		ID:                      tf.ID,
		Chromaprint:             pgtype.Text{String: fp, Valid: true},
		ChromaprintAlgorithm:    pgtype.Int2{Int16: chromaprintAlgorithm, Valid: true},
		ChromaprintDurationSecs: pgtype.Int4{Int32: window, Valid: true},
	}); err != nil {
		return fmt.Errorf("update track_file fingerprint: %w", err)
	}

	return nil
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

func chromaprintViaFFmpeg(ctx context.Context, path string) (string, error) {
	// -t before -i bounds the demux read itself. -vn/-sn/-dn plus an explicit
	// audio map keep an embedded cover-art stream from reaching the
	// audio-only chromaprint muxer.
	cmd := exec.CommandContext(ctx, //nolint:gosec // path comes from library_files we control; ffmpeg binary is fixed
		"ffmpeg",
		"-nostdin", "-nostats", "-hide_banner",
		"-t", fmt.Sprint(chromaprintWindowSecs),
		"-i", path,
		"-vn", "-sn", "-dn", "-map", "0:a:0",
		"-f", "chromaprint", "-fp_format", "base64", "-",
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
	cmd := exec.CommandContext(ctx, //nolint:gosec // path comes from library_files we control; fpcalc binary is fixed
		"fpcalc",
		"-length", fmt.Sprint(chromaprintWindowSecs),
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

// KickoffMusicFingerprintWorker is the single-phase loudness-pump clone for
// chromaprints: snooze-loop sweeping ListTrackFilesPendingFingerprint with a
// cursor, one wave of scan_track_fingerprint jobs in flight at a time.
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
		rows, err := q.ListTrackFilesPendingFingerprint(ctx, sqlc.ListTrackFilesPendingFingerprintParams{
			AfterID:  st.TrackCursor,
			RowLimit: int32(want),
		})
		if err != nil {
			return pumpTransientFailure(ctx, w.DB, q, job.ID, taskID, st, job.CreatedAt, err)
		}
		tracksListed = len(rows)
		for _, row := range rows {
			if ctx.Err() != nil {
				return pumpInterrupted(ctx, w.DB, job.ID, taskID, st)
			}
			w.Progress.Set("scan_music_fingerprint", "kickoff_music_fingerprint", row.Path)
			res, err := rc.Insert(ctx, ScanTrackFingerprintArgs{TrackFileID: row.ID, ScheduledTaskID: taskID}, nil)
			switch {
			case err != nil:
				log.Warn().Err(err).Int64("track_file_id", row.ID).Msg("kickoff_music_fingerprint: enqueue failed")
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
