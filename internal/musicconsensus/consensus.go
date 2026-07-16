// Package musicconsensus resolves release-level identity from the embedded
// tags of sibling audio files. It deliberately knows nothing about paths or
// sidecars: callers decide how a strong folder consensus participates in their
// own source-precedence rules.
package musicconsensus

import (
	"strings"

	"github.com/karbowiak/heya/internal/titlematch"
)

const ThresholdPercent = 80

// Evidence contains the release-level tag fields from one audio file.
type Evidence struct {
	Artist string
	Album  string
	Year   string
}

// Field is the winning value for one release-level field. Usable excludes
// empty/placeholder values (callers omit placeholders before calling Build).
// Missing is reported separately so a strong winner can safely fill those
// files without treating absence as disagreement.
type Field struct {
	Value   string
	Support int
	Usable  int
	Missing int
	Strong  bool
}

// Release is the independently-computed consensus for a single release
// directory. Callers must never combine evidence from different directories.
type Release struct {
	Artist Field
	Album  Field
	Year   Field
}

// Build selects a value when at least 80% of the usable sibling tags agree.
// Two supporting files are always required; this prevents either one lone tag
// in an otherwise untagged album or a single-file release from bypassing the
// normal path/tag fusion rules.
func Build(evidence []Evidence) Release {
	artists := make([]string, 0, len(evidence))
	albums := make([]string, 0, len(evidence))
	years := make([]string, 0, len(evidence))
	for _, item := range evidence {
		artists = append(artists, item.Artist)
		albums = append(albums, item.Album)
		years = append(years, item.Year)
	}
	return Release{
		Artist: selectField(artists, equivalentText),
		Album:  selectField(albums, equivalentText),
		Year:   selectField(years, equivalentExact),
	}
}

// Matches reports whether value belongs to the winning cluster. Empty values
// never match; callers can therefore distinguish a missing tag (inherit) from
// a contradictory tag (quarantine its hard identifiers).
func (f Field) Matches(value string) bool {
	if strings.TrimSpace(f.Value) == "" || strings.TrimSpace(value) == "" {
		return false
	}
	return equivalentText(f.Value, value)
}

type bucket struct {
	value string
	count int
}

func selectField(values []string, equivalent func(string, string) bool) Field {
	var out Field
	buckets := make([]bucket, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			out.Missing++
			continue
		}
		out.Usable++
		matched := false
		for i := range buckets {
			if equivalent(buckets[i].value, value) {
				buckets[i].count++
				matched = true
				break
			}
		}
		if !matched {
			buckets = append(buckets, bucket{value: value, count: 1})
		}
	}
	for _, candidate := range buckets {
		if candidate.count > out.Support {
			out.Value = candidate.value
			out.Support = candidate.count
		}
	}
	out.Strong = out.Support >= 2 &&
		out.Usable > 0 &&
		out.Support*100 >= ThresholdPercent*out.Usable
	return out
}

func equivalentText(a, b string) bool {
	aKeys := titlematch.Normalizations(strings.TrimSpace(a))
	bKeys := titlematch.Normalizations(strings.TrimSpace(b))
	for _, left := range aKeys {
		for _, right := range bKeys {
			if left == right {
				return true
			}
		}
	}
	return false
}

func equivalentExact(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
