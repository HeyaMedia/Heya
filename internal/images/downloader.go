package images

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type Downloader struct {
	dataDir string
	client  *http.Client
}

func NewDownloader(dataDir string) *Downloader {
	return &Downloader{
		dataDir: dataDir,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (d *Downloader) CacheDir() string {
	return d.dataDir
}

func (d *Downloader) Download(ctx context.Context, url, mediaType string, dirName string, filename string) (string, error) {
	if url == "" || !strings.HasPrefix(url, "http") {
		return "", nil
	}

	dir := filepath.Join(d.dataDir, "images", mediaType, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating image dir: %w", err)
	}

	localPath := filepath.Join(dir, filename)

	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d downloading %s", resp.StatusCode, url)
	}

	f, err := os.Create(localPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(localPath)
		return "", err
	}

	log.Debug().Str("url", url).Str("path", localPath).Msg("downloaded image")
	return localPath, nil
}
