package video

import (
	"math"
	"strings"

	"github.com/dlclark/regexp2"
)

var movieTitleYearRegex []*regexp2.Regexp

func init() {
	patterns := []string{
		`^(?<title>(?![([]).+?)?(?:(?:[-_\W](?<![)[!]))*\((?<year>(?:1[89]|20)\d{2}(?!p|i|(?:1[89]|20)\d{2}|\]|\W(?:1[89]|20)\d{2})))+`,
		`^(?<title>(?![([]).+?)?(?:(?:[-_\W](?<![)[!]))*(?<year>(?:1[89]|20)\d{2}(?!p|i|(?:1[89]|20)\d{2}|\]|\W(?:1[89]|20)\d{2})))+(?:\W+|_|$)(?!\\)`,
		`^(?<title>.+?)?(?:(?:[-_\W](?<![()[!]))*(?<year>\[\w *\]))+(?:\W+|_|$)(?!\\)`,
		`^(?<title>(?![([]).+?)?(?:(?:[-_\W](?<![)!]))*(?<year>(?:1[89]|20)\d{2}(?!p|i|\d+|\W\d+)))+(?:\W+|_|$)(?!\\)`,
		`^(?<title>.+?)?(?:(?:[-_\W](?<![)[!]))*(?<year>(?:1[89]|20)\d{2}(?!p|i|\d+|\]|\W\d+)))+(?:\W+|_|$)(?!\\)`,
	}

	for _, p := range patterns {
		re, err := regexp2.Compile("(?i)"+p, regexp2.ECMAScript)
		if err != nil {
			panic("failed to compile title regex: " + err.Error())
		}
		movieTitleYearRegex = append(movieTitleYearRegex, re)
	}
}

type TitleAndYear struct {
	Title string
	Year  string
}

func getGroupR2(match *regexp2.Match, name string) string {
	g := match.GroupByName(name)
	if g == nil {
		return ""
	}
	return g.String()
}

func ParseTitleAndYear(title string) TitleAndYear {
	simpleTitle := SimplifyTitle(title)
	grouplessRe, _ := regexp2.Compile(`(?i)-([a-z0-9]+)$`, regexp2.None)
	grouplessTitle, _ := grouplessRe.Replace(simpleTitle, "", -1, -1)

	for _, exp := range movieTitleYearRegex {
		match, err := exp.FindStringMatch(grouplessTitle)
		if err != nil || match == nil {
			continue
		}

		titleStr := getGroupR2(match, "title")
		result := ReleaseTitleCleaner(titleStr)
		if result == "" {
			continue
		}

		year := getGroupR2(match, "year")
		return TitleAndYear{Title: result, Year: year}
	}

	resResult := ParseResolution(title)
	resPosition := strings.Index(title, resResult.Source)

	codecResult := ParseVideoCodec(title)
	codecPosition := strings.Index(title, codecResult.Source)

	channelsResult := ParseAudioChannels(title)
	channelsPosition := strings.Index(title, channelsResult.Source)

	audioResult := ParseAudioCodec(title)
	audioPosition := strings.Index(title, audioResult.Source)

	var positions []int
	for _, p := range []int{resPosition, codecPosition, channelsPosition, audioPosition} {
		if p > 0 {
			positions = append(positions, p)
		}
	}

	if len(positions) > 0 {
		minPos := math.MaxInt
		for _, p := range positions {
			if p < minPos {
				minPos = p
			}
		}
		cleaned := ReleaseTitleCleaner(title[:minPos])
		if cleaned == "" {
			return TitleAndYear{Title: strings.TrimSpace(title)}
		}
		return TitleAndYear{Title: cleaned}
	}

	return TitleAndYear{Title: strings.TrimSpace(title)}
}
