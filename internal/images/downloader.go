package images

import (
	"context"
	"errors"
	"fmt"
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

const (
	maxAcceptedImagePolls = 60
	maxAcceptedImageWait  = 2 * time.Minute
	maxConcurrentFetches  = 8
)

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
	trusted       map[string]trustedSource
	fetchSlots    chan struct{}
}

type TrustedSource struct {
	BaseURL           string
	BearerToken       string
	ImageVariantWidth int
}

type trustedSource struct {
	basePath          string
	headers           http.Header
	imageVariantWidth int
}

func NewDownloader(dataDir string, trustedSources ...TrustedSource) *Downloader {
	// Raise the per-host connection pool (stock is 2): on-demand image serving
	// fetches artwork on first view, so a fresh library grid can burst dozens of
	// concurrent downloads from the same CDN host — reuse warm connections
	// instead of paying a TLS handshake each time.
	// SSRF guard: the Downloader now fetches DB-stored (semi-trusted, possibly
	// NFO-sourced) URLs synchronously from the anonymous /api/media/*/image and
	// /api/person/*/image endpoints. The canonical public client rejects
	// non-public dial targets post-DNS, disables environment proxies, and
	// revalidates every redirect hop.
	publicClient := safedial.NewPublicHTTPClientWithOptions(safedial.PublicHTTPClientOptions{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 16,
	})
	publicClient.Timeout = 30 * time.Second
	trustedTransport := http.DefaultTransport.(*http.Transport).Clone()
	trustedTransport.Proxy = nil
	trustedTransport.MaxIdleConns = 100
	trustedTransport.MaxIdleConnsPerHost = 16
	trusted := make(map[string]trustedSource)
	for _, source := range trustedSources {
		parsed, err := url.Parse(source.BaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			continue
		}
		header := make(http.Header)
		if token := strings.TrimSpace(source.BearerToken); token != "" {
			header.Set("Authorization", "Bearer "+token)
		}
		trusted[parsed.Scheme+"://"+parsed.Host] = trustedSource{
			basePath:          strings.TrimRight(parsed.Path, "/"),
			headers:           header,
			imageVariantWidth: max(0, source.ImageVariantWidth),
		}
	}
	trustedClient := &http.Client{
		Timeout: 30 * time.Second, Transport: trustedTransport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			origin := request.URL.Scheme + "://" + request.URL.Host
			if _, ok := trusted[origin]; !ok {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	return &Downloader{
		dataDir:       dataDir,
		client:        publicClient,
		trustedClient: trustedClient,
		trusted:       trusted,
		fetchSlots:    make(chan struct{}, maxConcurrentFetches),
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
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("creating image dir: %w", err)
	}

	filename = filepath.Base(filename)
	if filename == "" || filename == "." {
		return "", errors.New("invalid image cache filename")
	}
	candidates := cacheImageCandidates(dir, filename)

	if !replace {
		if candidate := newestValidCacheImage(ctx, candidates); candidate != "" {
			return candidate, nil
		}
		// A cache entry created by an older version may be truncated or may not
		// be an image at all. Keep it in place until a valid replacement has
		// been completely fetched and decoded below.
	}

	select {
	case d.fetchSlots <- struct{}{}:
		defer func() { <-d.fetchSlots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	// Another request may have populated this key while this one waited for a
	// bounded network slot.
	if !replace {
		if candidate := newestValidCacheImage(ctx, candidates); candidate != "" {
			return candidate, nil
		}
	}

	url = d.boundedImageURL(url)
	client, headers := d.clientForURL(url)
	pollCtx, cancelPoll := context.WithTimeout(ctx, maxAcceptedImageWait)
	defer cancelPoll()
	var resp *http.Response
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(pollCtx, http.MethodGet, url, nil)
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
		if attempt+1 >= maxAcceptedImagePolls {
			return "", &StatusError{Code: http.StatusAccepted, URL: url}
		}
		if err := waitForImage(pollCtx, imageRetryAfter(resp.Header.Get("Retry-After"))); err != nil {
			return "", &StatusError{Code: http.StatusAccepted, URL: url}
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", &StatusError{Code: resp.StatusCode, URL: url}
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(strings.ToLower(ct), "image/") {
		return "", fmt.Errorf("unexpected image content type %q downloading %s", ct, url)
	}

	staged, err := StageRasterContext(pollCtx, dir, resp.Body)
	if err != nil {
		return "", fmt.Errorf("validate downloaded image %s: %w", url, err)
	}
	defer func() { _ = staged.Rollback() }()
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	if stem == "" {
		stem = "image"
	}
	localPath := filepath.Join(dir, stem+staged.Info.Extension)
	if err := staged.Publish(localPath); err != nil {
		return "", err
	}
	if err := staged.Commit(); err != nil {
		return "", err
	}
	// Alternate-extension entries are intentionally retained here. Callers
	// persist the returned path in a separate DB transaction; deleting an older
	// path before that transaction commits could break its still-live row. The
	// DB-aware materialization/reconciliation path owns orphan cleanup.

	log.Debug().Str("url", url).Str("path", localPath).
		Str("format", staged.Info.Format).Int("width", staged.Info.Width).Int("height", staged.Info.Height).
		Msg("downloaded image")
	return localPath, nil
}

func newestValidCacheImage(ctx context.Context, candidates []string) string {
	newest := ""
	var newestModTime time.Time
	for _, candidate := range candidates {
		stat, err := os.Stat(candidate)
		if err != nil || !stat.Mode().IsRegular() {
			continue
		}
		if _, err := ValidateRasterFileContext(ctx, candidate); err != nil {
			continue
		}
		if newest == "" || stat.ModTime().After(newestModTime) {
			newest = candidate
			newestModTime = stat.ModTime()
		}
	}
	return newest
}

func cacheImageCandidates(dir, filename string) []string {
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	if stem == "" {
		stem = "image"
	}
	result := make([]string, 0, 5)
	seen := make(map[string]struct{}, 5)
	add := func(path string) {
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	add(filepath.Join(dir, filename))
	for _, extension := range []string{".jpg", ".png", ".webp"} {
		add(filepath.Join(dir, stem+extension))
	}
	return result
}

func (d *Downloader) clientForURL(rawURL string) (*http.Client, http.Header) {
	parsed, err := url.Parse(rawURL)
	if err == nil {
		if source, ok := d.trusted[parsed.Scheme+"://"+parsed.Host]; ok {
			return d.trustedClient, source.headers
		}
	}
	return d.client, nil
}

// boundedImageURL upgrades a canonical HeyaMetadata original to its configured
// WebP rendition. The rewrite is deliberately restricted to an exact image
// route on a trusted origin; already-bounded variants and unrelated trusted
// endpoints retain their original URLs.
func (d *Downloader) boundedImageURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	source, ok := d.trusted[parsed.Scheme+"://"+parsed.Host]
	if !ok || source.imageVariantWidth < 1 || parsed.RawQuery != "" {
		return rawURL
	}
	prefix := source.basePath + "/api/v2/images/"
	imageID := strings.TrimPrefix(parsed.Path, prefix)
	if imageID == parsed.Path || imageID == "" || strings.Contains(imageID, "/") {
		return rawURL
	}
	parsed.Path = prefix + imageID + "/variants/webp/" + strconv.Itoa(source.imageVariantWidth)
	parsed.RawPath = ""
	parsed.Fragment = ""
	return parsed.String()
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
