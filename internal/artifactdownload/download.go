// Package artifactdownload downloads pinned public runtime/model artifacts
// without exposing partial files or allowing an upstream to consume unbounded
// disk space.
package artifactdownload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/karbowiak/heya/internal/safedial"
)

var (
	ErrTooLarge     = errors.New("artifact download exceeds size limit")
	ErrSizeMismatch = errors.New("artifact download size mismatch")
	ErrHashMismatch = errors.New("artifact download checksum mismatch")
)

// Spec describes one pinned artifact publication. MaxBytes is mandatory and
// is enforced for both declared and chunked/unknown-length responses.
// ExpectedBytes, when positive, is an exact-size check in addition to the hard
// maximum. SHA256 is an optional lowercase or uppercase hexadecimal digest.
type Spec struct {
	URL           string
	Destination   string
	MaxBytes      int64
	ExpectedBytes int64
	SHA256        string
	Mode          fs.FileMode
	Progress      func(int64)
}

// NewClient returns the public-network-only HTTP client used for pinned
// artifacts. It disables environment proxies, rejects private destinations
// after DNS resolution, and revalidates every redirect. timeout is a whole-
// request ceiling, including reading the response body.
func NewClient(timeout time.Duration) *http.Client {
	client := safedial.NewPublicHTTPClient()
	client.Timeout = timeout
	return client
}

// Fetch downloads, validates, and atomically publishes one artifact. The
// temporary file is unique and lives beside Destination, so concurrent Heya
// processes cannot truncate each other's in-flight downloads and Rename stays
// on the same filesystem. Failed downloads always remove their temporary file.
//
// A caller-supplied client is deliberately accepted for hermetic tests. In
// production it must provide the same public-network guarantees as NewClient.
func Fetch(ctx context.Context, client *http.Client, spec Spec) (written int64, returnErr error) {
	if client == nil {
		return 0, errors.New("artifact download HTTP client is nil")
	}
	if spec.URL == "" {
		return 0, errors.New("artifact download URL is empty")
	}
	if spec.Destination == "" {
		return 0, errors.New("artifact download destination is empty")
	}
	if spec.MaxBytes <= 0 {
		return 0, errors.New("artifact download size limit must be positive")
	}
	if spec.ExpectedBytes < 0 || spec.ExpectedBytes > spec.MaxBytes {
		return 0, fmt.Errorf("invalid expected artifact size %d for limit %d", spec.ExpectedBytes, spec.MaxBytes)
	}
	if spec.Mode == 0 {
		spec.Mode = 0o640
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return 0, fmt.Errorf("build artifact request: %w", err)
	}
	resp, err := client.Do(req) //nolint:gosec // production clients come from NewClient; tests inject a hermetic client
	if err != nil {
		return 0, fmt.Errorf("download artifact: %w", err)
	}
	// Once validation and Rename succeed, publication is durable. A response-body
	// close error after EOF must not turn that committed success into a retry.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download artifact: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > spec.MaxBytes {
		return 0, fmt.Errorf("%w: declared %d bytes, limit %d", ErrTooLarge, resp.ContentLength, spec.MaxBytes)
	}
	if spec.ExpectedBytes > 0 && resp.ContentLength >= 0 && resp.ContentLength != spec.ExpectedBytes {
		return 0, fmt.Errorf("%w: declared %d bytes, expected %d", ErrSizeMismatch, resp.ContentLength, spec.ExpectedBytes)
	}

	dir := filepath.Dir(spec.Destination)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return 0, fmt.Errorf("create artifact directory: %w", err)
	}
	temporary, err := os.CreateTemp(dir, filepath.Base(spec.Destination)+".tmp-*")
	if err != nil {
		return 0, fmt.Errorf("create artifact temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if temporaryPath != "" {
			returnErr = errors.Join(returnErr, removeTemporary(temporaryPath))
		}
	}()
	if err := temporary.Chmod(spec.Mode); err != nil {
		return 0, fmt.Errorf("set artifact file mode: %w", err)
	}

	hasher := sha256.New()
	writers := []io.Writer{temporary, hasher}
	if spec.Progress != nil {
		writers = append(writers, &progressWriter{notify: spec.Progress})
	}
	limited := io.LimitReader(resp.Body, spec.MaxBytes+1)
	written, err = io.CopyBuffer(io.MultiWriter(writers...), limited, make([]byte, 1<<20))
	if err != nil {
		return written, fmt.Errorf("read artifact body: %w", err)
	}
	if written > spec.MaxBytes {
		return written, fmt.Errorf("%w: received more than %d bytes", ErrTooLarge, spec.MaxBytes)
	}
	if spec.ExpectedBytes > 0 && written != spec.ExpectedBytes {
		return written, fmt.Errorf("%w: received %d bytes, expected %d", ErrSizeMismatch, written, spec.ExpectedBytes)
	}
	if spec.SHA256 != "" {
		got := hex.EncodeToString(hasher.Sum(nil))
		if !equalASCIIFold(got, spec.SHA256) {
			return written, fmt.Errorf("%w: got %s, expected %s", ErrHashMismatch, got, spec.SHA256)
		}
	}
	if err := temporary.Sync(); err != nil {
		return written, fmt.Errorf("sync artifact temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return written, fmt.Errorf("close artifact temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, spec.Destination); err != nil {
		return written, fmt.Errorf("publish artifact: %w", err)
	}
	temporaryPath = ""
	return written, nil
}

type progressWriter struct {
	total  int64
	notify func(int64)
}

func (writer *progressWriter) Write(body []byte) (int, error) {
	writer.total += int64(len(body))
	writer.notify(writer.total)
	return len(body), nil
}

func removeTemporary(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("remove artifact temporary file: %w", err)
}

func equalASCIIFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range len(left) {
		l, r := left[i], right[i]
		if l >= 'A' && l <= 'Z' {
			l += 'a' - 'A'
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if l != r {
			return false
		}
	}
	return true
}
