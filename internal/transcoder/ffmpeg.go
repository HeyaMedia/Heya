package transcoder

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CommandBuilder abstracts how transcode commands are constructed.
// The session manager uses this interface so it doesn't need to know
// which encoder binary (ffmpeg, etc.) is behind the scenes.
type CommandBuilder interface {
	// BuildHLSCommand returns an exec.Cmd that will transcode the input
	// into HLS segments written to opts.OutputDir. The caller is responsible
	// for wiring Stdin (e.g. for SMB pipe:0 inputs), starting the process,
	// and reading the segment-list from Stdout.
	BuildHLSCommand(ctx context.Context, opts TranscodeOpts) (*exec.Cmd, error)

	// BuildMP4Command returns an exec.Cmd that will transcode the input
	// into a single fragmented MP4 at outputPath.
	BuildMP4Command(ctx context.Context, opts TranscodeOpts, outputPath string) (*exec.Cmd, error)

	// ExtractKeyframesCmd returns an exec.Cmd whose stdout will emit
	// CSV lines of (pts_time, flags) for keyframe extraction.
	ExtractKeyframesCmd(ctx context.Context, filePath string) (*exec.Cmd, error)

	// IsAvailable reports whether the underlying encoder binary can be found.
	IsAvailable() bool

	// FormatCommand returns a human-readable string of the full command,
	// useful for debug logging.
	FormatCommand(cmd *exec.Cmd) string
}

// FFmpegBuilder implements CommandBuilder using ffmpeg / ffprobe.
type FFmpegBuilder struct{}

// NewFFmpegBuilder returns a new FFmpegBuilder.
func NewFFmpegBuilder() *FFmpegBuilder {
	return &FFmpegBuilder{}
}

func (f *FFmpegBuilder) BuildHLSCommand(ctx context.Context, opts TranscodeOpts) (*exec.Cmd, error) {
	if opts.OutputDir == "" {
		return nil, fmt.Errorf("BuildHLSCommand: OutputDir is required")
	}
	args := BuildHLSArgs(opts, opts.OutputDir)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	return cmd, nil
}

func (f *FFmpegBuilder) BuildMP4Command(ctx context.Context, opts TranscodeOpts, outputPath string) (*exec.Cmd, error) {
	args := buildMP4TranscodeArgs(opts, outputPath)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	return cmd, nil
}

func (f *FFmpegBuilder) ExtractKeyframesCmd(ctx context.Context, filePath string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-select_streams", "v:0",
		"-show_entries", "packet=pts_time,flags",
		"-of", "csv=p=0",
		filePath,
	)
	return cmd, nil
}

func (f *FFmpegBuilder) IsAvailable() bool {
	return IsFFmpegAvailable()
}

func (f *FFmpegBuilder) FormatCommand(cmd *exec.Cmd) string {
	return strings.Join(cmd.Args, " ")
}
