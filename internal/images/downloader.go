package images

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/safedial"
	"github.com/rs/zerolog/log"
)

const maxImageBytes = 25 << 20

// StatusError is returned by Download when the server answers with a non-200
// status. Permanent reports whether a retry is pointless: a 4xx (other than
// 408 Request Timeout and 429 Too Many Requests) means the image simply isn't
// available upstream. That's the common, expected case for episode stills and
// some person headshots — a provider may have no materializable image —
// so callers swallow it instead of retrying and spamming the logs.
type StatusError struct {
	Code int
	URL  string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("HTTP %d downloading %s", e.Code, e.URL)
}

func (e *StatusError) Permanent() bool {
	return e.Code >= 400 && e.Code < 500 &&
		e.Code != http.StatusRequestTimeout &&
		e.Code != http.StatusTooManyRequests
}

type Downloader struct {
	dataDir       string
	client        *http.Client
	trustedClient *http.Client
	trusted       map[string]http.Header
}

type TrustedSource struct {
	BaseURL     string
	BearerToken string
}

func NewDownloader(dataDir string, trustedSources ...TrustedSource) *Downloader {
	// Raise the per-host connection pool (stock is 2): on-demand image serving
	// fetches artwork on first view, so a fresh library grid can burst dozens of
	// concurrent downloads from the same CDN host — reuse warm connections
	// instead of paying a TLS handshake each time.
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 16
	// SSRF guard: the Downloader now fetches DB-stored (semi-trusted, possibly
	// NFO-sourced) URLs synchronously from the anonymous /api/media/*/image and
	// /api/person/*/image endpoints. Reject non-public dial targets post-DNS and
	// disable Proxy so an HTTP_PROXY can't tunnel past the guard.
	t.Proxy = nil
	t.DialContext = (&net.Dialer{Timeout: 10 * time.Second, Control: safedial.Control}).DialContext
	trustedTransport := http.DefaultTransport.(*http.Transport).Clone()
	trustedTransport.Proxy = nil
	trustedTransport.MaxIdleConns = 100
	trustedTransport.MaxIdleConnsPerHost = 16
	trusted := make(map[string]http.Header)
	for _, source := range trustedSources {
		parsed, err := url.Parse(source.BaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			continue
		}
		header := make(http.Header)
		if token := strings.TrimSpace(source.BearerToken); token != "" {
			header.Set("Authorization", "Bearer "+token)
		}
		trusted[parsed.Scheme+"://"+parsed.Host] = header
	}
	trustedClient := &http.Client{
		Timeout: 30 * time.Second, Transport: trustedTransport,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			origin := request.URL.Scheme + "://" + request.URL.Host
			if _, ok := trusted[origin]; !ok {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	return &Downloader{
		dataDir:       dataDir,
		client:        &http.Client{Timeout: 30 * time.Second, Transport: t},
		trustedClient: trustedClient,
		trusted:       trusted,
	}
}

func (d *Downloader) CacheDir() string {
	return d.dataDir
}

func (d *Downloader) Download(ctx context.Context, url, mediaType string, dirName string, filename string) (string, error) {
	return d.download(ctx, url, mediaType, dirName, filename, false)
}

// DownloadFresh atomically replaces an existing cache entry. Metadata-editor
// selections deliberately keep stable public routes (poster, logo, etc.), so
// an existing filename must not be mistaken for proof that the newly selected
// canonical URL has already been downloaded.
func (d *Downloader) DownloadFresh(ctx context.Context, url, mediaType string, dirName string, filename string) (string, error) {
	return d.download(ctx, url, mediaType, dirName, filename, true)
}

func (d *Downloader) download(ctx context.Context, url, mediaType string, dirName string, filename string, replace bool) (string, error) {
	if url == "" || !strings.HasPrefix(url, "http") {
		return "", nil
	}

	dir := filepath.Join(d.dataDir, "images", mediaType, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating image dir: %w", err)
	}

	localPath := filepath.Join(dir, filename)

	if _, err := os.Stat(localPath); err == nil && !replace {
		return localPath, nil
	}

	client, headers := d.clientForURL(url)
	var resp *http.Response
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		for name, values := range headers {
			for _, value := range values {
				req.Header.Add(name, value)
			}
		}
		resp, err = client.Do(req)
		if err != nil {
			return "", err
		}
		if resp.StatusCode != http.StatusAccepted {
			break
		}
		_ = resp.Body.Close()
		if err := waitForImage(ctx, imageRetryAfter(resp.Header.Get("Retry-After"))); err != nil {
			return "", &StatusError{Code: http.StatusAccepted, URL: url}
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", &StatusError{Code: resp.StatusCode, URL: url}
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(strings.ToLower(ct), "image/") {
		return "", fmt.Errorf("unexpected image content type %q downloading %s", ct, url)
	}

	tmp, err := os.CreateTemp(dir, "."+filename+"-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	n, err := io.Copy(tmp, io.LimitReader(resp.Body, maxImageBytes+1))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if n > maxImageBytes {
		return "", fmt.Errorf("image exceeds %d bytes: %s", maxImageBytes, url)
	}
	if err := os.Rename(tmpPath, localPath); err != nil {
		return "", err
	}

	log.Debug().Str("url", url).Str("path", localPath).Msg("downloaded image")
	return localPath, nil
}

func (d *Downloader) clientForURL(rawURL string) (*http.Client, http.Header) {
	parsed, err := url.Parse(rawURL)
	if err == nil {
		if header, ok := d.trusted[parsed.Scheme+"://"+parsed.Host]; ok {
			return d.trustedClient, header
		}
	}
	return d.client, nil
}

func imageRetryAfter(value string) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Second
}

func waitForImage(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
