package matcher

import (
	"strings"
	"unicode"
)

func NormalizeTitle(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			return r
		}
		return ' '
	}, s)

	for _, article := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(s, article) {
			s = s[len(article):]
			break
		}
	}

	parts := strings.Fields(s)
	return strings.Join(parts, " ")
}

func StringSimilarity(a, b string) float64 {
	a = NormalizeTitle(a)
	b = NormalizeTitle(b)

	if a == b {
		return 1.0
	}
	if a == "" || b == "" {
		return 0.0
	}

	d := levenshtein(a, b)
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}

	return 1.0 - float64(d)/float64(maxLen)
}

func levenshtein(a, b string) int {
	la := len(a)
	lb := len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)

	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(
				prev[j]+1,
				curr[j-1]+1,
				prev[j-1]+cost,
			)
		}
		prev, curr = curr, prev
	}

	return prev[lb]
}

func ScoreConfidence(queryTitle, resultTitle, queryYear, resultYear string) float64 {
	sim := StringSimilarity(queryTitle, resultTitle)

	score := sim * 0.85

	// Substring-containment bonus. When one normalized title fully
	// contains the other, the strings differ only by a subtitle/sequel
	// suffix or prefix — that's a strong "this is the same thing"
	// signal that pure Levenshtein under-rates because the length delta
	// dominates the score. Common in anime where heya canonicalises as
	// "Demon Slayer: Kimetsu no Yaiba" while filenames carry only one
	// half, and in franchise films like "Star Wars" vs "Star Wars: A New
	// Hope".
	//
	// Requires the shorter side to be at least two words so a single
	// generic word like "Dune" or "Frozen" doesn't accidentally promote
	// every sequel hit. Year disambiguation then handles which entry
	// in a franchise the file belongs to.
	if substringTitleMatch(queryTitle, resultTitle) && score < 0.80 {
		score = 0.80
	}

	if queryYear != "" && resultYear != "" {
		if queryYear == resultYear {
			score += 0.10
		} else if abs(atoi(queryYear)-atoi(resultYear)) <= 1 {
			score += 0.05
		}
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}

// substringTitleMatch reports whether one normalized title fully contains
// the other, and the shorter side has at least two words. See
// ScoreConfidence for the rationale.
func substringTitleMatch(a, b string) bool {
	na := NormalizeTitle(a)
	nb := NormalizeTitle(b)
	if na == "" || nb == "" || na == nb {
		return false
	}
	shorter, longer := na, nb
	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}
	if !strings.Contains(longer, shorter) {
		return false
	}
	return len(strings.Fields(shorter)) >= 2
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}
