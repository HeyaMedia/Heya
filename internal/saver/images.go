package saver

import (
	"io"
	"os"
	"path/filepath"
	"strings"

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

func SaveSeasonPoster(mediaDir string, seasonNum int, cachedPath string) error {
	seasonDir := filepath.Join(mediaDir, seasonFolderName(seasonNum))
	if _, err := os.Stat(seasonDir); os.IsNotExist(err) {
		return nil
	}

	ext := filepath.Ext(cachedPath)
	if ext == "" {
		ext = ".jpg"
	}
	destPath := filepath.Join(seasonDir, "season-poster"+ext)

	if _, err := os.Stat(destPath); err == nil {
		return nil
	}

	return copyFile(cachedPath, destPath)
}

func seasonFolderName(num int) string {
	if num < 10 {
		return "Season 0" + strings.Repeat("", 0) + string(rune('0'+num))
	}
	return "Season " + strings.Repeat("", 0) + string(rune('0'+num/10)) + string(rune('0'+num%10))
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
