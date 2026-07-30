package decision

import "github.com/karbowiak/heya/internal/parser/video"

// SizeDefsVersion stamps the built-in size-definition table into policy
// snapshots; bump when the defaults change so historical decisions remain
// reproducible. The values follow the current Sonarr/Radarr shipped quality
// definitions (MB per minute of runtime); they are code defaults — not yet
// user-editable — which is a documented divergence from the live arrs when
// the user has customized theirs.
const SizeDefsVersion = 1

// VideoSizeDefs are shared by the movie and tv domains.
var VideoSizeDefs = map[string]SizeDef{
	"sdtv":         {MinMBPerMin: 2, MaxMBPerMin: 100, PreferredMBPerMin: 95},
	"dvd":          {MinMBPerMin: 2, MaxMBPerMin: 100, PreferredMBPerMin: 95},
	"webdl-480p":   {MinMBPerMin: 2, MaxMBPerMin: 100, PreferredMBPerMin: 95},
	"webrip-480p":  {MinMBPerMin: 2, MaxMBPerMin: 100, PreferredMBPerMin: 95},
	"bluray-480p":  {MinMBPerMin: 2, MaxMBPerMin: 100, PreferredMBPerMin: 95},
	"bluray-576p":  {MinMBPerMin: 2, MaxMBPerMin: 100, PreferredMBPerMin: 95},
	"hdtv-720p":    {MinMBPerMin: 3, MaxMBPerMin: 130, PreferredMBPerMin: 95},
	"webdl-720p":   {MinMBPerMin: 3, MaxMBPerMin: 130, PreferredMBPerMin: 95},
	"webrip-720p":  {MinMBPerMin: 3, MaxMBPerMin: 130, PreferredMBPerMin: 95},
	"bluray-720p":  {MinMBPerMin: 4.3, MaxMBPerMin: 130, PreferredMBPerMin: 95},
	"hdtv-1080p":   {MinMBPerMin: 4, MaxMBPerMin: 170, PreferredMBPerMin: 95},
	"webdl-1080p":  {MinMBPerMin: 4, MaxMBPerMin: 170, PreferredMBPerMin: 95},
	"webrip-1080p": {MinMBPerMin: 4, MaxMBPerMin: 170, PreferredMBPerMin: 95},
	"bluray-1080p": {MinMBPerMin: 4.3, MaxMBPerMin: 300, PreferredMBPerMin: 95},
	"remux-1080p":  {MinMBPerMin: 0, MaxMBPerMin: 0, PreferredMBPerMin: 0},
	"hdtv-2160p":   {MinMBPerMin: 4.7, MaxMBPerMin: 350, PreferredMBPerMin: 95},
	"webdl-2160p":  {MinMBPerMin: 4.7, MaxMBPerMin: 350, PreferredMBPerMin: 95},
	"webrip-2160p": {MinMBPerMin: 4.7, MaxMBPerMin: 350, PreferredMBPerMin: 95},
	"bluray-2160p": {MinMBPerMin: 4.3, MaxMBPerMin: 400, PreferredMBPerMin: 95},
	"remux-2160p":  {MinMBPerMin: 0, MaxMBPerMin: 0, PreferredMBPerMin: 0},
	// br-disk and raw-hd deliberately unbounded.
}

// releaseTitle extracts the comparable title from a release name per
// domain. Music/book identity is search-context-driven (slice 8) and falls
// back to movie-style parsing until then.
func releaseTitle(domain, name string) string {
	switch domain {
	case "tv":
		return video.FilenameParseShow(name).Title
	default:
		return video.FilenameParseMovie(name).Title
	}
}
