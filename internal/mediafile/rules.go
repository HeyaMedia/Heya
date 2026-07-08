package mediafile

import (
	"path/filepath"
	"strings"
)

var junkFiles = map[string]bool{
	".DS_Store": true, "Thumbs.db": true, "desktop.ini": true,
	"theme.mp3": true, "theme.flac": true, "theme.ogg": true,
}

var skipDirs = map[string]bool{
	"@eaDir": true, "#recycle": true, ".Trash": true, "lost+found": true,
}

var extrasDirs = map[string]string{
	"behindthescenes": "behindthescenes",
	"deletedscenes":   "deleted",
	"featurette":      "featurette",
	"featurettes":     "featurette",
	"interview":       "interview",
	"interviews":      "interview",
	"other":           "other",
	"sample":          "sample",
	"samples":         "sample",
	"scene":           "scene",
	"scenes":          "scene",
	"short":           "short",
	"shorts":          "short",
	"trailer":         "trailer",
	"trailers":        "trailer",
}

var extrasSuffixes = map[string]string{
	"behindthescenes": "behindthescenes",
	"deleted":         "deleted",
	"featurette":      "featurette",
	"interview":       "interview",
	"other":           "other",
	"sample":          "sample",
	"scene":           "scene",
	"short":           "short",
	"trailer":         "trailer",
}

var imageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}
var subtitleExts = map[string]bool{".srt": true, ".ass": true, ".ssa": true, ".sub": true, ".vtt": true}
var videoExts = map[string]bool{".mkv": true, ".mp4": true, ".avi": true, ".mov": true, ".m4v": true, ".wmv": true, ".webm": true, ".ts": true, ".mpg": true, ".mpeg": true}
var audioExts = map[string]bool{".flac": true, ".mp3": true, ".m4a": true, ".aac": true, ".wav": true, ".ogg": true, ".oga": true, ".opus": true, ".wma": true, ".alac": true, ".aiff": true, ".aif": true}
var lyricsExts = map[string]bool{".lrc": true}

func IsJunkFile(name string) bool { return junkFiles[name] }

func IsSkipDir(name string) bool {
	return skipDirs[name] || strings.HasSuffix(strings.ToLower(name), ".trickplay")
}

func IsExtrasDir(name string) bool { return ExtraTypeFromDir(name) != "" }

func ExtraTypeFromDir(name string) string {
	return extrasDirs[extraKey(name)]
}

func ExtraTypeFromPath(path string) string {
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if extraType := ExtraTypeFromDir(part); extraType != "" {
			return extraType
		}
	}
	return ExtraTypeFromFilename(filepath.Base(path))
}

func ExtraTypeFromFilename(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if extraKey(base) == "sample" {
		return "sample"
	}
	lower := strings.ToLower(base)
	for suffix, extraType := range extrasSuffixes {
		if strings.HasSuffix(lower, "-"+suffix) {
			return extraType
		}
	}
	return ""
}

func IsImageExt(ext string) bool { return imageExts[strings.ToLower(ext)] }

func IsSubtitleExt(ext string) bool { return subtitleExts[strings.ToLower(ext)] }

func IsVideoExt(ext string) bool { return videoExts[strings.ToLower(ext)] }

func IsAudioExt(ext string) bool { return audioExts[strings.ToLower(ext)] }

func IsLyricsExt(ext string) bool { return lyricsExts[strings.ToLower(ext)] }

func IsProbeable(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return IsVideoExt(ext) || IsAudioExt(ext)
}

func extraKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(value)
	return value
}
