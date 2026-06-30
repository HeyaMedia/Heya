package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/scanner"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type Disposition = mediaprobe.Disposition
type MediaInfo = mediaprobe.MediaInfo
type FormatInfo = mediaprobe.FormatInfo
type StreamInfo = mediaprobe.StreamInfo
type SideData = mediaprobe.SideData

// ParseFFProbeOutput parses raw ffprobe JSON into a MediaInfo struct.
// Filters attachment streams and populates numeric fields from format strings.
func ParseFFProbeOutput(data []byte) (*MediaInfo, error) {
	return mediaprobe.Parse(data)
}

func populateNumericFields(info *MediaInfo) {
	mediaprobe.PopulateNumericFields(info)
}

type FFProbeWorker struct {
	river.WorkerDefaults[FFProbeArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *FFProbeWorker) Work(ctx context.Context, job *river.Job[FFProbeArgs]) error {
	w.Progress.SetCurrent(FFProbeArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(job.Args.FilePath))
	probeCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	info, err := ProbeFile(probeCtx, job.Args.FilePath)
	if err != nil {
		log.Warn().Err(err).Str("path", vfs.RedactPath(job.Args.FilePath)).Msg("ffprobe failed")
		return fmt.Errorf("ffprobe: %w", err)
	}

	infoJSON, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("ffprobe marshal: %w", err)
	}

	q := sqlc.New(w.DB)
	if err := q.UpdateLibraryFileMediaInfo(ctx, sqlc.UpdateLibraryFileMediaInfoParams{
		ID:        job.Args.LibraryFileID,
		MediaInfo: infoJSON,
	}); err != nil {
		return fmt.Errorf("ffprobe db write: %w", err)
	}

	file, _ := q.GetLibraryFileByID(ctx, job.Args.LibraryFileID)
	contentHash := scanner.ComputeContentHash(file.Size, infoJSON)
	if contentHash != "" {
		q.UpdateLibraryFileContentHash(ctx, sqlc.UpdateLibraryFileContentHashParams{
			ID:          job.Args.LibraryFileID,
			ContentHash: contentHash,
		})
	}

	log.Debug().
		Int64("file_id", job.Args.LibraryFileID).
		Str("container", info.Container).
		Int("streams", len(info.Streams)).
		Float64("duration", info.Duration).
		Msg("ffprobe complete")

	hasVideo := false
	hasAudio := false
	var primaryAudio *StreamInfo
	for i := range info.Streams {
		s := &info.Streams[i]
		switch s.CodecType {
		case "video":
			hasVideo = true
		case "audio":
			hasAudio = true
			if primaryAudio == nil {
				primaryAudio = s
			}
		}
	}

	if hasAudio && primaryAudio != nil {
		UpdateAudioTrackFileFromProbe(ctx, q, job.Args.LibraryFileID, info, primaryAudio)
		w.enqueueLoudnessIfMusic(ctx, q, job.Args.LibraryFileID, job.Args.ScheduledTaskID)
	}

	if hasVideo && !vfs.IsSMBPath(job.Args.FilePath) {
		kf, err := transcoder.ExtractKeyframes(ctx, job.Args.FilePath)
		if err != nil {
			log.Warn().Err(err).Int64("file_id", job.Args.LibraryFileID).Msg("keyframe extraction failed")
		} else if kf != nil && len(kf.IFrames) > 0 {
			kfJSON, err := json.Marshal(kf)
			if err == nil {
				q.UpdateLibraryFileKeyframes(ctx, sqlc.UpdateLibraryFileKeyframesParams{
					ID:        job.Args.LibraryFileID,
					Keyframes: kfJSON,
				})
				log.Debug().
					Int64("file_id", job.Args.LibraryFileID).
					Int("keyframes", len(kf.IFrames)).
					Float64("duration", kf.Duration).
					Msg("keyframes extracted")
			}
		}

	}

	return nil
}

// ProbeFile runs ffprobe against a local or SMB path and returns parsed
// MediaInfo. It is the shared entry point for both the async FFProbeWorker and
// the on-demand service probe (App.EnsureFileProbed), so the two can never
// drift on ffprobe flags or SMB pipe handling.
func ProbeFile(ctx context.Context, path string) (*MediaInfo, error) {
	var output []byte
	var err error
	if vfs.IsSMBPath(path) {
		output, err = ffprobeSMB(ctx, path)
	} else {
		output, err = ffprobeLocal(ctx, path)
	}
	if err != nil {
		return nil, fmt.Errorf("ffprobe exec: %w", err)
	}
	info, err := ParseFFProbeOutput(output)
	if err != nil {
		return nil, fmt.Errorf("ffprobe parse: %w", err)
	}
	return info, nil
}

func ffprobeLocal(ctx context.Context, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"-i", path,
	)
	return cmd.Output()
}

func ffprobeSMB(ctx context.Context, smbPath string) ([]byte, error) {
	lastSlash := strings.LastIndex(smbPath, "/")
	if lastSlash < 0 {
		return nil, fmt.Errorf("invalid smb path: %s", smbPath)
	}
	dirPath := smbPath[:lastSlash]
	fileName := smbPath[lastSlash+1:]

	source, err := vfs.Open(dirPath)
	if err != nil {
		return nil, fmt.Errorf("open smb dir: %w", err)
	}
	defer source.Close()

	f, err := source.FS.Open(fileName)
	if err != nil {
		return nil, fmt.Errorf("open smb file %q: %w", fileName, err)
	}
	defer f.Close()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"-i", "pipe:0",
	)
	reader, ok := f.(io.Reader)
	if !ok {
		return nil, fmt.Errorf("smb file does not implement io.Reader: %s", smbPath)
	}
	cmd.Stdin = reader
	return cmd.Output()
}

// UpdateAudioTrackFileFromProbe writes probe results to the matching
// track_files row (if one exists — matcher creates it). Updates bitrate /
// sample_rate / bit_depth / channels / duration / size and recomputes
// quality_score using the real numbers (no longer extension-only). Shared by
// FFProbeWorker and the on-demand service probe.
func UpdateAudioTrackFileFromProbe(ctx context.Context, q *sqlc.Queries, libraryFileID int64, info *MediaInfo, audio *StreamInfo) {
	tf, err := q.GetTrackFileByLibraryFileID(ctx, libraryFileID)
	if err != nil {
		// Matcher hasn't run yet; nothing to update. The matcher will fill
		// these fields itself when it reads media_info on track_files insert.
		log.Debug().Int64("library_file_id", libraryFileID).Msg("no track_files row yet for audio probe")
		return
	}

	fields := mediaprobe.AudioFieldsFrom(info, audio)
	score := mediaprobe.RefinedQualityScore(tf.Format, fields.BitrateKbps, fields.BitDepth, fields.SampleRateHz)

	if err := q.UpdateTrackFileProbeData(ctx, sqlc.UpdateTrackFileProbeDataParams{
		ID:           tf.ID,
		BitrateKbps:  fields.BitrateKbps,
		SampleRateHz: fields.SampleRateHz,
		BitDepth:     fields.BitDepth,
		Channels:     fields.Channels,
		Duration:     fields.Duration,
		QualityScore: int32(score),
	}); err != nil {
		log.Warn().Err(err).Int64("track_file_id", tf.ID).Msg("update track_file probe data failed")
		return
	}

	log.Debug().
		Int64("track_file_id", tf.ID).
		Str("format", tf.Format).
		Int32("bitrate_kbps", fields.BitrateKbps).
		Int32("sample_rate", fields.SampleRateHz).
		Int32("bit_depth", fields.BitDepth).
		Int("score", score).
		Msg("audio probe written")
}

// enqueueLoudnessIfMusic schedules an ebur128 pass for the file's track_files
// row when the file lives in a music library. Silently noops outside music
// libraries or when no track_files row exists yet (matcher hasn't run).
func (w *FFProbeWorker) enqueueLoudnessIfMusic(ctx context.Context, q *sqlc.Queries, libraryFileID int64, scheduledTaskID string) {
	tf, err := q.GetTrackFileByLibraryFileID(ctx, libraryFileID)
	if err != nil {
		// Matcher hasn't created the track_files row yet. It'll re-trigger
		// loudness when the row eventually appears via the scheduled
		// backstop. Don't block the probe pipeline on it.
		return
	}

	lf, err := q.GetLibraryFileByID(ctx, libraryFileID)
	if err != nil {
		return
	}
	lib, err := q.GetLibraryByID(ctx, lf.LibraryID)
	if err != nil || lib.MediaType != sqlc.MediaTypeMusic {
		return
	}

	client := river.ClientFromContext[pgx.Tx](ctx)
	if client == nil {
		return
	}
	if _, err := client.Insert(ctx, ScanTrackLoudnessArgs{TrackFileID: tf.ID, ScheduledTaskID: scheduledTaskID}, nil); err != nil {
		log.Warn().Err(err).Int64("track_file_id", tf.ID).Msg("enqueue track loudness failed")
	}
}
