package server

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"
)

type LyricsLine struct {
	TimeMs int    `json:"time_ms"`
	Text   string `json:"text"`
}

type LyricsResponse struct {
	Synced bool         `json:"synced"`
	Lines  []LyricsLine `json:"lines"`
}

// LRC time codes: [mm:ss.cc] or [mm:ss.ccc] or [mm:ss], optionally repeated
// on a single line for multi-time karaoke entries. Also tolerates [hh:mm:ss].
var (
	reLRCTime = regexp.MustCompile(`\[(\d{1,2}):(\d{2}(?:[.:]\d{1,3})?)\]`)
	// Tag lines like [ti:Title] [ar:Artist] [length:03:21] are metadata,
	// not lyrics — recognise the prefix shape and drop them.
	reLRCTag = regexp.MustCompile(`^\[[a-zA-Z_]+:.+\]$`)
)

func parseLyrics(body []byte) LyricsResponse {
	resp := LyricsResponse{Lines: []LyricsLine{}}
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	anySynced := false

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		if reLRCTag.MatchString(strings.TrimSpace(line)) {
			continue
		}
		matches := reLRCTime.FindAllStringSubmatchIndex(line, -1)
		if len(matches) == 0 {
			// Plain-text line. Keep as unsynced entry; total file gets
			// synced=false unless we also see at least one timed line.
			resp.Lines = append(resp.Lines, LyricsLine{TimeMs: -1, Text: line})
			continue
		}
		// Strip all timing tags to get the lyric text; emit one entry per
		// timing tag with the shared text.
		text := strings.TrimSpace(reLRCTime.ReplaceAllString(line, ""))
		anySynced = true
		for _, m := range matches {
			mins, _ := strconv.Atoi(line[m[2]:m[3]])
			secStr := line[m[4]:m[5]]
			secs, hundredths := parseSecondsHundredths(secStr)
			ms := mins*60_000 + secs*1000 + hundredths*10
			resp.Lines = append(resp.Lines, LyricsLine{TimeMs: ms, Text: text})
		}
	}

	resp.Synced = anySynced
	// Sort synced entries chronologically — multi-tag LRC files can have
	// repeated tags out of order, and we want a single forward timeline.
	if anySynced {
		sortLyricsByTime(resp.Lines)
	}
	return resp
}

// parseSecondsHundredths handles "ss" / "ss.cc" / "ss.ccc" / "ss:cc" forms.
func parseSecondsHundredths(s string) (int, int) {
	// Normalise the rare ss:cc separator to ss.cc.
	s = strings.Replace(s, ":", ".", 1)
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		secs, _ := strconv.Atoi(s[:dot])
		frac := s[dot+1:]
		if len(frac) > 2 {
			frac = frac[:2]
		}
		// Pad short fractions so "5" becomes 50 hundredths (=0.50s).
		for len(frac) < 2 {
			frac += "0"
		}
		hundredths, _ := strconv.Atoi(frac)
		return secs, hundredths
	}
	secs, _ := strconv.Atoi(s)
	return secs, 0
}

func sortLyricsByTime(lines []LyricsLine) {
	// Insertion sort — lyrics are tiny (a few hundred lines at most).
	for i := 1; i < len(lines); i++ {
		j := i
		for j > 0 && lines[j-1].TimeMs > lines[j].TimeMs {
			lines[j-1], lines[j] = lines[j], lines[j-1]
			j--
		}
	}
}
