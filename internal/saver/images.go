package saver

import (
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

func SaveImageToMediaDir(mediaDir, cachedPath, assetType string) error {
	baseName, ok := assetTypeToFilename[assetType]
	if !ok {
		return nil
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
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		os.Remove(dst)
		return err
	}

	log.Debug().Str("src", filepath.Base(src)).Str("dst", dst).Msg("image saved to media dir")
	return nil
}
