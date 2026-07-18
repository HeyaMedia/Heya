package vfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/karbowiak/heya/internal/secrettext"
)

type Source struct {
	FS       fs.FS
	RootPath string
}

var ErrUnsupportedPathScheme = errors.New("URL-style library paths are not supported")

// ValidateLocalPath rejects transport URLs before they reach os.Stat/os.Open.
// Network storage remains fully supported when it is mounted by the host or
// container and configured using the resulting ordinary filesystem path.
//
// In particular, this gives installations with legacy smb:// rows a clear
// migration diagnostic instead of accidentally treating the URL as a local
// filename and returning a misleading "no such file" error.
func ValidateLocalPath(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return errors.New("filesystem path is required")
	}
	if trimmed != path {
		return errors.New("filesystem path must not have surrounding whitespace")
	}
	path = trimmed
	scheme, ok := urlStyleScheme(path)
	if ok {
		if strings.EqualFold(scheme, "smb") {
			return fmt.Errorf("%w: smb:// paths are no longer supported; mount the share on the host or in the container and configure that filesystem path", ErrUnsupportedPathScheme)
		}
		return fmt.Errorf("%w: %s://; mount or expose the media as a filesystem path", ErrUnsupportedPathScheme, strings.ToLower(scheme))
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("filesystem path must be absolute: %q", path)
	}
	return nil
}

func urlStyleScheme(path string) (string, bool) {
	i := strings.Index(path, "://")
	if i <= 0 {
		return "", false
	}
	for j, r := range path[:i] {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (j > 0 && ((r >= '0' && r <= '9') || r == '+' || r == '-' || r == '.')) {
			continue
		}
		return "", false
	}
	return path[:i], true
}

func Open(path string) (*Source, error) {
	return OpenContext(context.Background(), path)
}

// OpenContext exposes an ordinary directory through fs.FS. Network shares are
// expected to be mounted by the OS/container and are therefore ordinary paths
// here as well.
func OpenContext(ctx context.Context, path string) (*Source, error) {
	if ctx == nil {
		return nil, fmt.Errorf("open virtual filesystem: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateLocalPath(path); err != nil {
		return nil, err
	}

	return openLocal(path)
}

func openLocal(path string) (*Source, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("local path %q: %w", path, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("local path %q is not a directory", path)
	}

	return &Source{
		FS:       os.DirFS(path),
		RootPath: path,
	}, nil
}

// OpenFile validates and opens an ordinary filesystem path. The returned
// value is the real *os.File; callers do not need to learn a transport-shaped
// wrapper for host/container-mounted storage.
func OpenFile(path string) (*os.File, error) {
	if err := ValidateLocalPath(path); err != nil {
		return nil, err
	}
	return os.Open(path) //nolint:gosec // path is a configured library file
}

func RedactPath(path string) string {
	return secrettext.Redact(path)
}

// RedactError sanitizes URL credentials in an error for logging or response
// presentation. Inspect the original error first if errors.Is/As is needed.
func RedactError(err error) error {
	return secrettext.RedactError(err)
}
