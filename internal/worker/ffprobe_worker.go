package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/scanner"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type Disposition struct {
	Default         int `json:"default"`
	Forced          int `json:"forced"`
	HearingImpaired int `json:"hearing_impaired"`
	VisualImpaired  int `json:"visual_impaired"`
	Comment         int `json:"comment"`
}

type MediaInfo struct {
	Format    FormatInfo   `json:"format"`
	Streams   []StreamInfo `json:"streams"`
	Duration  float64      `json:"duration"`
	Size      int64        `json:"size"`
	BitRate   int64        `json:"bit_rate"`
	Container string       `json:"container"`
}

type FormatInfo struct {
	Filename       string            `json:"filename"`
	FormatName     string            `json:"format_name"`
	FormatLongName string            `json:"format_long_name"`
	Duration       string            `json:"duration"`
	Size           string            `json:"size"`
	BitRate        string            `json:"bit_rate"`
	Tags           map[string]string `json:"tags"`
}

type StreamInfo struct {
	Index              int               `json:"index"`
	CodecName          string            `json:"codec_name"`
	CodecLongName      string            `json:"codec_long_name"`
	CodecType          string            `json:"codec_type"`
	CodecTagString     string            `json:"codec_tag_string,omitempty"` // hvc1 / hev1 / av01 / etc.
	Profile            string            `json:"profile,omitempty"`
	Width              int               `json:"width,omitempty"`
	Height             int               `json:"height,omitempty"`
	PixFmt             string            `json:"pix_fmt,omitempty"`
	BitsPerRawSample   string            `json:"bits_per_raw_sample,omitempty"` // ffprobe reports as string
	ColorRange         string            `json:"color_range,omitempty"`
	ColorSpace         string            `json:"color_space,omitempty"`
	ColorTransfer      string            `json:"color_transfer,omitempty"`
	ColorPrimaries     string            `json:"color_primaries,omitempty"`
	FieldOrder         string            `json:"field_order,omitempty"`
	SampleAspectRatio  string            `json:"sample_aspect_ratio,omitempty"`  // "1:1", "8:9"
	DisplayAspectRatio string            `json:"display_aspect_ratio,omitempty"` // "16:9"
	SampleRate         string            `json:"sample_rate,omitempty"`
	Channels           int               `json:"channels,omitempty"`
	ChannelLayout      string            `json:"channel_layout,omitempty"`
	BitRate            string            `json:"bit_rate,omitempty"`
	Duration           string            `json:"duration,omitempty"`
	Disposition        *Disposition      `json:"disposition,omitempty"`
	Tags               map[string]string `json:"tags"`
	SideDataList       []SideData        `json:"side_data_list,omitempty"`
}

// SideData captures ffprobe's "side_data_list" entries — Display Matrix
// (rotation), DOVI configuration record (Dolby Vision profile/level/BL compat),
// and mastering display metadata. Many fields are union-style: only the fields
// matching the type's "side_data_type" string are populated.
type SideData struct {
	Type                      string `json:"side_data_type"`
	Rotation                  int    `json:"rotation,omitempty"` // signed degrees (-90 = 90° CW)
	DvVersionMajor            int    `json:"dv_version_major,omitempty"`
	DvVersionMinor            int    `json:"dv_version_minor,omitempty"`
	DvProfile                 int    `json:"dv_profile,omitempty"`
	DvLevel                   int    `json:"dv_level,omitempty"`
	DvBlSignalCompatibilityID int    `json:"dv_bl_signal_compatibility_id,omitempty"`
	RpuPresentFlag            int    `json:"rpu_present_flag,omitempty"`
	ElPresentFlag             int    `json:"el_present_flag,omitempty"`
	BlPresentFlag             int    `json:"bl_present_flag,omitempty"`
}

type ffprobeOutput struct {
	Format  FormatInfo   `json:"format"`
	Streams []StreamInfo `json:"streams"`
}

// ParseFFProbeOutput parses raw ffprobe JSON into a MediaInfo struct.
// Filters attachment streams and populates numeric fields from format strings.
func ParseFFProbeOutput(data []byte) (*MediaInfo, error) {
	var probe ffprobeOutput
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parse ffprobe output: %w", err)
	}

	var filtered []StreamInfo
	for _, s := range probe.Streams {
		if s.CodecType == "attachment" {
			continue
		}
		filtered = append(filtered, s)
	}

	info := &MediaInfo{
		Format:    probe.Format,
		Streams:   filtered,
		Container: probe.Format.FormatName,
	}
	populateNumericFields(info)
	return info, nil
}

func populateNumericFields(info *MediaInfo) {
	if info.Format.Duration != "" {
		if v, err := strconv.ParseFloat(info.Format.Duration, 64); err == nil {
			info.Duration = v
		}
	}
	if info.Format.Size != "" {
		if v, err := strconv.ParseInt(info.Format.Size, 10, 64); err == nil {
			info.Size = v
		}
	}
	if info.Format.BitRate != "" {
		if v, err := strconv.ParseInt(info.Format.BitRate, 10, 64); err == nil {
			info.BitRate = v
		}
	}
}

type FFProbeWorker struct {
	river.WorkerDefaults[FFProbeArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *FFProbeWorker) Work(ctx context.Context, job *river.Job[FFProbeArgs]) error {
	w.Progress.SetCurrentByKind(FFProbeArgs{}.Kind(), filepath.Base(job.Args.FilePath))
	probeCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	var output []byte
	var err error

	if vfs.IsSMBPath(job.Args.FilePath) {
		output, err = ffprobeSMB(probeCtx, job.Args.FilePath)
	} else {
		output, err = ffprobeLocal(probeCtx, job.Args.FilePath)
	}
	if err != nil {
		log.Warn().Err(err).Str("path", job.Args.FilePath).Msg("ffprobe failed")
		return fmt.Errorf("ffprobe exec: %w", err)
	}

	info, err := ParseFFProbeOutput(output)
	if err != nil {
		log.Warn().Err(err).Str("path", job.Args.FilePath).Msg("ffprobe output parse error")
		return fmt.Errorf("ffprobe parse: %w", err)
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
		w.updateAudioTrackFile(ctx, q, job.Args.LibraryFileID, info, primaryAudio)
		w.enqueueLoudnessIfMusic(ctx, q, job.Args.LibraryFileID)
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

func ffprobeLocal(ctx context.Context, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
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
	cmd.Stdin = f.(io.Reader)
	return cmd.Output()
}

// updateAudioTrackFile writes probe results to the matching track_files row
// (if one exists — matcher creates it). Updates bitrate / sample_rate /
// bit_depth / channels / duration / size and recomputes quality_score using
// the real numbers (no longer extension-only).
func (w *FFProbeWorker) updateAudioTrackFile(ctx context.Context, q *sqlc.Queries, libraryFileID int64, info *MediaInfo, audio *StreamInfo) {
	tf, err := q.GetTrackFileByLibraryFileID(ctx, libraryFileID)
	if err != nil {
		// Matcher hasn't run yet; nothing to update. The matcher will fill
		// these fields itself when it reads media_info on track_files insert.
		log.Debug().Int64("library_file_id", libraryFileID).Msg("no track_files row yet for audio probe")
		return
	}

	bitrate := int32(parseFloatString(audio.BitRate) / 1000)
	if bitrate == 0 && info.Format.BitRate != "" {
		bitrate = int32(parseFloatString(info.Format.BitRate) / 1000)
	}
	sampleRate := int32(parseFloatString(audio.SampleRate))
	bitDepth := int32(parseIntString(audio.BitsPerRawSample))
	channels := int32(audio.Channels)
	duration := int32(info.Duration)
	if duration == 0 && audio.Duration != "" {
		duration = int32(parseFloatString(audio.Duration))
	}

	score := refinedQualityScore(tf.Format, bitrate, bitDepth, sampleRate)

	if err := q.UpdateTrackFileProbeData(ctx, sqlc.UpdateTrackFileProbeDataParams{
		ID:           tf.ID,
		BitrateKbps:  bitrate,
		SampleRateHz: sampleRate,
		BitDepth:     bitDepth,
		Channels:     channels,
		Duration:     duration,
		QualityScore: int32(score),
	}); err != nil {
		log.Warn().Err(err).Int64("track_file_id", tf.ID).Msg("update track_file probe data failed")
		return
	}

	log.Debug().
		Int64("track_file_id", tf.ID).
		Str("format", tf.Format).
		Int32("bitrate_kbps", bitrate).
		Int32("sample_rate", sampleRate).
		Int32("bit_depth", bitDepth).
		Int("score", score).
		Msg("audio probe written")
}

// refinedQualityScore is the v2 ranking that incorporates actual ffprobe
// data. Base by codec; lossless bumps for bit-depth (24-bit > 16-bit) and
// sample rate (96k > 48k > 44.1k); lossy bumps for bitrate.
func refinedQualityScore(format string, bitrateKbps, bitDepth, sampleRateHz int32) int {
	base := extensionQualityBase(format)
	switch format {
	case "flac", "alac", "wav":
		if bitDepth > 16 {
			base += int(bitDepth-16) * 30
		}
		if sampleRateHz > 48000 {
			base += int((sampleRateHz - 48000) / 1000)
		}
	default:
		// Lossy: bitrate is the dominant signal. 320kbps gets +120, 192 gets +72.
		base += int(bitrateKbps) * 4 / 10
	}
	return base
}

// extensionQualityBase mirrors matcher.audioQualityScore (which we can't
// import here without a cycle). Kept in sync by hand.
func extensionQualityBase(format string) int {
	switch strings.ToLower(strings.TrimPrefix(format, ".")) {
	case "flac":
		return 1000
	case "alac":
		return 950
	case "wav":
		return 900
	case "opus":
		return 500
	case "ogg":
		return 450
	case "aac":
		return 350
	case "m4a":
		return 300
	case "mp3":
		return 200
	}
	return 0
}

func parseFloatString(s string) float64 {
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func parseIntString(s string) int64 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// enqueueLoudnessIfMusic schedules an ebur128 pass for the file's track_files
// row when the file lives in a music library. Silently noops outside music
// libraries or when no track_files row exists yet (matcher hasn't run).
func (w *FFProbeWorker) enqueueLoudnessIfMusic(ctx context.Context, q *sqlc.Queries, libraryFileID int64) {
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
	if _, err := client.Insert(ctx, ScanTrackLoudnessArgs{TrackFileID: tf.ID}, nil); err != nil {
		log.Warn().Err(err).Int64("track_file_id", tf.ID).Msg("enqueue track loudness failed")
	}
}
