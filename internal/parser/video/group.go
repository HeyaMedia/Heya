package video

import (
	"regexp"
	"strings"
)

var websitePrefixExp = regexp.MustCompile(`(?i)^\[\s*[a-z]+(\.[a-z]+)+\s*\][- ]*|^www\.[a-z]+\.(?:com|net)[ -]*`)
var cleanReleaseGroupExp = regexp.MustCompile(`(?i)(?:-(RP|1|NZBGeek|Obfuscated|Obfuscation|Scrambled|sample|Pre|postbot|xpost|Rakuv[a-z0-9]*|WhiteRev|BUYMORE|AsRequested|AlternativeToRequested|GEROV|Z0iDS3N|Chamele0n|4P|4Planet|AlteZachen|RePACKPOST))+$`)
var releaseGroupRegexExp = regexp.MustCompile(`(?i)-(?P<releasegroup>[a-z0-9]+)(?:\b|[-. _])`)
var animeReleaseGroupExp = regexp.MustCompile(`(?i)^(?:\[(?P<subgroup>[^\s][^\]]*[^\s])\](?:_|-|\s|\.)?)`)
var exceptionReleaseGroupRegex = regexp.MustCompile(`(?i)(?:\[)?(?P<releasegroup>Joy|YIFY|YTS\.(?:MX|LT|AG)|FreetheFish|VH-PROD|FTW-HS|DX-TV|Blu-bits|afm72|Anna|Bandi|Ghost|Kappa|MONOLITH|Qman|RZeroX|SAMPA|Silence|theincognito|D-Z0N3|t3nzin|Vyndros|HDO|DusIctv|DHD|SEV|CtrlHD|-ZR-|ADC|XZVN|RH|Kametsu|r00t|HONE)(?:\])?$`)

func ParseGroup(title string) string {
	nowebsiteTitle := websitePrefixExp.ReplaceAllString(title, "")
	releaseTitle := ParseTitleAndYear(nowebsiteTitle).Title
	releaseTitle = strings.ReplaceAll(releaseTitle, " ", ".")

	trimmed := nowebsiteTitle
	trimmed = strings.ReplaceAll(trimmed, " ", ".")
	if releaseTitle != nowebsiteTitle {
		trimmed = strings.Replace(trimmed, releaseTitle, "", 1)
	}
	trimmed = strings.ReplaceAll(trimmed, ".-.", ".")
	trimmed = SimplifyTitle(RemoveFileExtension(strings.TrimSpace(trimmed)))

	if len(trimmed) == 0 {
		return ""
	}

	exMatch := exceptionReleaseGroupRegex.FindStringSubmatch(trimmed)
	if exMatch != nil {
		for i, name := range exceptionReleaseGroupRegex.SubexpNames() {
			if name == "releasegroup" && i < len(exMatch) && exMatch[i] != "" {
				return exMatch[i]
			}
		}
	}

	animeMatch := animeReleaseGroupExp.FindStringSubmatch(trimmed)
	if animeMatch != nil {
		for i, name := range animeReleaseGroupExp.SubexpNames() {
			if name == "subgroup" && i < len(animeMatch) && animeMatch[i] != "" {
				return animeMatch[i]
			}
		}
	}

	trimmed = cleanReleaseGroupExp.ReplaceAllString(trimmed, "")

	allMatches := releaseGroupRegexExp.FindAllStringSubmatch(trimmed, -1)
	for _, match := range allMatches {
		for i, name := range releaseGroupRegexExp.SubexpNames() {
			if name == "releasegroup" && i < len(match) && match[i] != "" {
				return match[i]
			}
		}
	}

	return ""
}
