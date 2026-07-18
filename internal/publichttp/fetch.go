// Package publichttp provides bounded HTTP fetches for URLs that are only
// semi-trusted. Its production client rejects non-public destinations after
// DNS resolution and revalidates every redirect hop.
package publichttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/safedial"
)

const (
	// MaxImageBytes is shared by the image proxy surfaces. Fetching the whole
	// response before committing headers lets callers reject oversized images
	// instead of returning a truncated 200 response.
	MaxImageBytes int64 = 16 << 20

	imageFetchTimeout = 20 * time.Second
	maxImageFetches   = 4
)

var (
	ErrBodyTooLarge = errors.New("public HTTP response exceeds size limit")
	ErrNotImage     = errors.New("public HTTP response is not an image")

	defaultImageFetcher = NewFetcher(imageFetchTimeout)
	imageFetchSlots     = make(chan struct{}, maxImageFetches)
)

// Response is a fully read, size-bounded HTTP response. Header is cloned from
// the upstream response and is safe to retain after Get returns.
type Response struct {
	StatusCode int
	Status     string
	Header     http.Header
	Body       []byte
}

// StatusError reports a non-200 response from FetchImage.
type StatusError struct {
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("public image fetch returned HTTP %d", e.Code)
}

// Fetcher performs bounded public-network requests. NewFetcher is the
// production constructor. NewFetcherWithClient exists for hermetic tests; a
// caller-supplied client is part of the security boundary and must enforce
// equivalent post-DNS and redirect checks outside tests.
type Fetcher struct {
	client  *http.Client
	timeout time.Duration
}

func NewFetcher(timeout time.Duration) *Fetcher {
	return &Fetcher{client: safedial.NewPublicHTTPClient(), timeout: timeout}
}

func NewFetcherWithClient(client *http.Client, timeout time.Duration) *Fetcher {
	if client == nil {
		return NewFetcher(timeout)
	}
	return &Fetcher{client: client, timeout: timeout}
}

// Get validates rawURL, performs a GET, and reads at most maxBytes. It never
// returns a partial body. Non-2xx statuses are returned normally so callers
// can map upstream semantics onto their own API.
func (f *Fetcher) Get(ctx context.Context, rawURL string, maxBytes int64, headers http.Header) (*Response, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("public HTTP response limit must be positive")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil) //nolint:gosec // safedial validates and guards the destination
	if err != nil {
		return nil, fmt.Errorf("build public HTTP request: %w", err)
	}
	if err := safedial.ValidateHTTPURL(req.URL); err != nil {
		return nil, fmt.Errorf("validate public HTTP URL: %w", err)
	}
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}

	if f.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.timeout)
		defer cancel()
		req = req.WithContext(ctx)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch public HTTP URL: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("%w: declared %d bytes, limit %d", ErrBodyTooLarge, resp.ContentLength, maxBytes)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read public HTTP response: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("%w: limit %d", ErrBodyTooLarge, maxBytes)
	}
	return &Response{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     resp.Header.Clone(),
		Body:       body,
	}, nil
}

// Image is a validated, bounded remote image.
type Image struct {
	ContentType string
	Body        []byte
}

// ServeImage writes a validated Image with the security headers shared by
// every same-origin remote-image proxy.
func ServeImage(w http.ResponseWriter, request *http.Request, image *Image, cacheControl string) {
	header := w.Header()
	header.Set("Content-Type", image.ContentType)
	header.Set("Content-Length", strconv.Itoa(len(image.Body)))
	header.Set("X-Content-Type-Options", "nosniff")
	if cacheControl != "" {
		header.Set("Cache-Control", cacheControl)
	}
	w.WriteHeader(http.StatusOK)
	if request.Method != http.MethodHead {
		_, _ = w.Write(image.Body)
	}
}

// FetchImage uses the shared production fetcher and image limit.
func FetchImage(ctx context.Context, rawURL string) (*Image, error) {
	return defaultImageFetcher.FetchImage(ctx, rawURL, MaxImageBytes)
}

// FetchImage fetches and validates an image using f. The advertised media type
// is deliberately ignored: the returned type is derived from fully decoded
// raster bytes, preventing SVG or spoofed active content from being served as
// same-origin image data.
func (f *Fetcher) FetchImage(ctx context.Context, rawURL string, maxBytes int64) (*Image, error) {
	if f.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.timeout)
		defer cancel()
	}
	select {
	case imageFetchSlots <- struct{}{}:
		defer func() { <-imageFetchSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	headers := make(http.Header)
	headers.Set("Accept", "image/*")
	response, err := f.Get(ctx, rawURL, maxBytes, headers)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, &StatusError{Code: response.StatusCode}
	}
	info, err := images.ValidateRasterBytesContext(ctx, response.Body)
	if err != nil {
		if errors.Is(err, images.ErrImageTooLarge) {
			return nil, fmt.Errorf("%w: %v", ErrBodyTooLarge, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrNotImage, err)
	}
	return &Image{ContentType: info.ContentType, Body: response.Body}, nil
}
