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
	"github.com/karbowiak/heya/internal/mediafile"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/metadata"
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

type FFProbeWorker struct {
	river.WorkerDefaults[FFProbeArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *FFProbeWorker) Work(ctx context.Context, job *river.Job[FFProbeArgs]) error {
	w.Progress.SetCurrent(FFProbeArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(job.Args.FilePath))
	source := scheduledJobSource(job.Metadata)
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
	contentHash := mediafile.ComputeContentHash(file.Size, infoJSON)
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
		w.enqueueLoudnessIfMusic(ctx, q, job.Args.LibraryFileID, job.Args.ScheduledTaskID, source)
	}

	if hasVideo {
		if !vfs.IsSMBPath(job.Args.FilePath) && !hasCurrentHLSBoundaryArtifact(file.Keyframes) {
			args := ScanKeyframesArgs{
				LibraryFileID:   job.Args.LibraryFileID,
				FilePath:        job.Args.FilePath,
				ScheduledTaskID: job.Args.ScheduledTaskID,
			}
			opts := args.InsertOpts()
			if _, err := river.ClientFromContext[pgx.Tx](ctx).Insert(ctx, args, applyScheduledJobSource(opts, source)); err != nil {
				log.Warn().Err(err).Int64("file_id", job.Args.LibraryFileID).Msg("ffprobe: enqueue keyframes failed")
			}
		}
		w.enqueuePostProbeVideoWork(ctx, q, file, job.Args.ScheduledTaskID, source)
	}

	return nil
}

func hasCurrentHLSBoundaryArtifact(raw []byte) bool {
	var kf transcoder.Keyframes
	return json.Unmarshal(raw, &kf) == nil && transcoder.HasExactHLSBoundaries(&kf)
}

type ScanKeyframesWorker struct {
	river.WorkerDefaults[ScanKeyframesArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *ScanKeyframesWorker) Work(ctx context.Context, job *river.Job[ScanKeyframesArgs]) error {
	w.Progress.SetCurrent(ScanKeyframesArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(job.Args.FilePath))
	_, err := AnalyzeAndPersistKeyframes(ctx, w.DB, job.Args.LibraryFileID, job.Args.FilePath)
	return err
}

// AnalyzeAndPersistKeyframes is shared by the scan_keyframes worker and the
// on-demand playback backstop. It owns the complete artifact: ordinary video
// keyframes plus the exact cuts selected by ffmpeg's HLS muxer.
func AnalyzeAndPersistKeyframes(ctx context.Context, db *pgxpool.Pool, libraryFileID int64, filePath string) (*transcoder.Keyframes, error) {
	kf, err := transcoder.ExtractKeyframes(ctx, filePath)
	if err != nil {
		log.Warn().Err(err).Str("path", vfs.RedactPath(filePath)).Msg("keyframe extraction failed")
		return nil, fmt.Errorf("keyframes: %w", err)
	}
	if kf == nil || len(kf.IFrames) == 0 {
		return kf, nil
	}
	// Persist muxer-exact HLS boundaries during background analysis so
	// playback never has to demux the entire file synchronously. This is
	// best-effort: keyframes remain useful fallback data when the extra pass
	// times out or the source cannot be opened by ffmpeg.
	if ends, boundaryErr := transcoder.RealSegmentBoundaries(ctx, filePath, transcoder.SegmentDuration); boundaryErr != nil {
		log.Warn().Err(boundaryErr).
			Str("path", vfs.RedactPath(filePath)).
			Msg("exact HLS boundary analysis failed; persisting keyframes only")
	} else {
		kf.HLSBoundaryVersion = transcoder.HLSBoundaryVersion
		kf.HLSSegmentDuration = transcoder.SegmentDuration
		kf.HLSSegmentEnds = ends
	}
	kfJSON, err := json.Marshal(kf)
	if err != nil {
		return nil, fmt.Errorf("keyframes marshal: %w", err)
	}
	q := sqlc.New(db)
	if err := q.UpdateLibraryFileKeyframes(ctx, sqlc.UpdateLibraryFileKeyframesParams{
		ID:        libraryFileID,
		Keyframes: kfJSON,
	}); err != nil {
		return nil, fmt.Errorf("keyframes persistence: %w", err)
	}
	log.Debug().
		Int64("file_id", libraryFileID).
		Int("keyframes", len(kf.IFrames)).
		Int("hls_boundaries", len(kf.HLSSegmentEnds)).
		Float64("duration", kf.Duration).
		Msg("keyframes extracted")
	return kf, nil
}

func (w *FFProbeWorker) enqueuePostProbeVideoWork(ctx context.Context, q *sqlc.Queries, file sqlc.LibraryFile, scheduledTaskID string, source string) {
	rc := river.ClientFromContext[pgx.Tx](ctx)
	if rc == nil {
		return
	}
	lib, err := q.GetLibraryByID(ctx, file.LibraryID)
	if err != nil {
		log.Warn().Err(err).Int64("library_id", file.LibraryID).Msg("ffprobe: library lookup for post-probe fanout failed")
		return
	}
	settings := metadata.ParseSettings(lib.Settings)
	links, err := q.ListLibraryFileLinksByFile(ctx, file.ID)
	if err != nil {
		log.Warn().Err(err).Int64("file_id", file.ID).Msg("ffprobe: file link lookup for post-probe fanout failed")
		return
	}

	if settings.EnableTrickplay && !file.HasTrickplay && !vfs.IsSMBPath(file.Path) {
		if _, err := rc.Insert(ctx, TrickplayFileArgs{LibraryFileID: file.ID, ScheduledTaskID: scheduledTaskID}, scheduledJobInsertOpts(source)); err != nil {
			log.Warn().Err(err).Int64("file_id", file.ID).Msg("ffprobe: enqueue trickplay failed")
		}
	}
	if scannerMediaTypeScansSegments(lib.MediaType) && !file.SegmentsAnalyzedAt.Valid && libraryFileHasPrimaryLink(links) {
		if _, err := rc.Insert(ctx, ScanMediaSegmentsFileArgs{LibraryFileID: file.ID, ScheduledTaskID: scheduledTaskID}, scheduledJobInsertOpts(source)); err != nil {
			log.Warn().Err(err).Int64("file_id", file.ID).Msg("ffprobe: enqueue media segments failed")
		}
	}
	if !vfs.IsSMBPath(file.Path) {
		for _, link := range links {
			if link.RelationType == "extra" && link.ThumbnailPath == "" {
				if _, err := rc.Insert(ctx, ThumbnailExtraArgs{ExtraID: link.ID, ScheduledTaskID: scheduledTaskID}, scheduledJobInsertOpts(source)); err != nil {
					log.Warn().Err(err).Int64("extra_id", link.ID).Msg("ffprobe: enqueue extra thumbnail failed")
				}
			}
		}
	}
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
func (w *FFProbeWorker) enqueueLoudnessIfMusic(ctx context.Context, q *sqlc.Queries, libraryFileID int64, scheduledTaskID string, source string) {
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
	if _, _, err := enqueueTrackLoudnessIfNeeded(ctx, q, client, ScanTrackLoudnessArgs{TrackFileID: tf.ID, ScheduledTaskID: scheduledTaskID}, scheduledJobInsertOpts(source)); err != nil {
		log.Warn().Err(err).Int64("track_file_id", tf.ID).Msg("enqueue track loudness failed")
	}
}
