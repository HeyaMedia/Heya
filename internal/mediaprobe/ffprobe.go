package mediaprobe

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/karbowiak/heya/internal/vfs"
)

// Func probes a filesystem media path and returns its normalized media
// description. Keeping the function type here lets scanners and matchers
// accept a test double without depending on the worker package.
type Func func(context.Context, string) (*MediaInfo, error)

// Probe runs ffprobe against an ordinary filesystem path. Network shares are
// mounted by the host/container and therefore use the same path flow.
func Probe(ctx context.Context, path string) (*MediaInfo, error) {
	if err := vfs.ValidateLocalPath(path); err != nil {
		return nil, fmt.Errorf("media input: %w", err)
	}

	cmd := exec.CommandContext(ctx, "ffprobe", //nolint:gosec // path is a configured library file; executable is fixed
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"-i", path,
	)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("execute ffprobe: %w", err)
	}

	info, err := Parse(output)
	if err != nil {
		return nil, fmt.Errorf("parse ffprobe output: %w", err)
	}
	return info, nil
}
