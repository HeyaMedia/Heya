package studios

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/images"
	"github.com/rs/zerolog/log"
)

const (
	fallbackURL = "https://raw.githubusercontent.com/JamsRepos/Jellyfin-Studio-Images/main/Studios"
)

type Resolver struct {
	studioDir string
	initErr   error
}

func NewResolver(dataDir string) *Resolver {
	dir := filepath.Join(dataDir, "studios")
	return &Resolver{
		studioDir: dir,
		initErr:   os.MkdirAll(dir, 0o750),
	}
}

func (r *Resolver) HasLogo(name string) bool {
	slug := Slugify(name)
	for _, ext := range []string{".png", ".jpg", ".gif", ".webp"} {
		if _, err := os.Stat(filepath.Join(r.studioDir, slug+ext)); err == nil {
			return true
		}
	}
	return false
}

func (r *Resolver) LogoPath(name string) string {
	slug := Slugify(name)
	for _, ext := range []string{".png", ".jpg", ".gif", ".webp"} {
		path := filepath.Join(r.studioDir, slug+ext)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func (r *Resolver) Sync(ctx context.Context, names []string) (downloaded, skipped int, err error) {
	if r.initErr != nil {
		return 0, 0, fmt.Errorf("create studio logo directory: %w", r.initErr)
	}

	for _, name := range names {
		if ctx.Err() != nil {
			return downloaded, skipped, ctx.Err()
		}

		slug := Slugify(name)
		if r.HasLogo(name) {
			skipped++
			continue
		}

		if tryDownload(ctx, r.studioDir, slug, name) {
			downloaded++
		} else {
			skipped++
		}
	}
	return downloaded, skipped, nil
}

func tryDownload(ctx context.Context, dir, slug, originalName string) bool {
	sources := []struct {
		url string
		ext string
	}{
		{fmt.Sprintf("%s/%s.png", fallbackURL, url.PathEscape(originalName)), ".png"},
		{fmt.Sprintf("%s/%s.png", fallbackURL, slug), ".png"},
	}

	for _, src := range sources {
		if downloadFile(ctx, src.url, filepath.Join(dir, slug+src.ext)) {
			log.Debug().Str("studio", originalName).Str("slug", slug).Msg("studio logo downloaded")
			return true
		}
	}
	return false
}

func downloadFile(ctx context.Context, url, dest string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "Heya/1.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	dir := filepath.Dir(dest)
	staged, err := images.StageRasterContext(ctx, dir, resp.Body)
	if err != nil {
		return false
	}
	defer func() { _ = staged.Rollback() }()
	stem := strings.TrimSuffix(filepath.Base(dest), filepath.Ext(dest))
	dest = filepath.Join(dir, stem+staged.Info.Extension)
	if err := staged.Publish(dest); err != nil {
		return false
	}
	return staged.Commit() == nil
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify intentionally differs from internal/slug (it maps "&" → "and"
// where slug.Generate does not). The slugs key the on-disk studio logo
// filenames, so the mapping must stay byte-stable across releases — do NOT
// replace this with slug.Generate or existing logos stop resolving.
func Slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, "&", "and")
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
