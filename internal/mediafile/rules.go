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

var extrasDirs = map[string]bool{
	"trailers": true, "trailer": true, "behind the scenes": true,
	"deleted scenes": true, "featurettes": true, "interviews": true,
	"scenes": true, "shorts": true, "other": true,
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

func IsExtrasDir(name string) bool { return extrasDirs[strings.ToLower(name)] }

func IsImageExt(ext string) bool { return imageExts[strings.ToLower(ext)] }

func IsSubtitleExt(ext string) bool { return subtitleExts[strings.ToLower(ext)] }

func IsVideoExt(ext string) bool { return videoExts[strings.ToLower(ext)] }

func IsAudioExt(ext string) bool { return audioExts[strings.ToLower(ext)] }

func IsLyricsExt(ext string) bool { return lyricsExts[strings.ToLower(ext)] }

func IsProbeable(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return IsVideoExt(ext) || IsAudioExt(ext)
}
