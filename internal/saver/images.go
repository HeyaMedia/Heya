package saver

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

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

	return copyFile(cachedPath, destPath)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		_ = os.Remove(dst)
		return err
	}

	log.Debug().Str("src", filepath.Base(src)).Str("dst", dst).Msg("image saved to media dir")
	return nil
}
