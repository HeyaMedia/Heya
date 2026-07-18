package llm

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/artifactdownload"
)

// Download plumbing for the two local artifacts: the llama-server bundle
// (small tar.gz, extracted) and the GGUF model (single large file). Same
// contract as sonicanalysis.ModelFetcher: temp file + hash-while-writing +
// verify + atomic rename, with an in-memory progress snapshot polled by the
// status endpoint. URLs and hashes are pinned constants (artifacts.go), and the
// shared downloader still enforces public-only DNS/redirects plus hard size
// bounds so a compromised upstream cannot pivot inward or consume the disk.

// DownloadState mirrors the sonic fetcher's state machine.
type DownloadState string

const (
	DownloadIdle        DownloadState = "idle"
	DownloadDownloading DownloadState = "downloading"
	DownloadReady       DownloadState = "ready"
	DownloadFailed      DownloadState = "failed"
)

// GGUFs are multi-gigabyte deliberate downloads. Six hours is a finite safety
// net while still accommodating slow links; connect/TLS/header waits have much
// shorter deadlines in artifactdownload's public-only transport.
const localArtifactDownloadTimeout = 6 * time.Hour

var localArtifactHTTPClient = artifactdownload.NewClient(localArtifactDownloadTimeout)

// DownloadProgress is the poll-friendly snapshot of an in-flight download.
type DownloadProgress struct {
	CurrentFile string    `json:"current_file,omitempty"`
	BytesDone   int64     `json:"bytes_done"`
	BytesTotal  int64     `json:"bytes_total"`
	StartedAt   time.Time `json:"started_at"`
}

// fetchFile downloads url to destPath atomically, enforcing the catalog's
// exact size and checksum while reporting cumulative byte progress.
func fetchFile(ctx context.Context, url, sha string, size int64, destPath string, onProgress func(int64)) error {
	_, err := artifactdownload.Fetch(ctx, localArtifactHTTPClient, artifactdownload.Spec{
		URL:           url,
		Destination:   destPath,
		MaxBytes:      size,
		ExpectedBytes: size,
		SHA256:        sha,
		Mode:          0o640,
		Progress:      onProgress,
	})
	if err != nil {
		// Concurrent Heya processes can race at publication. Both downloads are
		// verified; if a peer installed the exact-size destination first, use it.
		if fileSizeMatches(destPath, size) {
			return nil
		}
		return fmt.Errorf("fetch %s: %w", filepath.Base(destPath), err)
	}
	return nil
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
