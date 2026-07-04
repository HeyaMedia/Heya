package transcoder

import (
	"context"
	"fmt"
	"io"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func PlannedSegmentTimes(kf *Keyframes, duration float64, target float64) []float64 {
	if target <= 0 {
		target = SegmentDuration
	}
	if kf != nil && len(kf.IFrames) > 1 {
		ends := keyframeBoundaries(kf, duration, target)
		if len(ends) > 0 {
			return ends
		}
	}
	return fixedIntervalBoundaries(duration, target)
}

func keyframeBoundaries(kf *Keyframes, duration float64, target float64) []float64 {
	if duration <= 0 {
		duration = kf.Duration
	}
	minSeg := target * 0.75
	var ends []float64
	lastCut := 0.0
	for _, ts := range kf.IFrames {
		if ts <= lastCut {
			continue
		}
		if ts-lastCut >= minSeg {
			ends = append(ends, ts)
			lastCut = ts
		}
	}
	if duration > lastCut+0.1 {
		ends = append(ends, duration)
	}
	return ends
}

func fixedIntervalBoundaries(duration float64, target float64) []float64 {
	if duration <= 0 {
		return []float64{1}
	}
	n := int(math.Ceil(duration / target))
	if n < 1 {
		n = 1
	}
	out := make([]float64, n)
	for i := 0; i < n-1; i++ {
		out[i] = float64(i+1) * target
	}
	out[n-1] = duration
	return out
}

func IsFFmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

func IsFFprobeAvailable() bool {
	_, err := exec.LookPath("ffprobe")
	return err == nil
}

type TranscodeOpts struct {
	Input        string
	OutputDir    string
	Profile      Profile
	HWAccel      HwAccelConfig
	Keyframes    *Keyframes
	StartTime    float64
	AudioTrack   int
	StartSegment int
	ToneMap      bool
	UseFMP4      bool

	// Plan carries surgical fixes (deinterlace, rotation, anamorphic,
	// HEVC retag, DV EL strip) that the upstream decision layer chose. Nil
	// means "no extra work" — the existing scale/tonemap behaviour applies.
	Plan *PlaybackPlan
}

func TranscodeToHLSWithOpts(ctx context.Context, opts TranscodeOpts) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	args := buildTranscodeArgs(opts)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	return cmd.Run()
}

func buildTranscodeArgs(opts TranscodeOpts) []string {
	var args []string

	args = append(args, opts.HWAccel.InputFlags...)

	if opts.StartTime > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", opts.StartTime))
	}

	args = append(args, "-i", opts.Input)

	args = appendVideoArgs(args, opts)
	args = appendAudioArgs(args, opts.Profile)
	args = appendOutputArgs(args, opts)

	return args
}

func appendVideoArgs(args []string, opts TranscodeOpts) []string {
	p := opts.Profile
	hw := opts.HWAccel

	if p.VideoCodec == "" || p.VideoCodec == "copy" {
		args = append(args, "-c:v", "copy")
		if opts.Plan != nil {
			if opts.Plan.RetagHEVC {
				// Safari (and a few other clients) only play HEVC tagged as
				// hvc1. ffmpeg's MP4 muxer defaults to hev1 for HEVC copies,
				// so we explicitly retag.
				args = append(args, "-tag:v", "hvc1")
			}
			if opts.Plan.StripDoViEL {
				// Strip the Dolby Vision RPU NALs from each access unit. The
				// HDR10 base layer remains intact and plays on any HEVC+HDR10
				// client. Requires ffmpeg 6.0+.
				args = append(args, "-bsf:v", "hevc_metadata=remove_dovi_rpu=1")
			}
		}
		return args
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

	filterChain := buildVideoFilterChain(opts)
	if filterChain != "" {
		args = append(args, "-vf", filterChain)
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

func buildVideoFilterChain(opts TranscodeOpts) string {
	h := opts.Profile.MaxHeight
	hw := opts.HWAccel

	var steps []string

	// Plan-driven surgical fixes apply BEFORE tone-map/scale: deinterlace and
	// rotation need to happen in the source-pixel space; anamorphic SAR
	// correction is folded into the final scale.
	if opts.Plan != nil {
		if opts.Plan.Deinterlace {
			if f := deinterlaceFilter(hw); f != "" {
				steps = append(steps, f)
			}
		}
		if opts.Plan.Rotate != 0 {
			if f := rotationFilter(opts.Plan.Rotate); f != "" {
				steps = append(steps, f)
			}
		}
	}

	if opts.ToneMap {
		if tm := buildToneMapFilterChain(hw, h); tm != "" {
			steps = append(steps, tm)
		}
		return strings.Join(steps, ",")
	}

	// Anamorphic fix: setsar=1:1 forces square pixels. We let the resolution
	// scale further down handle final sizing (DAR is preserved by the source
	// width/height, which we don't override).
	if opts.Plan != nil && opts.Plan.FixAnamorphic {
		steps = append(steps, "setsar=1:1")
	}

	if h > 0 {
		var scale string
		switch hw.Type {
		case HwAccelNVENC:
			scale = fmt.Sprintf("scale_cuda=-2:min'(%d,ih)'", h)
		case HwAccelVAAPI:
			scale = fmt.Sprintf("scale_vaapi=w=-2:h=min(%d\\,ih)", h)
		case HwAccelQSV:
			scale = fmt.Sprintf("scale_qsv=w=-2:h=min(%d\\,ih)", h)
		case HwAccelVideoToolbox:
			scale = fmt.Sprintf("scale=-2:min(%d\\,ih),format=nv12", h)
		default:
			scale = fmt.Sprintf("scale=-2:'min(%d,ih)'", h)
		}
		steps = append(steps, scale)
	}

	return strings.Join(steps, ",")
}

// deinterlaceFilter picks the appropriate deinterlacer for the active
// hardware accel mode. Returns "" if deinterlacing isn't supported on the
// current path (caller should fall back to no-op).
func deinterlaceFilter(hw HwAccelConfig) string {
	switch hw.Type {
	case HwAccelVAAPI:
		return "deinterlace_vaapi"
	case HwAccelQSV:
		return "vpp_qsv=deinterlace=2"
	default:
		return "yadif"
	}
}

// rotationFilter returns the ffmpeg transpose chain for the given clockwise
// rotation. 0 means no filter. Anything else is a single or chained transpose.
func rotationFilter(degrees int) string {
	// Normalise to canonical 0/90/180/270 CW.
	d := degrees % 360
	if d < 0 {
		d += 360
	}
	switch d {
	case 90:
		return "transpose=1"
	case 180:
		return "transpose=2,transpose=2"
	case 270:
		return "transpose=2"
	}
	return ""
}

func buildToneMapFilterChain(hw HwAccelConfig, maxHeight int) string {
	scale := ""
	if maxHeight > 0 {
		scale = fmt.Sprintf(",scale=-2:'min(%d,ih)'", maxHeight)
	}

	switch hw.Type {
	case HwAccelVAAPI:
		if maxHeight > 0 {
			return fmt.Sprintf("tonemap_vaapi=t=bt709:p=bt709:m=bt709,scale_vaapi=w=-2:h=min(%d\\,ih)", maxHeight)
		}
		return "tonemap_vaapi=t=bt709:p=bt709:m=bt709"
	case HwAccelQSV:
		if maxHeight > 0 {
			return fmt.Sprintf("vpp_qsv=tonemap=1:format=nv12,scale_qsv=w=-2:h=min(%d\\,ih)", maxHeight)
		}
		return "vpp_qsv=tonemap=1:format=nv12"
	case HwAccelNVENC:
		return "hwdownload,format=nv12,zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p" + scale + ",hwupload_cuda"
	default:
		return "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p" + scale
	}
}

func appendAudioArgs(args []string, p Profile) []string {
	if p.AudioCodec != "" && p.AudioCodec != "copy" {
		args = append(args, "-c:a", p.AudioCodec)
		// Downmix to stereo. Multi-channel AAC is poorly supported in
		// browser MSE pipelines (Firefox/macOS in particular fails with
		// "AudioConverter cookie" errors on 5.1 AAC). Stereo is universally
		// safe and the bitrates we use are tuned for it.
		args = append(args, "-ac", "2")
		if p.Name == "audio" {
			args = append(args, "-b:a", "256k")
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

func BuildHLSArgs(opts TranscodeOpts, outputDir string) []string {
	var args []string
	args = append(args, "-nostats", "-loglevel", "info")
	// Emit structured progress on FD 3. The session wires an os.Pipe to
	// cmd.ExtraFiles so this address resolves correctly. Cheap to enable
	// unconditionally — ffmpeg silently drops if the fd doesn't exist.
	args = append(args, "-progress", "pipe:3")
	args = append(args, opts.HWAccel.InputFlags...)

	if opts.StartTime > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.6f", opts.StartTime))
	}

	args = append(args, "-i", opts.Input)

	if opts.UseFMP4 {
		args = append(args, "-copyts", "-avoid_negative_ts", "disabled")
	} else {
		args = append(args, "-copyts", "-muxdelay", "0", "-start_at_zero")
	}

	args = append(args, "-map", "0:v:0")
	args = append(args, "-map", fmt.Sprintf("0:a:%d", opts.AudioTrack))

	args = appendVideoArgs(args, opts)
	args = appendAudioArgs(args, opts.Profile)

	if opts.UseFMP4 && opts.Profile.VideoCodec != "copy" && opts.Profile.VideoCodec != "" {
		args = append(args, "-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%.1f)", SegmentDuration))
	}

	startNum := opts.StartSegment

	if opts.UseFMP4 {
		args = append(args,
			"-f", "hls",
			"-hls_time", fmt.Sprintf("%.1f", SegmentDuration),
			"-hls_segment_type", "fmp4",
			"-hls_fmp4_init_filename", "init.mp4",
			"-hls_segment_filename", filepath.Join(outputDir, "seg_%d.m4s"),
			"-hls_playlist_type", "vod",
			"-hls_list_size", "0",
			"-hls_flags", "independent_segments+temp_file",
			"-start_number", strconv.Itoa(startNum),
			filepath.Join(outputDir, "_ffmpeg.m3u8"),
		)
	} else {
		args = append(args,
			"-f", "segment",
			"-segment_time", fmt.Sprintf("%.1f", SegmentDuration),
			"-segment_format", "mpegts",
			"-segment_start_number", strconv.Itoa(startNum),
			"-segment_list", "pipe:1",
			"-segment_list_type", "flat",
			filepath.Join(outputDir, "seg_%04d.ts"),
		)
	}
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
		Input:     input,
		OutputDir: outputDir,
		Profile:   profile,
		HWAccel:   BuildHwAccelConfig(HwAccelNone),
	})
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
