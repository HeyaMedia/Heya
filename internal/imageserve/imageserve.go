// Package imageserve provides on-the-fly image resizing with a disk cache.
//
// Callers wrap a source path on disk with Serve(). Resize parameters come from
// URL query (w, h, q, f, blur). Transformed output is written to a cache
// directory keyed by source mtime so cache invalidates automatically when the
// source changes.
// Concurrent identical requests are coalesced via singleflight.
package imageserve

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	deepwebp "github.com/deepteams/webp"
	"github.com/disintegration/imaging"
	"github.com/karbowiak/heya/internal/atomicfile"
	"golang.org/x/sync/singleflight"

	// Registers the established x/image decoder used elsewhere in Heya.
	// deepteams/webp below supplies the pure-Go encoder for transformed output.
	_ "golang.org/x/image/webp"
)

const (
	maxDimension        = 4096
	defaultQuality      = 85
	maxConcurrentResize = 2
)

type Resizer struct {
	cacheDir string
	sf       singleflight.Group
	// Bounds concurrent decode/Lanczos/encode across DIFFERENT variants
	// (singleflight only dedups identical ones). A cold grid of 40 posters
	// used to fire 40 concurrent resizes and starve the API of CPU.
	sem chan struct{}
}

func New(cacheDir string) (*Resizer, error) {
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return nil, fmt.Errorf("imageserve: mkdir %s: %w", cacheDir, err)
	}
	return &Resizer{cacheDir: cacheDir, sem: make(chan struct{}, maxConcurrentResize)}, nil
}

type Params struct {
	Width   int
	Height  int
	Quality int
	Format  string // "jpeg", "png", "webp", or "" to match source
	Blur    int    // Gaussian radius in pixels, 0 disables it
}

// active reports whether the params request any transformation.
func (p Params) active() bool {
	return p.Width > 0 || p.Height > 0 || p.Format != "" || p.Blur > 0
}

// ParseQuery reads resize parameters from URL query. Returns the parsed params
// and whether any resize-related param was present (so callers can fast-path
// the passthrough case).
func ParseQuery(q url.Values) Params {
	p := Params{Quality: defaultQuality}
	if v := q.Get("w"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= maxDimension {
			p.Width = n
		}
	}
	if v := q.Get("h"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= maxDimension {
			p.Height = n
		}
	}
	if v := q.Get("q"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 100 {
			p.Quality = n
		}
	}
	if v := q.Get("blur"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 64 {
			p.Blur = n
		}
	}
	switch q.Get("f") {
	case "jpeg", "jpg":
		p.Format = "jpeg"
	case "png":
		p.Format = "png"
	case "webp":
		p.Format = "webp"
	}
	return p
}

// Serve writes srcPath to w, applying params if any are set. Falls back to the
// original file on resize errors so a bad query never hides the asset.
func (r *Resizer) Serve(w http.ResponseWriter, req *http.Request, srcPath string, params Params) {
	if !params.active() {
		serveFile(w, req, srcPath, "")
		return
	}

	srcStat, err := os.Stat(srcPath)
	if err != nil {
		http.NotFound(w, req)
		return
	}

	cachePath, err := r.cacheKey(srcPath, srcStat, params)
	if err != nil {
		serveFile(w, req, srcPath, "")
		return
	}

	if _, err := os.Stat(cachePath); err == nil {
		serveFile(w, req, cachePath, contentTypeFor(params.Format))
		return
	}

	// Coalesce concurrent identical requests so we don't encode the same
	// variant twice under load.
	result := r.sf.DoChan(cachePath, func() (any, error) {
		if _, err := os.Stat(cachePath); err == nil {
			return nil, nil
		}
		select {
		case r.sem <- struct{}{}:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		defer func() { <-r.sem }()
		return nil, r.generate(srcPath, cachePath, params)
	})
	select {
	case completed := <-result:
		err = completed.Err
	case <-req.Context().Done():
		return
	}
	if err != nil {
		// Fall back to the source — a broken resize shouldn't 500 the UI.
		serveFile(w, req, srcPath, "")
		return
	}
	serveFile(w, req, cachePath, contentTypeFor(params.Format))
}

func (r *Resizer) cacheKey(srcPath string, st os.FileInfo, p Params) (string, error) {
	abs, err := filepath.Abs(srcPath)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	// sha256 with these inputs is purely a cache-key hash, not a security
	// primitive — collisions just cost an extra resize, not a security
	// breach. Errors from Fprintf can't happen with a hasher Writer.
	_, _ = fmt.Fprintf(h, "%s|%d|%d|%d|%d|%d|%d|%s",
		abs, st.ModTime().UnixNano(), st.Size(),
		p.Width, p.Height, p.Quality, p.Blur, p.Format,
	)
	sum := hex.EncodeToString(h.Sum(nil))
	ext := p.Format
	if ext == "" {
		ext = sourceExt(srcPath)
	}
	if ext == "" {
		ext = "bin"
	}
	// Shard by first two chars so single directories don't grow unbounded on
	// large libraries.
	return filepath.Join(r.cacheDir, sum[:2], sum+"."+ext), nil
}

func (r *Resizer) generate(srcPath, cachePath string, p Params) error {
	src, err := imaging.Open(srcPath, imaging.AutoOrientation(true))
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}

	out := resize(src, p)
	if p.Blur > 0 {
		// Ambient backdrops are deliberately soft. Baking the Gaussian into the
		// cached derivative avoids a large live CSS filter over the viewport on
		// every transition frame.
		out = imaging.Blur(out, float64(p.Blur))
	}

	if err := os.MkdirAll(filepath.Dir(cachePath), 0o750); err != nil {
		return err
	}
	format := p.Format
	if format == "" {
		format = sourceExt(srcPath)
	}
	return atomicfile.Write(cachePath, 0o640, func(writer io.Writer) error {
		switch format {
		case "png":
			err = png.Encode(writer, out)
		case "webp":
			opts := deepwebp.OptionsForPreset(deepwebp.PresetPhoto, float32(p.Quality))
			// Ambient derivatives are generated once and disk-cached. Method 4
			// spends a little more time on that cold request to reduce every
			// subsequent network transfer without introducing CGO/libwebp.
			opts.Method = 4
			err = deepwebp.Encode(writer, out, opts)
		default: // jpeg
			err = jpeg.Encode(writer, out, &jpeg.Options{Quality: p.Quality})
		}
		if err != nil {
			return fmt.Errorf("encode: %w", err)
		}
		return nil
	})
}

func resize(src image.Image, p Params) image.Image {
	if p.Width == 0 && p.Height == 0 {
		return src
	}
	if p.Width > 0 && p.Height > 0 {
		// Fit-inside while preserving aspect ratio.
		return imaging.Fit(src, p.Width, p.Height, imaging.Lanczos)
	}
	// Single-axis resize; the other auto-scales to preserve aspect.
	return imaging.Resize(src, p.Width, p.Height, imaging.Lanczos)
}

// sourceExt returns the output format for a source when no explicit f= is
// requested. Canonical HeyaMetadata URLs are opaque UUIDs, so their local
// cache filenames commonly end in the fallback .jpg even when the bytes are
// PNG or WebP. Inspect the bytes to preserve transparency for logos and
// clearart and to avoid needlessly transcoding existing WebP sources.
func sourceExt(path string) string {
	file, err := os.Open(path) //nolint:gosec // path is a resolved internal cache file
	if err == nil {
		defer func() { _ = file.Close() }()
		if _, format, decodeErr := image.DecodeConfig(file); decodeErr == nil {
			switch format {
			case "png", "webp":
				return format
			}
		}
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "png"
	case ".webp":
		return "webp"
	}
	return "jpeg"
}

func contentTypeFor(format string) string {
	switch format {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	}
	return "" // let http.ServeContent sniff
}

func serveFile(w http.ResponseWriter, r *http.Request, path, contentType string) {
	// path is the resolved cache path inside our own cacheDir.
	f, err := os.Open(path) //nolint:gosec // cache-internal path
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=604800, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if contentType == "" {
		contentType = ContentTypeForPath(path)
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), f)
}

// ContentTypeForPath returns a fixed, non-sniffed MIME for supported raster
// extensions and a safe binary fallback for everything else.
func ContentTypeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}
