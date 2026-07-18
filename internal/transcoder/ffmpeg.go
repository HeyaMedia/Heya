package transcoder

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/karbowiak/heya/internal/vfs"
)

// CommandBuilder abstracts how transcode commands are constructed.
// The session manager uses this interface so it doesn't need to know
// which encoder binary (ffmpeg, etc.) is behind the scenes.
type CommandBuilder interface {
	// BuildHLSCommand returns an exec.Cmd that will transcode the input
	// into HLS segments written to opts.OutputDir. The caller is responsible
	// for starting the process and reading the segment list from Stdout.
	BuildHLSCommand(ctx context.Context, opts TranscodeOpts) (*exec.Cmd, error)

	// IsAvailable reports whether the underlying encoder binary can be found.
	IsAvailable() bool

	// FormatCommand returns a human-readable string of the full command,
	// useful for debug logging.
	FormatCommand(cmd *exec.Cmd) string
}

// FFmpegBuilder implements CommandBuilder using ffmpeg / ffprobe.
type FFmpegBuilder struct{}

// ffmpegCommandContext constructs the fixed ffmpeg executable directly.
// Arguments are discrete argv entries and are never interpreted by a shell.
func ffmpegCommandContext(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "ffmpeg", args...) //nolint:gosec // fixed executable with non-shell argv
}

// NewFFmpegBuilder returns a new FFmpegBuilder.
func NewFFmpegBuilder() *FFmpegBuilder {
	return &FFmpegBuilder{}
}

func (f *FFmpegBuilder) BuildHLSCommand(ctx context.Context, opts TranscodeOpts) (*exec.Cmd, error) {
	if opts.OutputDir == "" {
		return nil, fmt.Errorf("BuildHLSCommand: OutputDir is required")
	}
	if err := vfs.ValidateLocalPath(opts.Input); err != nil {
		return nil, fmt.Errorf("BuildHLSCommand: input: %w", err)
	}
	args := BuildHLSArgs(opts, opts.OutputDir)
	cmd := ffmpegCommandContext(ctx, args...)
	return cmd, nil
}

func (f *FFmpegBuilder) IsAvailable() bool {
	return IsFFmpegAvailable()
}

func (f *FFmpegBuilder) FormatCommand(cmd *exec.Cmd) string {
	return strings.Join(cmd.Args, " ")
}
