package studios

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
)

const (
	repoBaseURL = "https://raw.githubusercontent.com/jellyfin/jellyfin-ux/master/branding/SVG"
	fallbackURL = "https://raw.githubusercontent.com/JamsRepos/Jellyfin-Studio-Images/main/Studios"
)

type Resolver struct {
	studioDir string
}

func NewResolver(dataDir string) *Resolver {
	dir := filepath.Join(dataDir, "studios")
	os.MkdirAll(dir, 0o755)
	return &Resolver{studioDir: dir}
}

func (r *Resolver) HasLogo(name string) bool {
	slug := Slugify(name)
	for _, ext := range []string{".svg", ".png", ".jpg", ".webp"} {
		if _, err := os.Stat(filepath.Join(r.studioDir, slug+ext)); err == nil {
			return true
		}
	}
	return false
}

func (r *Resolver) LogoPath(name string) string {
	slug := Slugify(name)
	for _, ext := range []string{".svg", ".png", ".jpg", ".webp"} {
		path := filepath.Join(r.studioDir, slug+ext)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func (r *Resolver) Dir() string {
	return r.studioDir
}

func (r *Resolver) Sync(ctx context.Context, names []string) (downloaded, skipped int, err error) {
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
		{fmt.Sprintf("%s/%s.png", fallbackURL, originalName), ".png"},
		{fmt.Sprintf("%s/%s.png", fallbackURL, slug), ".png"},
		{fmt.Sprintf("%s/%s.svg", repoBaseURL, slug), ".svg"},
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	f, err := os.Create(dest)
	if err != nil {
		return false
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		os.Remove(dest)
		return false
	}
	return true
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

func Slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, "&", "and")
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
