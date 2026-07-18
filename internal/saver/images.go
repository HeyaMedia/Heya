package saver

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/karbowiak/heya/internal/atomicfile"
	"github.com/karbowiak/heya/internal/images"
	"github.com/rs/zerolog/log"
)

var assetTypeToFilename = map[string]string{
	"poster":   "poster",
	"backdrop": "fanart",
	"banner":   "banner",
	"art":      "clearart",
	"logo":     "logo",
	"thumb":    "landscape",
	"disc":     "disc",
}

// albumCoverSidecars mirrors the scanner's recognized cover filenames — when
// any of these already sits in the release directory, the folder owns its
// art and the export must not add a duplicate.
var albumCoverSidecars = []string{"cover.jpg", "cover.png", "cover.jpeg", "folder.jpg", "folder.png", "folder.jpeg"}

var backdropSidecarName = regexp.MustCompile(`^(fanart|backdrop)[0-9]*\.(jpg|jpeg|png|webp|gif)$`)

// SaveAlbumCoverToDir copies a cached album cover into the release directory
// as cover.<ext>, skipping when the folder already carries recognized cover
// art (user-provided files always win).
func SaveAlbumCoverToDir(albumDir, cachedPath string) error {
	for _, name := range albumCoverSidecars {
		if _, err := os.Stat(filepath.Join(albumDir, name)); err == nil {
			return nil
		}
	}
	ext := filepath.Ext(cachedPath)
	if ext == "" {
		ext = ".jpg"
	}
	return copyFile(cachedPath, filepath.Join(albumDir, "cover"+ext))
}

func SaveImageToMediaDir(mediaDir, cachedPath, assetType string, sortOrder int) error {
	baseName, ok := assetTypeToFilename[assetType]
	if !ok {
		return nil
	}
	if sortOrder > 0 {
		baseName = fmt.Sprintf("%s%d", baseName, sortOrder)
	}

	ext := filepath.Ext(cachedPath)
	if ext == "" {
		ext = ".jpg"
	}

	destPath := filepath.Join(mediaDir, baseName+ext)

	if _, err := os.Stat(destPath); err == nil {
		return nil
	}
	if assetType == "backdrop" {
		duplicate, err := equivalentBackdropSidecarExists(mediaDir, cachedPath)
		if err == nil && duplicate {
			return nil
		}
	}

	return copyFile(cachedPath, destPath)
}

// equivalentBackdropSidecarExists prevents save_images from writing another
// fanartN/backdropN file when the folder already contains the same visual at a
// different size or JPEG quality. Existing source sidecars are never modified.
func equivalentBackdropSidecarExists(mediaDir, candidatePath string) (bool, error) {
	candidate, err := images.FingerprintFile(candidatePath)
	if err != nil {
		return false, err
	}
	entries, err := os.ReadDir(mediaDir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !backdropSidecarName.MatchString(strings.ToLower(entry.Name())) {
			continue
		}
		existing, fingerprintErr := images.FingerprintFile(filepath.Join(mediaDir, entry.Name()))
		if fingerprintErr == nil && images.VisuallyEquivalent(candidate, existing) {
			return true, nil
		}
	}
	return false, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // source is a database-backed media asset selected by the scanner
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	// dst is composed from the scanner-selected media directory and a fixed
	// sidecar basename; cachedPath contributes only a file extension.
	if _, err := atomicfile.Copy(dst, 0o644, in); err != nil {
		return err
	}

	log.Debug().Str("src", filepath.Base(src)).Str("dst", dst).Msg("image saved to media dir")
	return nil
}
