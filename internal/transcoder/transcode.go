package transcoder

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

func IsFFmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

func IsFFprobeAvailable() bool {
	_, err := exec.LookPath("ffprobe")
	return err == nil
}

func TranscodeToHLS(ctx context.Context, input, outputDir string, profile Profile) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	args := []string{
		"-i", input,
		"-f", "hls",
		"-hls_time", "6",
		"-hls_segment_type", "fmp4",
		"-hls_playlist_type", "event",
		"-hls_segment_filename", outputDir + "/segment_%04d.m4s",
	}

	if profile.VideoCodec != "" && profile.VideoCodec != "copy" {
		args = append(args, "-c:v", profile.VideoCodec)
		if profile.CRF > 0 {
			args = append(args, "-crf", strconv.Itoa(profile.CRF))
		}
		if profile.MaxBitrate != "" {
			args = append(args, "-maxrate", profile.MaxBitrate, "-bufsize", profile.MaxBitrate)
		}
		if profile.Preset != "" {
			args = append(args, "-preset", profile.Preset)
		}
		if profile.MaxHeight > 0 {
			args = append(args, "-vf", fmt.Sprintf("scale=-2:'min(%d,ih)'", profile.MaxHeight))
		}
	} else {
		args = append(args, "-c:v", "copy")
	}

	if profile.AudioCodec != "" && profile.AudioCodec != "copy" {
		args = append(args, "-c:a", profile.AudioCodec)
	} else {
		args = append(args, "-c:a", "copy")
	}

	args = append(args, outputDir+"/index.m3u8")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	return cmd.Run()
}

func RemuxToMP4(ctx context.Context, input, output string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", input,
		"-c", "copy",
		"-movflags", "+faststart",
		output,
	)
	return cmd.Run()
}

func ExtractSubtitles(ctx context.Context, input string, streamIndex int, output string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", input,
		"-map", fmt.Sprintf("0:%d", streamIndex),
		"-c:s", "webvtt",
		output,
	)
	return cmd.Run()
}
