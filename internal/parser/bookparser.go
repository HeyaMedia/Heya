package parser

import (
	"regexp"
	"strings"
)

var (
	isbnRE = regexp.MustCompile(`(?:ISBN[-: ]?)?(97[89]\d{10}|\d{9}[\dXx])`)

	bookGroupRE = regexp.MustCompile(`(?:[-.])([A-Za-z0-9_]{2,})$`)

	bookYearRE = regexp.MustCompile(`\b(?:19|20)\d{2}\b`)

	cleanAuthorTitleRE = regexp.MustCompile(`^(.+?)\s*[-–—]\s*(.+?)$`)

	bracketGroupRE = regexp.MustCompile(`^\[([^\]]+)\]\s*`)

	yearParenRE = regexp.MustCompile(`\s*\((\d{4})\)\s*$`)

	bookFormatRE = regexp.MustCompile(`(?i)\b(EPUB|PDF|MOBI|AZW3?|CBR|CBZ|DJVU)\b`)
)

func canParseBook(_ PreparedSegment, mediaHint SceneMediaKind) bool {
	return mediaHint == MediaBook
}

func parseBook(prepared PreparedSegment) *SceneReleaseParse {
	workingName := prepared.CleanedName
	if workingName == "" {
		return nil
	}

	flags := append([]string{}, prepared.Flags...)
	var isbn string

	if m := isbnRE.FindString(workingName); m != "" {
		isbn = m
		flags = append(flags, "isbn-detected")
		workingName = strings.TrimSpace(isbnRE.ReplaceAllString(workingName, ""))
	}

	if m := bracketGroupRE.FindStringSubmatch(workingName); m != nil {
		workingName = workingName[len(m[0]):]
	}

	var group string
	if m := bookGroupRE.FindStringSubmatch(workingName); m != nil {
		token := m[1]
		hasUpper := regexp.MustCompile(`[A-Z]`).MatchString(token)
		if hasUpper && IsLikelySceneGroup(token) && !bookFormatRE.MatchString(token) {
			group = token
			workingName = workingName[:len(workingName)-len(m[0])]
		}
	}

	if m := bookFormatRE.FindString(workingName); m != "" {
		flags = append(flags, strings.ToLower(m))
		workingName = bookFormatRE.ReplaceAllString(workingName, "")
	}

	var year string
	if m := yearParenRE.FindStringSubmatch(workingName); m != nil {
		year = m[1]
		workingName = yearParenRE.ReplaceAllString(workingName, "")
	} else {
		matches := bookYearRE.FindAllStringIndex(workingName, -1)
		if len(matches) > 0 {
			last := matches[len(matches)-1]
			year = workingName[last[0]:last[1]]
			workingName = strings.TrimSpace(workingName[:last[0]] + workingName[last[1]:])
		}
	}

	title := normalizeBookTitle(workingName)

	if title == "" && isbn == "" {
		return nil
	}

	score := scoreBookRelease(title, year, group, isbn)
	if score < 1 {
		return nil
	}

	return &SceneReleaseParse{
		Strategy:       StrategyBookHeuristic,
		RawName:        prepared.RawName,
		NormalizedName: prepared.CleanedName,
		Media:          MediaBook,
		Title:          title,
		Year:           year,
		Group:          group,
		ReleaseHash:    isbn,
		Flags:          dedupeFlags(flags),
		Seasons:        []int{},
		Episodes:       []int{},
		IsTv:           false,
		Score:          score,
	}
}

func normalizeBookTitle(s string) string {
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.ReplaceAll(s, "_", " ")

	if m := cleanAuthorTitleRE.FindStringSubmatch(s); m != nil {
		author := strings.TrimSpace(m[1])
		title := strings.TrimSpace(m[2])
		if author != "" && title != "" && strings.Contains(author, " ") {
			s = author + " - " + title
		}
	}

	s = regexp.MustCompile(`\([^)]*\)`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`\[[^\]]*\]`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func scoreBookRelease(title, year, group, isbn string) int {
	score := 0
	if title != "" {
		score++
	}
	if year != "" {
		score++
	}
	if group != "" {
		score++
	}
	if isbn != "" {
		score += 2
	}
	if strings.Contains(title, " - ") {
		score++
	}
	return score
}
