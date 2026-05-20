package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
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
	Index          int               `json:"index"`
	CodecName      string            `json:"codec_name"`
	CodecLongName  string            `json:"codec_long_name"`
	CodecType      string            `json:"codec_type"`
	Profile        string            `json:"profile,omitempty"`
	Width          int               `json:"width,omitempty"`
	Height         int               `json:"height,omitempty"`
	PixFmt         string            `json:"pix_fmt,omitempty"`
	ColorRange     string            `json:"color_range,omitempty"`
	ColorSpace     string            `json:"color_space,omitempty"`
	ColorTransfer  string            `json:"color_transfer,omitempty"`
	ColorPrimaries string            `json:"color_primaries,omitempty"`
	FieldOrder     string            `json:"field_order,omitempty"`
	SampleRate     string            `json:"sample_rate,omitempty"`
	Channels       int               `json:"channels,omitempty"`
	ChannelLayout  string            `json:"channel_layout,omitempty"`
	BitRate        string            `json:"bit_rate,omitempty"`
	Duration       string            `json:"duration,omitempty"`
	Disposition    *Disposition      `json:"disposition,omitempty"`
	Tags           map[string]string `json:"tags"`
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
	DB *pgxpool.Pool
}

func (w *FFProbeWorker) Work(ctx context.Context, job *river.Job[FFProbeArgs]) error {
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

	log.Debug().
		Int64("file_id", job.Args.LibraryFileID).
		Str("container", info.Container).
		Int("streams", len(info.Streams)).
		Float64("duration", info.Duration).
		Msg("ffprobe complete")

	hasVideo := false
	for _, s := range info.Streams {
		if s.CodecType == "video" {
			hasVideo = true
			break
		}
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
