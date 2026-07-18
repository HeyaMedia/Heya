package mediaanalysis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
)

// Service owns reusable media analysis for one process. Shared work is rooted
// in the runtime context instead of the first HTTP/job caller, so one caller
// leaving does not cancel analysis that other waiters still need. Every caller
// can nevertheless stop waiting through its own context.
type Service struct {
	ctx    context.Context
	cancel context.CancelFunc
	db     *pgxpool.Pool

	mu        sync.Mutex
	closed    bool
	wg        sync.WaitGroup
	closeOnce sync.Once

	loudness   singleflight.Group
	boundaries singleflight.Group
	keyframes  singleflight.Group
}

func New(ctx context.Context, db *pgxpool.Pool) *Service {
	if ctx == nil {
		ctx = context.Background()
	}
	serviceCtx, cancel := context.WithCancel(ctx)
	return &Service{ctx: serviceCtx, cancel: cancel, db: db}
}

// ErrServiceClosed is returned when new analysis is requested after shutdown
// has begun.
var ErrServiceClosed = errors.New("media analysis service is closed")

// run admits one caller before scheduling shared work. If the caller stops
// waiting, its completion token is retained by a tiny waiter until the shared
// result arrives; Close can therefore join the actual singleflight work rather
// than merely the HTTP/River caller that happened to start it.
func (s *Service) run(ctx context.Context, group *singleflight.Group, key string, work func() (any, error)) (any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrServiceClosed
	}
	s.wg.Add(1)
	s.mu.Unlock()

	result := group.DoChan(key, work)
	select {
	case value := <-result:
		s.wg.Done()
		return value.Val, value.Err
	case <-ctx.Done():
		go func() {
			<-result
			s.wg.Done()
		}()
		return nil, ctx.Err()
	}
}

// Close is terminal: it rejects new analysis, cancels runtime-owned work, and
// waits for every admitted singleflight result before App closes the database.
func (s *Service) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.cancel()
		s.mu.Unlock()
		s.wg.Wait()
	})
}

// EnsureTrackLoudness computes and persists ReplayGain data once per process.
func (s *Service) EnsureTrackLoudness(ctx context.Context, trackFileID int64) error {
	key := strconv.FormatInt(trackFileID, 10)
	_, err := s.run(ctx, &s.loudness, key, func() (any, error) {
		workCtx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
		defer cancel()

		q := sqlc.New(s.db)
		tf, err := q.GetTrackFileByID(workCtx, trackFileID)
		if err != nil {
			return nil, err
		}
		if tf.IntegratedLufs.Valid && tf.TruePeakDb.Valid {
			return nil, nil
		}
		file, err := q.GetLibraryFileByID(workCtx, tf.LibraryFileID)
		if err != nil || file.DeletedAt.Valid {
			return nil, err
		}
		result, err := AnalyzeLoudness(workCtx, file.Path)
		if err != nil {
			return nil, fmt.Errorf("analyze track loudness: %w", err)
		}
		return nil, q.UpdateTrackFileLoudness(workCtx, sqlc.UpdateTrackFileLoudnessParams{
			ID:              tf.ID,
			IntegratedLufs:  numeric(result.IntegratedLUFS),
			TruePeakDb:      numeric(result.TruePeakDB),
			LoudnessRangeDb: numeric(result.LoudnessRangeDB),
			SamplePeakDb:    numeric(result.SamplePeakDB),
		})
	})
	return err
}

// EnsureTrackBoundaries computes the smart-crossfade envelope once per
// process. A completed attempt is stamped even when decoding finds no useful
// boundaries, preserving the existing scheduled-backfill semantics.
func (s *Service) EnsureTrackBoundaries(ctx context.Context, trackFileID int64) error {
	key := strconv.FormatInt(trackFileID, 10)
	_, err := s.run(ctx, &s.boundaries, key, func() (any, error) {
		workCtx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
		defer cancel()

		q := sqlc.New(s.db)
		tf, err := q.GetTrackFileByID(workCtx, trackFileID)
		if err != nil || tf.BoundariesAnalyzedAt.Valid {
			return nil, err
		}
		file, err := q.GetLibraryFileByID(workCtx, tf.LibraryFileID)
		if err != nil || file.DeletedAt.Valid {
			return nil, err
		}

		params := sqlc.UpdateTrackFileBoundariesParams{ID: tf.ID}
		if boundaries, detectErr := sonicanalysis.DetectBoundaries(workCtx, file.Path); detectErr != nil {
			log.Debug().Err(vfs.RedactError(detectErr)).Str("path", vfs.RedactPath(file.Path)).Msg("boundary detection failed; marking analyzed with no boundaries")
		} else if boundaries != nil {
			params.IntroEndMs = nullableInt4(boundaries.IntroEndMs)
			params.OutroStartMs = nullableInt4(boundaries.OutroStartMs)
			params.FadeStartMs = nullableInt4(boundaries.FadeStartMs)
			params.SilenceStartMs = nullableInt4(boundaries.SilenceStartMs)
		}
		return nil, q.UpdateTrackFileBoundaries(workCtx, params)
	})
	return err
}

// AnalyzeAndPersistKeyframes owns the complete playback artifact: ordinary
// keyframes plus ffmpeg's exact HLS muxer boundaries. Concurrent on-demand and
// worker calls in this process share one pass.
func (s *Service) AnalyzeAndPersistKeyframes(ctx context.Context, libraryFileID int64) (*transcoder.Keyframes, error) {
	key := strconv.FormatInt(libraryFileID, 10)
	value, err := s.run(ctx, &s.keyframes, key, func() (any, error) {
		workCtx, cancel := context.WithTimeout(s.ctx, 3*time.Minute)
		defer cancel()
		q := sqlc.New(s.db)
		file, lookupErr := q.GetLibraryFileByID(workCtx, libraryFileID)
		if lookupErr != nil {
			return nil, fmt.Errorf("keyframes file lookup: %w", lookupErr)
		}
		if file.DeletedAt.Valid {
			return nil, nil
		}
		var existing transcoder.Keyframes
		if json.Unmarshal(file.Keyframes, &existing) == nil && transcoder.HasExactHLSBoundaries(&existing) {
			return &existing, nil
		}
		filePath := file.Path

		keyframes, err := transcoder.ExtractKeyframes(workCtx, filePath)
		if err != nil {
			log.Warn().Err(vfs.RedactError(err)).Str("path", vfs.RedactPath(filePath)).Msg("keyframe extraction failed")
			return nil, fmt.Errorf("keyframes: %w", err)
		}
		if keyframes == nil || len(keyframes.IFrames) == 0 {
			return keyframes, nil
		}
		if ends, boundaryErr := transcoder.RealSegmentBoundaries(workCtx, filePath, transcoder.SegmentDuration); boundaryErr != nil {
			log.Warn().Err(vfs.RedactError(boundaryErr)).Str("path", vfs.RedactPath(filePath)).Msg("exact HLS boundary analysis failed; persisting keyframes only")
		} else {
			keyframes.HLSBoundaryVersion = transcoder.HLSBoundaryVersion
			keyframes.HLSSegmentDuration = transcoder.SegmentDuration
			keyframes.HLSSegmentEnds = ends
		}
		buf, err := json.Marshal(keyframes)
		if err != nil {
			return nil, fmt.Errorf("keyframes marshal: %w", err)
		}
		if err := q.UpdateLibraryFileKeyframes(workCtx, sqlc.UpdateLibraryFileKeyframesParams{ID: libraryFileID, Keyframes: buf}); err != nil {
			return nil, fmt.Errorf("keyframes persistence: %w", err)
		}
		log.Debug().Int64("file_id", libraryFileID).Int("keyframes", len(keyframes.IFrames)).Int("hls_boundaries", len(keyframes.HLSSegmentEnds)).Float64("duration", keyframes.Duration).Msg("keyframes extracted")
		return keyframes, nil
	})
	if err != nil || value == nil {
		return nil, err
	}
	return value.(*transcoder.Keyframes), nil
}

// UpdateAudioTrackFileFromProbe refreshes the matching track_files row when
// one already exists. Matcher remains responsible for the initial row.
func (s *Service) UpdateAudioTrackFileFromProbe(ctx context.Context, libraryFileID int64, info *mediaprobe.MediaInfo, audio *mediaprobe.StreamInfo) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	workCtx, cancel := context.WithCancel(ctx)
	stopServiceCancel := context.AfterFunc(s.ctx, cancel)
	defer func() {
		stopServiceCancel()
		cancel()
	}()

	q := sqlc.New(s.db)
	tf, err := q.GetTrackFileByLibraryFileID(workCtx, libraryFileID)
	if err != nil {
		log.Debug().Int64("library_file_id", libraryFileID).Msg("no track_files row yet for audio probe")
		return
	}
	fields := mediaprobe.AudioFieldsFrom(info, audio)
	score := mediaprobe.RefinedQualityScore(tf.Format, fields.BitrateKbps, fields.BitDepth, fields.SampleRateHz)
	if err := q.UpdateTrackFileProbeData(workCtx, sqlc.UpdateTrackFileProbeDataParams{
		ID: tf.ID, BitrateKbps: fields.BitrateKbps, SampleRateHz: fields.SampleRateHz,
		BitDepth: fields.BitDepth, Channels: fields.Channels, Duration: fields.Duration,
		QualityScore: int32(score),
	}); err != nil {
		log.Warn().Err(err).Int64("track_file_id", tf.ID).Msg("update track_file probe data failed")
		return
	}
	log.Debug().Int64("track_file_id", tf.ID).Str("format", tf.Format).Int32("bitrate_kbps", fields.BitrateKbps).Int32("sample_rate", fields.SampleRateHz).Int32("bit_depth", fields.BitDepth).Int("score", score).Msg("audio probe written")
}

type Loudness struct {
	IntegratedLUFS  float64
	TruePeakDB      float64
	LoudnessRangeDB float64
	SamplePeakDB    float64
}

// AnalyzeLoudness invokes ffmpeg's ebur128 filter on one filesystem path and
// parses the summary emitted on stderr.
func AnalyzeLoudness(ctx context.Context, path string) (Loudness, error) {
	if err := vfs.ValidateLocalPath(path); err != nil {
		return Loudness{}, fmt.Errorf("audio input: %w", err)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", //nolint:gosec // library path; executable is fixed
		"-nostdin", "-nostats", "-hide_banner",
		"-i", path,
		"-vn", "-sn", "-dn", "-map", "0:a:0",
		"-af", "ebur128=peak=true",
		"-f", "null", "-",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Loudness{}, fmt.Errorf("ffmpeg ebur128: %w (stderr: %s)", err, tail(stderr.String(), 300))
	}
	return ParseEBUR128(stderr.String())
}

var (
	reIntegrated = regexp.MustCompile(`(?m)^\s*I:\s*(-?\d+(?:\.\d+)?)\s*LUFS`)
	reLRA        = regexp.MustCompile(`(?m)^\s*LRA:\s*(-?\d+(?:\.\d+)?)\s*LU`)
	reSamplePeak = regexp.MustCompile(`(?s)Sample peak:.*?Peak:\s*(-?\d+(?:\.\d+)?)\s*dBFS`)
	reTruePeak   = regexp.MustCompile(`(?s)True peak:.*?Peak:\s*(-?\d+(?:\.\d+)?)\s*dBFS`)
)

func ParseEBUR128(output string) (Loudness, error) {
	if idx := strings.Index(output, "Summary:"); idx >= 0 {
		output = output[idx:]
	}
	result := Loudness{}
	found := false
	if match := reIntegrated.FindStringSubmatch(output); len(match) == 2 {
		result.IntegratedLUFS, _ = strconv.ParseFloat(match[1], 64)
		found = true
	}
	if match := reLRA.FindStringSubmatch(output); len(match) == 2 {
		result.LoudnessRangeDB, _ = strconv.ParseFloat(match[1], 64)
	}
	if match := reTruePeak.FindStringSubmatch(output); len(match) == 2 {
		result.TruePeakDB, _ = strconv.ParseFloat(match[1], 64)
	}
	if match := reSamplePeak.FindStringSubmatch(output); len(match) == 2 {
		result.SamplePeakDB, _ = strconv.ParseFloat(match[1], 64)
	}
	if !found {
		return result, errors.New("ebur128 summary not found in output")
	}
	return result, nil
}

func numeric(value float64) pgtype.Numeric {
	var result pgtype.Numeric
	_ = result.Scan(strconv.FormatFloat(value, 'f', 2, 64))
	return result
}

func nullableInt4(value int) pgtype.Int4 {
	return pgtype.Int4{Int32: int32(value), Valid: true}
}

func tail(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[len(value)-max:]
}
