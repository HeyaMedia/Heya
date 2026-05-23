package video

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
)

type Season struct {
	ReleaseTitle    string
	SeriesTitle     string
	Seasons         []int
	EpisodeNumbers  []int
	AirDate         *time.Time
	FullSeason      bool
	IsPartialSeason bool
	IsMultiSeason   bool
	IsSeasonExtra   bool
	IsSpecial       bool
	SeasonPart      int
}

var reportTitleExp []*regexp2.Regexp

func init() {
	patterns := []string{
		// Daily episodes without title
		`^(?<airyear>19[6-9]\d|20\d\d)(?<sep>[-_]?)(?<airmonth>0\d|1[0-2])\k<sep>(?<airday>[0-2]\d|3[01])(?!\d)`,
		// Multi-Part episodes without title
		`^(?:\W*S?(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?!\d+))(?:(?:[ex]){1,2}(?<episode>\d{1,3}(?!\d+)))+){2,}`,
		// Multi-episode with single episode numbers
		`^(?<title>.+?)[-_. ]S(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?!\d+))(?:[E\-_. ]?[ex]?(?<episode>(?<!\d+)\d{1,2}(?!\d+)))+(?:[-_. ]?[ex]?(?<episode1>(?<!\d+)\d{1,2}(?!\d+)))+`,
		// Multi-Episode with title and trailing info in slashes
		`^(?<title>.+?)(?:(?:[-_\W](?<![()[!]))+S?(?<season>(?<!\d+)(?:\d{1,2})(?!\d+))(?:[ex]|\W[ex]|_){1,2}(?<episode>\d{2,3}(?!\d+))(?:(?:-|[ex]|\W[ex]|_){1,2}(?<episode1>\d{2,3}(?!\d+)))+).+?(?:\[.+?\])(?!\\)`,
		// Episodes without title, Multi
		`(?:S?(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?!\d+))(?:(?:[-_]|[ex]){1,2}(?<episode>\d{2,3}(?!\d+))){2,})`,
		// Episodes without title, Single
		`^(?:S?(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?!\d+))(?:(?:[-_ ]?[ex])(?<episode>\d{2,3}(?!\d+))))`,
		// Anime - [SubGroup] Title Episode Absolute Episode Number
		`^(?:\[(?<subgroup>.+?)\][-_. ]?)(?<title>.+?)[-_. ](?:Episode)(?:[-_. ]+(?<absoluteepisode>(?<!\d+)\d{2,3}(\.\d{1,2})?(?!\d+)))+(?:_|-|\s|\.)*?(?<hash>\[.{8}\])?(?:$|\.)`,
		// Anime - [SubGroup] Title Absolute + Season+Episode
		`^(?:\[(?<subgroup>.+?)\](?:_|-|\s|\.)?)(?<title>.+?)(?:(?:[-_\W](?<![()[!]))+(?<absoluteepisode>\d{2,3}(\.\d{1,2})?))+(?:_|-|\s|\.)+(?:S?(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:-|[ex]|\W[ex]){1,2}(?<episode>\d{2}(?!\d+)))+).*?(?<hash>[([]\w{8}[)\]])?(?:$|\.)`,
		// Anime - [SubGroup] Title Season+Episode + Absolute
		`^(?:\[(?<subgroup>.+?)\](?:_|-|\s|\.)?)(?<title>.+?)(?:[-_\W](?<![()[!]))+(?:S?(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:-|[ex]|\W[ex]){1,2}(?<episode>\d{2}(?!\d+)))+)(?:(?:_|-|\s|\.)+(?<absoluteepisode>(?<!\d+)\d{2,3}(\.\d{1,2})?(?!\d+)))+.*?(?<hash>\[\w{8}\])?(?:$|\.)`,
		// Anime - [SubGroup] Title Season+Episode
		`^(?:\[(?<subgroup>.+?)\](?:_|-|\s|\.)?)(?<title>.+?)(?:[-_\W](?<![()[!]))+(?:S?(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:[ex]|\W[ex]){1,2}(?<episode>\d{2}(?!\d+)))+)(?:\s|\.).*?(?<hash>\[\w{8}\])?(?:$|\.)`,
		// Anime - [SubGroup] Title with trailing number Absolute Episode
		`^\[(?<subgroup>.+?)\][-_. ]?(?<title>[^-]+?\d+?)[-_. ]+(?:[-_. ]?(?<absoluteepisode>\d{3}(\.\d{1,2})?(?!\d+)))+(?:[-_. ]+(?<special>special|ova|ovd))?.*?(?<hash>\[\w{8}\])?(?:$|\.mkv)`,
		// Anime - [SubGroup] Title - Absolute Episode Number
		`^\[(?<subgroup>.+?)\][-_. ]?(?<title>.+?)(?:[. ]-[. ](?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+|[-])))+(?:[-_. ]+(?<special>special|ova|ovd))?.*?(?<hash>\[\w{8}\])?(?:$|\.mkv)`,
		// Anime - [SubGroup] Title Absolute Episode Number
		`^\[(?<subgroup>.+?)\][-_. ]?(?<title>.+?)[-_. ]+\(?(?:[-_. ]?#?(?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+)))+\)?(?:[-_. ]+(?<special>special|ova|ovd))?.*?(?<hash>\[\w{8}\])?(?:$|\.mkv)`,
		// Multi-episode Repeated
		`^(?<title>.+?)(?:(?:[-_\W](?<![()[!]))+S?(?<season>(?<!\d+)(?:\d{1,2}|\d{4})(?!\d+))(?:(?:[ex]|[-_. ]e){1,2}(?<episode>\d{1,3}(?!\d+)))+){2,}`,
		// Single episodes with title
		`^(?<title>.+?)(?:(?:[-_\W](?<![()[!]))+S?(?<season>(?<!\d+)(?:\d{1,2})(?!\d+))(?:[ex]|\W[ex]|_){1,2}(?<episode>(?!265|264)\d{2,3}(?!\d+|(?:[ex]|\W[ex]|_|-){1,2})))`,
		// Anime - Title Season EpisodeNumber + Absolute [SubGroup]
		`^(?<title>.+?)(?:[-_\W](?<![()[!]))+(?:S?(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:[ex]|\W[ex]){1,2}(?<episode>(?<!\d+)\d{2}(?!\d+)))).+?(?:[-_. ]?(?<absoluteepisode>(?<!\d+)\d{3}(\.\d{1,2})?(?!\d+)))+.+?\[(?<subgroup>.+?)\](?:$|\.mkv)`,
		// Anime - Title Absolute Episode Number [SubGroup] [Hash]? (Series Title Episode 99-100)
		`^(?<title>.+?)[-_. ]Episode(?:[-_. ]+(?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+)))+(?:.+?)\[(?<subgroup>.+?)\].*?(?<hash>\[\w{8}\])?(?:$|\.)`,
		// Anime - Title Absolute Episode Number [SubGroup] [Hash]
		`^(?<title>.+?)(?:(?:_|-|\s|\.)+(?<absoluteepisode>\d{3}(\.\d{1,2})(?!\d+)))+(?:.+?)\[(?<subgroup>.+?)\].*?(?<hash>\[\w{8}\])?(?:$|\.)`,
		// Anime - Title Absolute Episode Number [Hash]
		`^(?<title>.+?)(?:(?:_|-|\s|\.)+(?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+)))+(?:[-_. ]+(?<special>special|ova|ovd))?[-_. ]+.*?(?<hash>\[\w{8}\])(?:$|\.)`,
		// Episodes with airdate AND season/episode, capture season/episode only
		`^(?<title>.+?)?\W*(?<airdate>\d{4}\W+[0-1][0-9]\W+[0-3][0-9])(?!\W+[0-3][0-9])[-_. ](?:s?(?<season>(?<!\d+)(?:\d{1,2})(?!\d+)))(?:[ex](?<episode>(?<!\d+)(?:\d{1,3})(?!\d+)))`,
		// Episodes with airdate AND season/episode
		`^(?<title>.+?)?\W*(?<airyear>\d{4})\W+(?<airmonth>[0-1][0-9])\W+(?<airday>[0-3][0-9])(?!\W+[0-3][0-9]).+?(?:s?(?<season>(?<!\d+)(?:\d{1,2})(?!\d+)))(?:[ex](?<episode>(?<!\d+)(?:\d{1,3})(?!\d+)))`,
		// 4 digit season, Single/Multi (S2016E05)
		`^(?<title>.+?)(?:(?:[-_\W](?<![()[!]))+S(?<season>(?<!\d+)(?:\d{4})(?!\d+))(?:e|\We|_){1,2}(?<episode>\d{2,3}(?!\d+))(?:(?:-|e|\We|_){1,2}(?<episode1>\d{2,3}(?!\d+)))*)\W?(?!\\)`,
		// 4 digit season, x format (2016x05)
		`^(?<title>.+?)(?:(?:[-_\W](?<![()[!]))+(?<season>(?<!\d+)(?:\d{4})(?!\d+))(?:x|\Wx){1,2}(?<episode>\d{2,3}(?!\d+))(?:(?:-|x|\Wx|_){1,2}(?<episode1>\d{2,3}(?!\d+)))*)\W?(?!\\)`,
		// Multi-season pack
		`^(?<title>.+?)[-_. ]+S(?<season>(?<!\d+)(?:\d{1,2})(?!\d+))\W?-\W?S?(?<season1>(?<!\d+)(?:\d{1,2})(?!\d+))`,
		// Partial season pack
		`^(?<title>.+?)(?:\W+S(?<season>(?<!\d+)(?:\d{1,2})(?!\d+))\W+(?:(?:Part\W?|(?<!\d+\W+)e)(?<seasonpart>\d{1,2}(?!\d+)))+)`,
		// Mini-Series with year in title
		`^(?<title>.+?\d{4})(?:\W+(?:(?:Part\W?|e)(?<episode>\d{1,2}(?!\d+)))+)`,
		// Mini-Series, multi episodes E1-E2
		`^(?<title>.+?)(?:[-._ ][e])(?<episode>\d{2,3}(?!\d+))(?:(?:-?[e])(?<episode1>\d{2,3}(?!\d+)))+`,
		// Mini-Series, episodes labelled as Part01
		`^(?<title>.+?)(?:\W+(?:(?:Part\W?|(?<!\d+\W+)e)(?<episode>\d{1,2}(?!\d+)))+)`,
		// Mini-Series, Part One/Two/etc
		`^(?<title>.+?)(?:\W+(?:Part[-._ ](?<episode>One|Two|Three|Four|Five|Six|Seven|Eight|Nine)(?>[-._ ])))`,
		// Mini-Series, XofY
		`^(?<title>.+?)(?:\W+(?:(?<episode>(?<!\d+)\d{1,2}(?!\d+))of\d+)+)`,
		// Season 01 Episode 03
		`(?:.*(?:""|^))(?<title>.*?)(?:[-_\W](?<![()[]))+(?:\W?Season\W?)(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:\W|_)+(?:Episode\W)(?:[-_. ]?(?<episode>(?<!\d+)\d{1,2}(?!\d+)))+`,
		// Multi-episode in square brackets
		`(?:.*(?:^))(?<title>.*?)[-._ ]+\[S(?<season>(?<!\d+)\d{2}(?!\d+))(?:[E-]{1,2}(?<episode>(?<!\d+)\d{2}(?!\d+)))+\]`,
		// Multi-episode no space (S01E11E12)
		`(?:.*(?:^))(?<title>.*?)S(?<season>(?<!\d+)\d{2}(?!\d+))(?:E(?<episode>(?<!\d+)\d{2}(?!\d+)))+`,
		// S1E1 or S1-E1 or S1.Ep1
		`(?:.*(?:""|^))(?<title>.*?)(?:\W?|_)S(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:\W|_)?Ep?[ ._]?(?<episode>(?<!\d+)\d{1,2}(?!\d+))`,
		// 3 digit season S010E05
		`(?:.*(?:""|^))(?<title>.*?)(?:\W?|_)S(?<season>(?<!\d+)\d{3}(?!\d+))(?:\W|_)?E(?<episode>(?<!\d+)\d{1,2}(?!\d+))`,
		// 5 digit episode with title
		`^(?:(?<title>.+?)(?:_|-|\s|\.)+)(?:S?(?<season>(?<!\d+)\d{1,2}(?!\d+)))(?:(?:-|[ex]|\W[ex]|_){1,2}(?<episode>(?<!\d+)\d{5}(?!\d+)))`,
		// 5 digit multi-episode with title
		`^(?:(?<title>.+?)(?:_|-|\s|\.)+)(?:S?(?<season>(?<!\d+)\d{1,2}(?!\d+)))(?:(?:[-_. ]{1,3}ep){1,2}(?<episode>(?<!\d+)\d{5}(?!\d+)))+`,
		// Separated season and episode S01 - E01
		`^(?<title>.+?)(?:_|-|\s|\.)+S(?<season>\d{2}(?!\d+))(\W-\W)E(?<episode>(?<!\d+)\d{2}(?!\d+))(?!\\)`,
		// Anime - Title with season number - Absolute Episode (Title S01 - EP14)
		`^(?<title>.+?S\d{1,2})[-_. ]{3,}(?:EP)?(?<absoluteepisode>\d{2,3}(\.\d{1,2})?(?!\d+|[-]))`,
		// Anime - French titles with single episode numbers
		`^(?:\[(?<subgroup>.+?)\][-_. ]?)?(?<title>.+?)[-_. ]+?(?:Episode[-_. ]+?)(?<absoluteepisode>\d{1}(\.\d{1,2})?(?!\d+))`,
		// Season only releases
		`^(?<title>.+?)\W(?:S|Season)\W?(?<season>\d{1,2}(?!\d+))(\W+|_|$)(?<extras>EXTRAS|SUBPACK)?(?!\\)`,
		// 4 digit season only
		`^(?<title>.+?)\W(?:S|Season)\W?(?<season>\d{4}(?!\d+))(\W+|_|$)(?<extras>EXTRAS|SUBPACK)?(?!\\)`,
		// Episodes with title and season/episode in square brackets
		`^(?<title>.+?)(?:(?:[-_\W](?<![()[!]))+\[S?(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:-|[ex]|\W[ex]|_){1,2}(?<episode>(?<!\d+)\d{2}(?!\d+|i|p)))+\])\W?(?!\\)`,
		// 103/113 naming
		`^(?<title>.+?)?(?:(?:[_.](?<![()[!]))+(?<season>(?<!\d+)[1-9])(?<episode>[1-9][0-9]|[0][1-9])(?![a-z]|\d+))+(?:[_.]|$)`,
		// 4 digit episode without title
		`^(?:S?(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:-|[ex]|\W[ex]|_){1,2}(?<episode>\d{4}(?!\d+|i|p)))+)(\W+|_|$)(?!\\)`,
		// 4 digit episode with title
		`^(?<title>.+?)(?:(?:[-_\W](?<![()[!]))+S?(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:-|[ex]|\W[ex]|_){1,2}(?<episode>\d{4}(?!\d+|i|p)))+)\W?(?!\\)`,
		// Episodes with airdate (2018.04.28)
		`^(?<title>.+?)?\W*(?<airyear>\d{4})[-_. ]+(?<airmonth>[0-1][0-9])[-_. ]+(?<airday>[0-3][0-9])(?![-_. ]+[0-3][0-9])`,
		// Episodes with airdate (04.28.2018)
		`^(?<title>.+?)?\W*(?<airmonth>[0-1][0-9])[-_. ]+(?<airday>[0-3][0-9])[-_. ]+(?<airyear>\d{4})(?!\d+)`,
		// 1103/1113 naming
		`^(?<title>.+?)?(?:(?:[-_\W](?<![()[!]))*(?<season>(?<!\d+|\(|\[|e|x)\d{2})(?<episode>(?<!e|x)\d{2}(?!p|i|\d+|\)|\]|\W\d+|\W(?:e|ep|x)\d+)))+(\W+|_|$)(?!\\)`,
		// Single digit episode (S01E1)
		`^(?<title>.*?)(?:(?:[-_\W](?<![()[!]))+S?(?<season>(?<!\d+)\d{1,2}(?!\d+))(?:(?:-|[ex]){1,2}(?<episode>\d{1}))+)+(\W+|_|$)(?!\\)`,
		// iTunes Season 1\05 Title
		`^(?:Season(?:_|-|\s|\.)(?<season>(?<!\d+)\d{1,2}(?!\d+)))(?:_|-|\s|\.)(?<episode>(?<!\d+)\d{1,2}(?!\d+))`,
		// iTunes 1-05 Title
		`^(?:(?<season>(?<!\d+)(?:\d{1,2})(?!\d+))(?:-(?<episode>\d{2,3}(?!\d+))))`,
		// Anime Range ep01-12
		`^(?:\[(?<subgroup>.+?)\][-_. ]?)?(?<title>.+?)(?:_|\s|\.)+(?:e|ep)(?<absoluteepisode>\d{2,3}(\.\d{1,2})?)-(?<absoluteepisode1>(?<!\d+)\d{1,2}(\.\d{1,2})?(?!\d+|-)).*?(?<hash>\[\w{8}\])?(?:$|\.)`,
		// Anime - Title Absolute Episode Number (e66)
		`^(?:\[(?<subgroup>.+?)\][-_. ]?)?(?<title>.+?)(?:(?:_|-|\s|\.)+(?:e|ep)(?<absoluteepisode>\d{2,4}(\.\d{1,2})?))+.*?(?<hash>\[\w{8}\])?(?:$|\.)`,
		// Anime - Title Episode Absolute
		`^(?<title>.+?)[-_. ](?:Episode)(?:[-_. ]+(?<absoluteepisode>(?<!\d+)\d{2,3}(\.\d{1,2})?(?!\d+)))+(?:_|-|\s|\.)*?(?<hash>\[.{8}\])?(?:$|\.)?`,
		// Anime Range 1-digit absolute (1-10)
		`^(?:\[(?<subgroup>.+?)\][-_. ]?)?(?<title>.+?)[_. ]+(?<absoluteepisode>(?<!\d+)\d{1,2}(\.\d{1,2})?(?!\d+))-(?<absoluteepisode1>(?<!\d+)\d{1,2}(\.\d{1,2})?(?!\d+|-))(?:_|\s|\.)*?(?<hash>\[.{8}\])?(?:$|\.)?`,
		// Anime - Title Absolute Episode Number (2-3 digits)
		`^(?:\[(?<subgroup>.+?)\][-_. ]?)?(?<title>.+?)(?:[-_. ]+(?<absoluteepisode>(?<!\d+)\d{2,3}(\.\d{1,2})?(?!\d+)))+(?:_|-|\s|\.)*?(?<hash>\[.{8}\])?(?:$|\.)?`,
		// Anime - Title {Absolute Episode Number}
		`^(?:\[(?<subgroup>.+?)\][-_. ]?)?(?<title>.+?)(?:(?:[-_\W](?<![()[!]))+(?<absoluteepisode>(?<!\d+)\d{2,3}(\.\d{1,2})?(?!\d+)))+(?:_|-|\s|\.)*?(?<hash>\[.{8}\])?(?:$|\.)?`,
		// Extant multi-episode (extant.10708.hdtv-lol.mp4)
		`^(?<title>.+?)[-_. ](?<season>[0]?\d?)(?:(?<episode>\d{2}){2}(?!\d+))[-_. ]`,
	}

	for _, p := range patterns {
		re, err := regexp2.Compile("(?i)"+p, regexp2.RE2)
		if err != nil {
			re, _ = regexp2.Compile("(?i)"+p, regexp2.None)
		}
		reportTitleExp = append(reportTitleExp, re)
	}
}

var rejectedRegexes = []*regexp.Regexp{
	regexp.MustCompile(`^[0-9a-zA-Z]{32}`),
	regexp.MustCompile(`(?i)^[a-z0-9]{24}$`),
	regexp.MustCompile(`^[A-Z]{11}\d{3}$`),
	regexp.MustCompile(`^[a-z]{12}\d{3}$`),
	regexp.MustCompile(`(?i)^Backup_\d{5,}S\d{2}-\d{2}$`),
	regexp.MustCompile(`^123$`),
	regexp.MustCompile(`(?i)^abc$`),
	regexp.MustCompile(`(?i)^b00bs$`),
	regexp.MustCompile(`^\d{6}_\d{2}$`),
}

var requestInfoExp = regexp.MustCompile(`^(?:\[.+?\])+`)

var sixDigitAirDateMatchExp = regexp.MustCompile(`(?i)(?:^|[_.\-])(?P<airdate>(?P<airyear>[1-9]\d)(?P<airmonth>[0-1][0-9])(?P<airday>[0-3][0-9]))[_.\-]`)

func ParseSeason(title string) *Season {
	if !preValidation(title) {
		return nil
	}

	simpleTitle := SimplifyTitle(title)

	sixMatch := sixDigitAirDateMatchExp.FindStringSubmatch(title)
	if sixMatch != nil {
		names := sixDigitAirDateMatchExp.SubexpNames()
		groups := make(map[string]string)
		for i, name := range names {
			if i > 0 && name != "" && i < len(sixMatch) && sixMatch[i] != "" {
				groups[name] = sixMatch[i]
			}
		}
		airYear := groups["airyear"]
		airMonth := groups["airmonth"]
		airDay := groups["airday"]
		airdate := groups["airdate"]
		if airMonth != "00" || airDay != "00" {
			fixedDate := fmt.Sprintf("20%s.%s.%s", airYear, airMonth, airDay)
			simpleTitle = strings.Replace(simpleTitle, airdate, fixedDate, 1)
		}
	}

	for _, exp := range reportTitleExp {
		match, err := exp.FindStringMatch(simpleTitle)
		if err != nil || match == nil {
			continue
		}

		result := parseMatchCollection(match, simpleTitle)
		if result == nil {
			continue
		}

		if result.FullSeason && result.releaseTokens != "" {
			specialRe := regexp.MustCompile(`(?i)Special`)
			if specialRe.MatchString(result.releaseTokens) {
				result.FullSeason = false
				result.IsSpecial = true
			}
		}

		return &Season{
			ReleaseTitle:    title,
			SeriesTitle:     result.seriesName,
			Seasons:         result.seasonNumbers,
			EpisodeNumbers:  result.episodeNumbers,
			AirDate:         result.airDate,
			FullSeason:      result.FullSeason,
			IsPartialSeason: result.IsPartialSeason,
			IsMultiSeason:   result.IsMultiSeason,
			IsSeasonExtra:   result.IsSeasonExtra,
			IsSpecial:       result.IsSpecial,
			SeasonPart:      result.seasonPart,
		}
	}

	return nil
}

func preValidation(title string) bool {
	for _, exp := range rejectedRegexes {
		if exp.MatchString(title) {
			return false
		}
	}
	return true
}

func CompleteRange(arr []int) []int {
	if len(arr) == 0 {
		return arr
	}

	seen := map[int]bool{}
	var unique []int
	for _, v := range arr {
		if !seen[v] {
			seen[v] = true
			unique = append(unique, v)
		}
	}
	sort.Ints(unique)

	first := unique[0]
	last := unique[len(unique)-1]
	if first > last {
		return arr
	}

	count := last - first + 1
	result := make([]int, count)
	for i := range result {
		result[i] = first + i
	}
	return result
}

type matchResult struct {
	seriesName      string
	seasonNumbers   []int
	episodeNumbers  []int
	airDate         *time.Time
	FullSeason      bool
	IsPartialSeason bool
	IsMultiSeason   bool
	IsSeasonExtra   bool
	IsSpecial       bool
	seasonPart      int
	releaseTokens   string
}

func getGroup(match *regexp2.Match, name string) string {
	g := match.GroupByName(name)
	if g == nil {
		return ""
	}
	return g.String()
}

func getAllCaptures(match *regexp2.Match, name string) []string {
	g := match.GroupByName(name)
	if g == nil {
		return nil
	}
	var results []string
	for _, c := range g.Captures {
		if c.String() != "" {
			results = append(results, c.String())
		}
	}
	return results
}

func indexOfEnd(str1, str2 string) int {
	idx := strings.Index(str1, str2)
	if idx == -1 {
		return -1
	}
	return idx + len(str2)
}

func parseMatchCollection(match *regexp2.Match, simpleTitle string) *matchResult {
	seriesName := getGroup(match, "title")
	seriesName = strings.ReplaceAll(seriesName, ".", " ")
	seriesName = strings.ReplaceAll(seriesName, "_", " ")
	seriesName = requestInfoExp.ReplaceAllString(seriesName, "")
	seriesName = strings.TrimSpace(seriesName)

	result := &matchResult{
		seriesName: seriesName,
	}

	titleGroup := getGroup(match, "title")
	lastIndex := indexOfEnd(simpleTitle, titleGroup)

	airYearStr := getGroup(match, "airyear")
	airYear, _ := strconv.Atoi(airYearStr)

	if airYear < 1900 || airYearStr == "" {
		seasonStr := getGroup(match, "season")
		season1Str := getGroup(match, "season1")

		var seasons []int
		if seasonStr != "" {
			s, _ := strconv.Atoi(seasonStr)
			seasons = append(seasons, s)
			lastIndex = maxInt(indexOfEnd(simpleTitle, seasonStr), lastIndex)
		}
		if season1Str != "" {
			s, _ := strconv.Atoi(season1Str)
			seasons = append(seasons, s)
			lastIndex = maxInt(indexOfEnd(simpleTitle, season1Str), lastIndex)
		}

		if len(seasons) > 1 {
			seasons = CompleteRange(seasons)
			result.IsMultiSeason = true
		}
		result.seasonNumbers = seasons

		episodeCaptures := getAllCaptures(match, "episode")
		episode1Captures := getAllCaptures(match, "episode1")
		allEpisodes := append(episodeCaptures, episode1Captures...)

		absCaptures := getAllCaptures(match, "absoluteepisode")
		abs1Captures := getAllCaptures(match, "absoluteepisode1")
		allAbsolute := append(absCaptures, abs1Captures...)

		if len(allEpisodes) > 0 {
			first, _ := strconv.Atoi(allEpisodes[0])
			last, _ := strconv.Atoi(allEpisodes[len(allEpisodes)-1])
			if first > last {
				return nil
			}
			count := last - first + 1
			eps := make([]int, count)
			for i := range eps {
				eps[i] = first + i
			}
			result.episodeNumbers = eps
		}

		if len(allAbsolute) > 0 {
			firstF, _ := strconv.ParseFloat(allAbsolute[0], 64)
			lastIdx := allAbsolute[len(allAbsolute)-1]
			if len(allEpisodes) > 0 && len(allEpisodes)-1 < len(allAbsolute) {
				lastIdx = allAbsolute[len(allEpisodes)-1]
			}
			lastF, _ := strconv.ParseFloat(lastIdx, 64)

			if math.Floor(firstF) != firstF || math.Floor(lastF) != lastF {
				if len(allAbsolute) != 1 {
					return nil
				}
				result.episodeNumbers = []int{int(firstF)}
				result.IsSpecial = true
				if len(allAbsolute) > 0 {
					lastIndex = maxInt(indexOfEnd(simpleTitle, allAbsolute[0]), lastIndex)
				}
			} else {
				first := int(firstF)
				last := int(lastF)
				count := last - first + 1
				eps := make([]int, count)
				for i := range eps {
					eps[i] = first + i
				}
				result.episodeNumbers = eps

				specialStr := getGroup(match, "special")
				if specialStr != "" {
					result.IsSpecial = true
				}
			}
		}

		if len(allEpisodes) == 0 && len(allAbsolute) == 0 {
			extras := getGroup(match, "extras")
			if extras != "" {
				result.IsSeasonExtra = true
			}

			seasonPart := getGroup(match, "seasonpart")
			if seasonPart != "" {
				sp, _ := strconv.Atoi(seasonPart)
				result.seasonPart = sp
				result.IsPartialSeason = true
			} else {
				result.FullSeason = true
			}
		}

		if len(allAbsolute) > 0 && result.episodeNumbers == nil {
			result.seasonNumbers = []int{0}
		}
	} else {
		airMonthStr := getGroup(match, "airmonth")
		airDayStr := getGroup(match, "airday")
		airMonth, _ := strconv.Atoi(airMonthStr)
		airDay, _ := strconv.Atoi(airDayStr)

		if airMonth > 12 {
			airMonth, airDay = airDay, airMonth
		}

		airDate := time.Date(airYear, time.Month(airMonth), airDay, 0, 0, 0, 0, time.UTC)
		if airDate.After(time.Now()) {
			return nil
		}
		if airDate.Before(time.Date(1970, 2, 1, 0, 0, 0, 0, time.UTC)) {
			return nil
		}

		lastIndex = maxInt(indexOfEnd(simpleTitle, airYearStr), lastIndex)
		lastIndex = maxInt(indexOfEnd(simpleTitle, airMonthStr), lastIndex)
		lastIndex = maxInt(indexOfEnd(simpleTitle, airDayStr), lastIndex)
		result.airDate = &airDate
	}

	if lastIndex == len(simpleTitle) || lastIndex == -1 {
		result.releaseTokens = simpleTitle
	} else {
		result.releaseTokens = simpleTitle[lastIndex:]
	}

	return result
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
