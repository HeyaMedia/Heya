package transcoder

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

type TranscodeOpts struct {
	Input          string
	InputReader    io.Reader
	OutputDir      string
	Profile        Profile
	HWAccel        HwAccelConfig
	Keyframes      *Keyframes
	StartTime      float64
	Duration       float64
	AudioTrack     int
	SubtitleTrack  int
	BurnSubtitles  bool
	SubtitleCodec  string
}

func TranscodeToHLSWithOpts(ctx context.Context, opts TranscodeOpts) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	args := buildTranscodeArgs(opts)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if opts.InputReader != nil {
		cmd.Stdin = opts.InputReader
	}
	return cmd.Run()
}

func buildTranscodeArgs(opts TranscodeOpts) []string {
	var args []string

	args = append(args, opts.HWAccel.InputFlags...)

	if opts.StartTime > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", opts.StartTime))
	}

	if opts.InputReader != nil {
		args = append(args, "-i", "pipe:0")
	} else {
		args = append(args, "-i", opts.Input)
	}

	args = appendVideoArgs(args, opts)
	args = appendAudioArgs(args, opts.Profile)
	args = appendOutputArgs(args, opts)

	return args
}

func appendVideoArgs(args []string, opts TranscodeOpts) []string {
	p := opts.Profile
	hw := opts.HWAccel

	if p.VideoCodec == "" || p.VideoCodec == "copy" {
		return append(args, "-c:v", "copy")
	}

	encoder := resolveVideoEncoder(p.VideoCodec, hw)
	args = append(args, "-c:v", encoder)

	if p.CRF > 0 && hw.Type == HwAccelNone {
		args = append(args, "-crf", strconv.Itoa(p.CRF))
	}

	if hw.Type == HwAccelVideoToolbox {
		args = append(args, "-q:v", "65")
	}

	if hw.Type == HwAccelNVENC {
		args = append(args, "-rc", "vbr", "-cq", strconv.Itoa(p.CRF))
	}

	if hw.Type == HwAccelQSV {
		args = append(args, "-global_quality", strconv.Itoa(p.CRF))
	}

	if p.MaxBitrate != "" {
		args = append(args, "-maxrate", p.MaxBitrate, "-bufsize", p.MaxBitrate)
	}

	if p.Preset != "" && hw.Type == HwAccelNone {
		args = append(args, "-preset", p.Preset)
	}

	if p.MaxHeight > 0 {
		args = appendScaleFilter(args, opts)
	}

	if hw.Type == HwAccelNVENC {
		args = append(args,
			"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", 4),
		)
	}

	if hw.Type == HwAccelQSV && strings.Contains(encoder, "hevc") {
		args = append(args, "-load_plugin", "hevc_hw")
	}

	return args
}

func resolveVideoEncoder(profileCodec string, hw HwAccelConfig) string {
	if profileCodec == "libx264" || profileCodec == "h264" {
		return hw.EncoderH264
	}
	if profileCodec == "libx265" || profileCodec == "hevc" {
		return hw.EncoderHEVC
	}
	return profileCodec
}

func appendScaleFilter(args []string, opts TranscodeOpts) []string {
	h := opts.Profile.MaxHeight
	hw := opts.HWAccel

	var filter string
	switch hw.Type {
	case HwAccelNVENC:
		filter = fmt.Sprintf("scale_cuda=-2:min'(%d,ih)'", h)
	case HwAccelVAAPI:
		filter = fmt.Sprintf("scale_vaapi=w=-2:h=min(%d\\,ih)", h)
	case HwAccelQSV:
		filter = fmt.Sprintf("scale_qsv=w=-2:h=min(%d\\,ih)", h)
	case HwAccelVideoToolbox:
		filter = fmt.Sprintf("scale=-2:min(%d\\,ih),format=nv12", h)
	default:
		filter = fmt.Sprintf("scale=-2:'min(%d,ih)'", h)
	}

	return append(args, "-vf", filter)
}

func appendAudioArgs(args []string, p Profile) []string {
	if p.AudioCodec != "" && p.AudioCodec != "copy" {
		args = append(args, "-c:a", p.AudioCodec)
		if p.Name == "audio" {
			args = append(args, "-b:a", "320k")
		} else {
			args = append(args, "-b:a", "192k")
		}
	} else {
		args = append(args, "-c:a", "copy")
	}
	return args
}

func appendOutputArgs(args []string, opts TranscodeOpts) []string {
	if opts.Keyframes != nil && len(opts.Keyframes.IFrames) > 0 {
		segTimes := KeyframesToSegmentTimes(opts.Keyframes, 4.0)
		if len(segTimes) > 0 {
			return appendSegmentOutput(args, opts.OutputDir, segTimes)
		}
	}

	return appendHLSOutput(args, opts.OutputDir)
}

func appendSegmentOutput(args []string, outputDir string, segTimes []float64) []string {
	timeStrs := make([]string, len(segTimes))
	for i, t := range segTimes {
		timeStrs[i] = fmt.Sprintf("%.3f", t)
	}

	args = append(args,
		"-f", "segment",
		"-segment_times", strings.Join(timeStrs, ","),
		"-segment_format", "mp4",
		"-segment_format_options", "movflags=+frag_keyframe+empty_moov+default_base_moof",
		"-segment_list", outputDir+"/index.m3u8",
		"-segment_list_type", "m3u8",
		outputDir+"/segment_%04d.m4s",
	)
	return args
}

func buildHLSArgs(opts TranscodeOpts, outputDir string) []string {
	var args []string
	args = append(args, "-nostats", "-loglevel", "warning")
	args = append(args, opts.HWAccel.InputFlags...)

	if opts.StartTime > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", opts.StartTime))
	}

	if opts.InputReader != nil || opts.Input == "pipe:0" {
		args = append(args, "-i", "pipe:0")
	} else {
		args = append(args, "-i", opts.Input)
	}

	args = append(args, "-map", "0:v:0")
	args = append(args, "-map", fmt.Sprintf("0:a:%d", opts.AudioTrack))

	args = appendVideoArgs(args, opts)
	args = appendAudioArgs(args, opts.Profile)

	args = append(args,
		"-f", "segment",
		"-segment_time", fmt.Sprintf("%.1f", SegmentDuration),
		"-segment_format", "mpegts",
		"-segment_start_number", "0",
		filepath.Join(outputDir, "seg_%04d.ts"),
	)
	return args
}

func appendHLSOutput(args []string, outputDir string) []string {
	args = append(args,
		"-f", "hls",
		"-hls_time", "6",
		"-hls_segment_type", "fmp4",
		"-hls_playlist_type", "event",
		"-hls_segment_filename", outputDir+"/segment_%04d.m4s",
		outputDir+"/index.m3u8",
	)
	return args
}

func TranscodeToHLS(ctx context.Context, input, outputDir string, profile Profile) error {
	return TranscodeToHLSWithOpts(ctx, TranscodeOpts{
		Input:   input,
		OutputDir: outputDir,
		Profile: profile,
		HWAccel: BuildHwAccelConfig(HwAccelNone),
	})
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

func BuildFFmpegPipe(ctx context.Context, opts TranscodeOpts) *exec.Cmd {
	args := buildMP4TranscodeArgs(opts, "pipe:1")
	return exec.CommandContext(ctx, "ffmpeg", args...)
}

func TranscodeToMP4(ctx context.Context, opts TranscodeOpts, outputPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Minute)
	defer cancel()

	args := buildMP4TranscodeArgs(opts, outputPath)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if opts.InputReader != nil {
		cmd.Stdin = opts.InputReader
	}
	return cmd.Run()
}

func buildMP4TranscodeArgs(opts TranscodeOpts, outputPath string) []string {
	var args []string

	args = append(args, "-y", "-nostats", "-loglevel", "warning")
	args = append(args, opts.HWAccel.InputFlags...)

	if opts.StartTime > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", opts.StartTime))
	}

	if opts.InputReader != nil {
		args = append(args, "-i", "pipe:0")
	} else {
		args = append(args, "-i", opts.Input)
	}

	if opts.Duration > 0 {
		args = append(args, "-t", fmt.Sprintf("%.3f", opts.Duration))
	}

	args = append(args, "-map", "0:v:0")
	args = append(args, "-map", fmt.Sprintf("0:a:%d", opts.AudioTrack))

	if opts.BurnSubtitles && opts.SubtitleTrack >= 0 {
		args = appendSubtitleBurn(args, opts)
	} else {
		args = appendVideoArgs(args, opts)
	}

	args = appendAudioArgs(args, opts.Profile)

	args = append(args,
		"-movflags", "+frag_keyframe+empty_moov+default_base_moof",
		"-f", "mp4",
		outputPath,
	)
	return args
}

func appendSubtitleBurn(args []string, opts TranscodeOpts) []string {
	p := opts.Profile
	hw := opts.HWAccel

	if p.VideoCodec == "copy" {
		p.VideoCodec = "libx264"
	}

	encoder := resolveVideoEncoder(p.VideoCodec, hw)
	args = append(args, "-c:v", encoder)

	if p.CRF > 0 && hw.Type == HwAccelNone {
		args = append(args, "-crf", strconv.Itoa(p.CRF))
	}
	if hw.Type == HwAccelVideoToolbox {
		args = append(args, "-q:v", "65")
	}
	if hw.Type == HwAccelNVENC {
		args = append(args, "-rc", "vbr", "-cq", strconv.Itoa(p.CRF))
	}
	if hw.Type == HwAccelQSV {
		args = append(args, "-global_quality", strconv.Itoa(p.CRF))
	}
	if p.MaxBitrate != "" {
		args = append(args, "-maxrate", p.MaxBitrate, "-bufsize", p.MaxBitrate)
	}
	if p.Preset != "" && hw.Type == HwAccelNone {
		args = append(args, "-preset", p.Preset)
	}

	isASS := opts.SubtitleCodec == "ass" || opts.SubtitleCodec == "ssa"
	subIdx := opts.SubtitleTrack

	var filter string
	if isASS {
		if opts.Input != "pipe:0" && opts.Input != "" {
			escapedInput := strings.ReplaceAll(opts.Input, "\\", "\\\\")
			escapedInput = strings.ReplaceAll(escapedInput, "'", "\\'")
			escapedInput = strings.ReplaceAll(escapedInput, ":", "\\:")
			filter = fmt.Sprintf("ass='%s':si=%d", escapedInput, subIdx)
		} else {
			filter = fmt.Sprintf("subtitles='%s':si=%d", opts.Input, subIdx)
		}
	} else {
		filter = fmt.Sprintf("subtitles='%s':si=%d", opts.Input, subIdx)
	}

	if p.MaxHeight > 0 {
		switch hw.Type {
		case HwAccelVideoToolbox:
			filter = fmt.Sprintf("scale=-2:min(%d\\,ih),format=nv12,%s", p.MaxHeight, filter)
		default:
			filter = fmt.Sprintf("scale=-2:'min(%d,ih)',%s", p.MaxHeight, filter)
		}
	}

	args = append(args, "-vf", filter)
	return args
}

func ExtractSubtitles(ctx context.Context, input string, streamIndex int, output string) error {
	return ExtractSubtitlesAs(ctx, input, streamIndex, output, "webvtt")
}

func ExtractSubtitlesAs(ctx context.Context, input string, streamIndex int, output string, codec string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-nostats", "-loglevel", "warning",
		"-i", input,
		"-map", fmt.Sprintf("0:%d", streamIndex),
		"-c:s", codec,
		output,
	)
	return cmd.Run()
}

func ExtractSubtitlesFromReaderAs(ctx context.Context, reader io.Reader, streamIndex int, output string, codec string) error {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-nostats", "-loglevel", "warning",
		"-i", "pipe:0",
		"-map", fmt.Sprintf("0:%d", streamIndex),
		"-c:s", codec,
		output,
	)
	cmd.Stdin = reader
	return cmd.Run()
}

func ExtractSubtitlesFromReader(ctx context.Context, reader io.Reader, streamIndex int, output string) error {
	return ExtractSubtitlesFromReaderAs(ctx, reader, streamIndex, output, "webvtt")
}
