// Package imageserve provides on-the-fly image resizing with a disk cache.
//
// Callers wrap a source path on disk with Serve(). Resize parameters come from
// URL query (w, h, q, f). Resized output is written to a cache directory keyed
// by source mtime so cache invalidates automatically when the source changes.
// Concurrent identical requests are coalesced via singleflight.
package imageserve

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"github.com/disintegration/imaging"
	"golang.org/x/sync/singleflight"

	// Decode-only WebP support — registers a decoder with the stdlib image
	// package so imaging.Open() can read .webp posters. We don't have a
	// pure-Go WebP encoder, so cached output is JPEG either way.
	_ "golang.org/x/image/webp"
)

const (
	maxDimension   = 4096
	defaultQuality = 85
)

type Resizer struct {
	cacheDir string
	sf       singleflight.Group
}

func New(cacheDir string) (*Resizer, error) {
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return nil, fmt.Errorf("imageserve: mkdir %s: %w", cacheDir, err)
	}
	return &Resizer{cacheDir: cacheDir}, nil
}

type Params struct {
	Width   int
	Height  int
	Quality int
	Format  string // "jpeg", "png", or "" to match source
}

// active reports whether the params request any transformation.
func (p Params) active() bool {
	return p.Width > 0 || p.Height > 0 || p.Format != ""
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
	switch q.Get("f") {
	case "jpeg", "jpg":
		p.Format = "jpeg"
	case "png":
		p.Format = "png"
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
	_, err, _ = r.sf.Do(cachePath, func() (any, error) {
		if _, err := os.Stat(cachePath); err == nil {
			return nil, nil
		}
		return nil, r.generate(srcPath, cachePath, params)
	})
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
	_, _ = fmt.Fprintf(h, "%s|%d|%d|%d|%d|%d|%s",
		abs, st.ModTime().UnixNano(), st.Size(),
		p.Width, p.Height, p.Quality, p.Format,
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

	if err := os.MkdirAll(filepath.Dir(cachePath), 0o750); err != nil {
		return err
	}
	// Write to a temp file then rename so partial writes can't be served.
	// tmp path is constructed from cachePath inside our own cacheDir, so
	// the "potential file inclusion via variable" warning is a false
	// positive — there's no user-controlled path here.
	tmp := cachePath + ".tmp" //nolint:gosec // path is internal cache key
	f, err := os.Create(tmp)  //nolint:gosec
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}()

	format := p.Format
	if format == "" {
		format = sourceExt(srcPath)
	}
	switch format {
	case "png":
		err = png.Encode(f, out)
	default: // jpeg
		err = jpeg.Encode(f, out, &jpeg.Options{Quality: p.Quality})
	}
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, cachePath)
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
// PNG. Inspect the bytes to preserve transparency for logos and clearart.
// WebP/GIF remain decode-only and transcode to JPEG.
func sourceExt(path string) string {
	file, err := os.Open(path) //nolint:gosec // path is a resolved internal cache file
	if err == nil {
		defer func() { _ = file.Close() }()
		if _, format, decodeErr := image.DecodeConfig(file); decodeErr == nil && format == "png" {
			return "png"
		}
	}
	if filepath.Ext(path) == ".png" {
		return "png"
	}
	return "jpeg"
}

func contentTypeFor(format string) string {
	switch format {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
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
	stat, _ := f.Stat()
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), f)
}
