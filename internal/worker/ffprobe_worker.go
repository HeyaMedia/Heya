package worker

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

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
	Index         int               `json:"index"`
	CodecName     string            `json:"codec_name"`
	CodecLongName string            `json:"codec_long_name"`
	CodecType     string            `json:"codec_type"`
	Width         int               `json:"width,omitempty"`
	Height        int               `json:"height,omitempty"`
	SampleRate    string            `json:"sample_rate,omitempty"`
	Channels      int               `json:"channels,omitempty"`
	ChannelLayout string            `json:"channel_layout,omitempty"`
	BitRate       string            `json:"bit_rate,omitempty"`
	Duration      string            `json:"duration,omitempty"`
	Tags          map[string]string `json:"tags"`
}

type ffprobeOutput struct {
	Format  FormatInfo   `json:"format"`
	Streams []StreamInfo `json:"streams"`
}

type FFProbeWorker struct {
	river.WorkerDefaults[FFProbeArgs]
	DB *pgxpool.Pool
}

func (w *FFProbeWorker) Work(ctx context.Context, job *river.Job[FFProbeArgs]) error {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		job.Args.FilePath,
	)

	output, err := cmd.Output()
	if err != nil {
		log.Debug().Err(err).Str("path", job.Args.FilePath).Msg("ffprobe failed (may not be installed)")
		return nil
	}

	var probe ffprobeOutput
	if err := json.Unmarshal(output, &probe); err != nil {
		log.Warn().Err(err).Msg("ffprobe output parse error")
		return nil
	}

	info := MediaInfo{
		Format:    probe.Format,
		Streams:   probe.Streams,
		Container: probe.Format.FormatName,
	}

	infoJSON, _ := json.Marshal(info)

	q := sqlc.New(w.DB)
	q.UpdateLibraryFileMediaInfo(ctx, sqlc.UpdateLibraryFileMediaInfoParams{
		ID:        job.Args.LibraryFileID,
		MediaInfo: infoJSON,
	})

	log.Debug().Int64("file_id", job.Args.LibraryFileID).Str("container", info.Container).Int("streams", len(info.Streams)).Msg("ffprobe complete")
	return nil
}
