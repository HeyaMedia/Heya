package audiotags

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/mediaprobe"
)

// Tags is the normalized embedded-tag identity for one audio file. ffprobe
// surfaces ID3, MP4 atoms, and Vorbis comments with container-specific casing;
// this struct is the scanner-facing shape after that noise is removed.
type Tags struct {
	Artist           string
	AlbumArtist      string
	Album            string
	Title            string
	Year             string
	TrackNumber      int
	TrackTotal       int
	DiscNumber       int
	DiscTotal        int
	ArtistMBID       string
	AlbumArtistMBID  string
	AlbumMBID        string
	ReleaseGroupMBID string
}

func (t Tags) HasAny() bool {
	return t.Artist != "" ||
		t.AlbumArtist != "" ||
		t.Album != "" ||
		t.Title != "" ||
		t.Year != "" ||
		t.TrackNumber > 0 ||
		t.DiscNumber > 0 ||
		t.ArtistMBID != "" ||
		t.AlbumArtistMBID != "" ||
		t.AlbumMBID != "" ||
		t.ReleaseGroupMBID != ""
}

// ProbeFile runs ffprobe against a local file path and extracts embedded tags.
// Callers that need SMB support should inject the shared worker ProbeFile and
// pass its MediaInfo through FromMediaInfo instead.
func ProbeFile(ctx context.Context, path string) (*mediaprobe.MediaInfo, error) {
	//nolint:gosec // The scanner intentionally probes the configured media path.
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"-i", path,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return mediaprobe.Parse(out)
}

func FromMediaInfo(info *mediaprobe.MediaInfo) Tags {
	return Extract(Collect(info))
}

// Collect merges the primary audio stream's tags with the container-level
// tags, with container tags winning on conflict. Most formats expose music
// identity at the container level, but some only expose stream tags.
func Collect(info *mediaprobe.MediaInfo) map[string]string {
	if info == nil {
		return nil
	}
	merged := map[string]string{}
	if audio := mediaprobe.PrimaryAudio(info); audio != nil {
		for k, v := range audio.Tags {
			merged[k] = v
		}
	}
	for k, v := range info.Format.Tags {
		merged[k] = v
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// Extract pulls scanner-relevant identity fields out of raw ffprobe tags while
// tolerating common naming drift between taggers.
func Extract(raw map[string]string) Tags {
	if len(raw) == 0 {
		return Tags{}
	}
	n := normalizeKeys(raw)
	trackNum, trackTotal := parseSlashInt(first(n, "tracknumber", "track", "trck", "trackno"))
	discNum, discTotal := parseSlashInt(first(n, "discnumber", "disc", "discno", "tpos"))
	return Tags{
		Artist:           first(n, "artist", "tpe1"),
		AlbumArtist:      first(n, "album_artist", "albumartist", "album artist", "tpe2"),
		Album:            first(n, "album", "talb"),
		Title:            first(n, "title", "tit2"),
		Year:             extractYear(first(n, "originalyear", "originaldate", "date", "year", "tdrc", "tyer", "tdor")),
		TrackNumber:      trackNum,
		TrackTotal:       trackTotal,
		DiscNumber:       discNum,
		DiscTotal:        discTotal,
		ArtistMBID:       first(n, "musicbrainz_artistid", "musicbrainz artist id", "musicbrainzartistid"),
		AlbumArtistMBID:  first(n, "musicbrainz_albumartistid", "musicbrainz album artist id", "musicbrainzalbumartistid"),
		AlbumMBID:        first(n, "musicbrainz_albumid", "musicbrainz album id", "musicbrainzalbumid"),
		ReleaseGroupMBID: first(n, "musicbrainz_releasegroupid", "musicbrainz release group id", "musicbrainzreleasegroupid"),
	}
}

func normalizeKeys(raw map[string]string) map[string]string {
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.TrimSpace(v)
		if key == "" || val == "" {
			continue
		}
		if _, seen := out[key]; !seen {
			out[key] = val
		}
	}
	return out
}

func first(norm map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := norm[key]; value != "" {
			return value
		}
	}
	return ""
}

var slashIntRE = regexp.MustCompile(`^\s*(\d+)(?:\s*/\s*(\d+))?`)

func parseSlashInt(s string) (num, total int) {
	m := slashIntRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, 0
	}
	num, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		total, _ = strconv.Atoi(m[2])
	}
	return num, total
}

var yearRE = regexp.MustCompile(`(?:19|20)\d{2}`)

func extractYear(s string) string {
	return yearRE.FindString(s)
}

var placeholderValueRE = regexp.MustCompile(`(?i)^(?:unknown(?:\s+(?:artist|album|title))?|<unknown>|various(?:\s+artists)?|va|untitled|track\s*\d*|audio\s*track|no\s+(?:artist|album|title)|ripped by\b.*|.*\b(?:www\.\S+|\S+\.com)\b.*|cdda)$`)
var placeholderNameRE = regexp.MustCompile(`(?i)^(?:unknown(?:\s+(?:artist|album|title))?|<unknown>|various(?:\s+artists)?|va|untitled|no\s+(?:artist|album|title)|cdda)$`)
var numericOnlyRE = regexp.MustCompile(`^\d+$`)

func IsPlaceholderValue(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" || numericOnlyRE.MatchString(v) {
		return true
	}
	return placeholderValueRE.MatchString(v)
}

func IsPlaceholderName(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return true
	}
	return placeholderNameRE.MatchString(v)
}
