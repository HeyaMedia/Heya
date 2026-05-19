package video

import (
	"regexp"
	"strings"
)

var fileExtensions = []string{
	".webm", ".m4v", ".3gp", ".nsv", ".ty", ".strm", ".rm", ".rmvb",
	".m3u", ".ifo", ".mov", ".qt", ".divx", ".xvid", ".bivx", ".nrg",
	".pva", ".wmv", ".asf", ".asx", ".ogm", ".ogv", ".m2v", ".avi",
	".bin", ".dat", ".dvr-ms", ".mpg", ".mpeg", ".mp4", ".avc", ".vp3",
	".svq3", ".nuv", ".viv", ".dv", ".fli", ".flv", ".wpl",
	".img", ".iso", ".vob",
	".mkv", ".mk3d", ".ts", ".wtv",
	".m2ts",
}

var fileExtensionExp = regexp.MustCompile(`(?i)\.[a-z0-9]{2,4}$`)

func RemoveFileExtension(title string) string {
	return fileExtensionExp.ReplaceAllStringFunc(title, func(match string) string {
		lower := strings.ToLower(match)
		for _, ext := range fileExtensions {
			if ext == lower {
				return ""
			}
		}
		return match
	})
}
