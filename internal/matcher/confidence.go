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
