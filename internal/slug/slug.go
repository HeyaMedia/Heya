package slug

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var (
	nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)
	leadTrail   = regexp.MustCompile(`^-+|-+$`)
)

func Generate(title, year string) string {
	// Romanize any Japanese text first so kana/kanji titles produce
	// readable slugs instead of getting stripped to "untitled" by the
	// non-alphanumeric filter. Mixed-script titles keep their Latin
	// parts untouched — Transliterate is a no-op when no Japanese
	// characters are present.
	s := Transliterate(title)
	s = strings.ToLower(s)
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = leadTrail.ReplaceAllString(s, "")
	if s == "" {
		s = "untitled"
	}
	if year != "" && len(year) == 4 {
		s = s + "-" + year
	}
	return s
}

type ExistsFunc func(ctx context.Context, slug string, excludeID int64) (bool, error)

func GenerateUnique(ctx context.Context, title, year string, id int64, exists ExistsFunc) string {
	base := Generate(title, year)

	ok, err := exists(ctx, base, id)
	if err != nil || !ok {
		return base
	}

	for i := 2; i <= 100; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		ok, err := exists(ctx, candidate, id)
		if err != nil || !ok {
			return candidate
		}
	}

	return fmt.Sprintf("%s-%d", base, id)
}
