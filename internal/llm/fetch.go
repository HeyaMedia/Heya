package llm

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Download plumbing for the two local artifacts: the llama-server bundle
// (small tar.gz, extracted) and the GGUF model (single large file). Same
// contract as sonicanalysis.ModelFetcher: temp file + hash-while-writing +
// verify + atomic rename, with an in-memory progress snapshot polled by the
// status endpoint. URLs and hashes are pinned constants (artifacts.go), so no
// SSRF guard is needed — same trust model as the sonic model manifest.

// DownloadState mirrors the sonic fetcher's state machine.
type DownloadState string

const (
	DownloadIdle        DownloadState = "idle"
	DownloadDownloading DownloadState = "downloading"
	DownloadReady       DownloadState = "ready"
	DownloadFailed      DownloadState = "failed"
)

// DownloadProgress is the poll-friendly snapshot of an in-flight download.
type DownloadProgress struct {
	CurrentFile string    `json:"current_file,omitempty"`
	BytesDone   int64     `json:"bytes_done"`
	BytesTotal  int64     `json:"bytes_total"`
	StartedAt   time.Time `json:"started_at"`
}

// fetchFile downloads url to destPath atomically, verifying sha256 and
// reporting byte progress via onProgress (called with cumulative bytes).
func fetchFile(ctx context.Context, url, sha, destPath string, onProgress func(int64)) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}
	tmpPath := destPath + ".tmp"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	dst, err := os.Create(tmpPath) //nolint:gosec // G304: path built from server-controlled data dir + pinned filename
	if err != nil {
		return err
	}
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(dst, hasher, progressWriter(onProgress)), resp.Body)
	closeErr := dst.Close()
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("download body: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close tmp: %w", closeErr)
	}
	_ = written

	if sha != "" {
		got := hex.EncodeToString(hasher.Sum(nil))
		if got != sha {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("sha256 mismatch for %s: got %s, want %s", filepath.Base(destPath), got, sha)
		}
	}
	return os.Rename(tmpPath, destPath)
}

// progressWriter adapts a cumulative-bytes callback into an io.Writer for
// io.MultiWriter.
type progressWriterFn func(int64)

func progressWriter(fn func(int64)) io.Writer {
	if fn == nil {
		return io.Discard
	}
	return &countingWriter{fn: fn}
}

type countingWriter struct {
	fn    progressWriterFn
	total int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.total += int64(len(p))
	w.fn(w.total)
	return len(p), nil
}

// extractServerBundle unpacks the llama.cpp release tar.gz into destDir,
// flattening everything into one directory (the bundles are a single
// `llama-<build>/` dir of binaries + shared libs linked via $ORIGIN/@rpath,
// so flat co-location matches upstream's own layout). Extraction goes to
// destDir+".tmp" then renames for atomicity; the presence of llama-server is
// verified before the rename.
func extractServerBundle(archivePath, destDir string) error {
	tmpDir := destDir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return err
	}
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		return err
	}

	f, err := os.Open(archivePath) //nolint:gosec // G304: server-built path
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	extracted := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		base := filepath.Base(filepath.ToSlash(filepath.Clean(hdr.Name)))
		if hdr.Typeflag == tar.TypeSymlink {
			// SONAME links (libllama.0.dylib → libllama.0.0.9941.dylib).
			// Everything is flattened into one dir, so the target is always a
			// sibling — keep only its basename to kill any traversal.
			target := filepath.Base(filepath.ToSlash(filepath.Clean(hdr.Linkname)))
			if base == "." || target == "." || strings.Contains(base, "..") {
				continue
			}
			if err := os.Symlink(target, filepath.Join(tmpDir, base)); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			extracted++
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if base == "." || base == "/" || strings.Contains(base, "..") {
			continue
		}
		mode := os.FileMode(0o640)
		if hdr.FileInfo().Mode()&0o100 != 0 {
			mode = 0o750
		}
		out, err := os.OpenFile(filepath.Join(tmpDir, base), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) //nolint:gosec // G304: base sanitized above
		if err != nil {
			return err
		}
		// The bundle is a pinned, checksum-verified release — bounded copy
		// mainly pacifies the decompression-bomb linter.
		if _, err := io.Copy(out, io.LimitReader(tr, 4<<30)); err != nil { //nolint:gosec // G110: source verified by sha256
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		extracted++
	}
	if extracted == 0 {
		return fmt.Errorf("no files found in %s", filepath.Base(archivePath))
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "llama-server")); err != nil {
		return fmt.Errorf("bundle %s did not contain llama-server", filepath.Base(archivePath))
	}

	if err := os.RemoveAll(destDir); err != nil {
		return err
	}
	return os.Rename(tmpDir, destDir)
}

// fileSizeMatches reports whether path exists as a regular file of exactly
// want bytes. Presence checks use size (not a re-hash) — hashing a 2.5 GB
// GGUF on every status poll would be absurd; integrity was verified at
// download time.
func fileSizeMatches(path string, want int64) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && (want <= 0 || st.Size() == want)
}
