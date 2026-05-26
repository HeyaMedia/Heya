// Package titlematch implements forgiving title comparison helpers shared
// between the matcher (album/track resolution against upstream metadata)
// and the music service (Last.fm top-track resolution against local
// tracks). The matching strategies are:
//
//  1. Case-fold exact equality
//  2. Equality after stripping parenthetical content (e.g.
//     "Title (feat. X)" → "Title")
//  3. Equality after kagome romanization (kana/kanji → romaji)
//  4. Word-boundary substring match — either side appears as a
//     contiguous run of tokens in the other. Prevents prefix
//     collisions ("Odo" no longer catches "Odoru Ponpokorin") while
//     still tolerating long parenthetical suffixes.
//
// All matchers run inside the artist's catalog scope at the call site,
// so the substring fallback can't bleed across artists.
package titlematch

import (
	"regexp"
	"strings"

	"github.com/karbowiak/heya/internal/slug"
)

// parensPattern strips runs of "(...)" / "[...]" plus full-width
// equivalents — Japanese releases frequently carry「kana」or（meta）
// asides we want to ignore.
var parensPattern = regexp.MustCompile(`\s*[\(\[（［][^\)\]）］]*[\)\]）］]\s*`)

// wordSplitPattern splits on anything that isn't a unicode letter or
// digit, producing a token stream we can compare with word-boundary
// semantics.
var wordSplitPattern = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// Normalizations returns up to four lookup keys derived from `title`:
// raw lowercase, lowercase+parens-stripped, lowercase+romanized, and
// lowercase+parens-stripped+romanized. The caller typically inserts
// these into a map keyed by the normalization, deduping on the way.
func Normalizations(title string) []string {
	if title == "" {
		return nil
	}
	out := make([]string, 0, 4)
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			out = append(out, s)
		}
	}
	add(title)
	if stripped := parensPattern.ReplaceAllString(title, " "); stripped != title {
		add(stripped)
	}
	if romanized := slug.Transliterate(title); romanized != title {
		add(romanized)
		if strippedRoman := parensPattern.ReplaceAllString(romanized, " "); strippedRoman != romanized {
			add(strippedRoman)
		}
	}
	return out
}

// Tokenize splits `s` on non-alphanumeric runs, returning the surviving
// tokens. Empty entries are dropped so re-joinable round-trips drop
// punctuation cleanly.
func Tokenize(s string) []string {
	parts := wordSplitPattern.Split(s, -1)
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ContainsWordSequence reports whether `needle` appears as a contiguous
// run of tokens inside `haystack`. Both inputs are expected to already
// be lower-cased + tokenized via Tokenize.
func ContainsWordSequence(haystack, needle []string) bool {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// FuzzyEqual is the convenience "does X probably refer to the same
// thing as Y" predicate. Order doesn't matter. Returns true on any of
// the normalization passes, plus a word-sequence containment fallback
// that needs at least 2 tokens on the shorter side to fire — that
// keeps "Show" from absorbing "Show me the World" while still letting
// "Stay Gold" match "Stay Gold (from BEYBLADE X)".
func FuzzyEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}

	aKeys := Normalizations(a)
	bKeys := Normalizations(b)
	bSet := make(map[string]struct{}, len(bKeys))
	for _, k := range bKeys {
		bSet[k] = struct{}{}
	}
	for _, k := range aKeys {
		if _, ok := bSet[k]; ok {
			return true
		}
	}

	aLower := strings.ToLower(strings.TrimSpace(slug.Transliterate(a)))
	bLower := strings.ToLower(strings.TrimSpace(slug.Transliterate(b)))
	if len(aLower) < 3 || len(bLower) < 3 {
		return false
	}
	aWords := Tokenize(aLower)
	bWords := Tokenize(bLower)
	short, long := aWords, bWords
	if len(bWords) < len(aWords) {
		short, long = bWords, aWords
	}
	if len(short) < 2 {
		return false
	}
	return ContainsWordSequence(long, short)
}
