package video

import (
	"regexp"
	"strconv"
	"strings"
)

type QualityModifier string

const (
	ModREMUX  QualityModifier = "REMUX"
	ModBRDISK QualityModifier = "BRDISK"
	ModRAWHD  QualityModifier = "RAWHD"
)

var (
	properRegex    = regexp.MustCompile(`(?i)\b(?:proper|repack|rerip)\b`)
	realRegex      = regexp.MustCompile(`\b(REAL)\b`)
	versionExp     = regexp.MustCompile(`(?i)(?:v\d\b|\[v\d\])`)
	remuxExp       = regexp.MustCompile(`(?i)\b(?:(?:BD|UHD)?Remux)\b`)
	bdiskExp       = regexp.MustCompile(`(?i)\b(?:COMPLETE|ISO|BDISO|BDMux|BD25|BD50|BR\.?DISK)\b`)
	rawHdExp       = regexp.MustCompile(`(?i)\b(?:RawHD|1080i[-_. ]HDTV|Raw[-_. ]HD|MPEG[-_. ]?2)\b`)
	highDefPdtvExp = regexp.MustCompile(`(?i)hr[-_. ]ws`)
)

type QualityRevision struct {
	Version int
	Real    int
}

type QualityResult struct {
	Sources    []Source
	Resolution Resolution
	Revision   QualityRevision
	Modifier   QualityModifier
}

func parseQualityModifiers(title string) QualityRevision {
	normalized := strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(title), "_", " "))
	normalized = strings.ToLower(normalized)

	result := QualityRevision{Version: 1, Real: 0}

	if properRegex.MatchString(normalized) {
		result.Version = 2
	}

	vMatch := versionExp.FindString(normalized)
	if vMatch != "" {
		digitRe := regexp.MustCompile(`\d`)
		digits := digitRe.FindString(vMatch)
		if digits != "" {
			val, err := strconv.Atoi(digits)
			if err == nil {
				result.Version = val
			}
		}
	}

	realCount := 0
	realGlobal := regexp.MustCompile(realRegex.String())
	matches := realGlobal.FindAllString(title, -1)
	realCount = len(matches)
	result.Real = realCount

	return result
}

func ParseQuality(title string) QualityResult {
	normalized := strings.TrimSpace(title)
	normalized = strings.ReplaceAll(normalized, "_", " ")
	normalized = strings.ReplaceAll(normalized, "[", " ")
	normalized = strings.ReplaceAll(normalized, "]", " ")
	normalized = strings.TrimSpace(normalized)
	normalized = strings.ToLower(normalized)

	revision := parseQualityModifiers(title)
	resResult := ParseResolution(normalized)
	resolution := resResult.Resolution
	sourceGroups := ParseSourceGroups(normalized)
	sources := ParseSource(normalized)
	codecResult := ParseVideoCodec(title)
	codec := codecResult.Codec

	result := QualityResult{
		Sources:    sources,
		Resolution: resolution,
		Revision:   revision,
	}

	if bdiskExp.MatchString(normalized) && sourceGroups.Bluray {
		result.Modifier = ModBRDISK
		result.Sources = []Source{SourceBLURAY}
	}

	if remuxExp.MatchString(normalized) && !sourceGroups.Webdl && !sourceGroups.Hdtv {
		result.Modifier = ModREMUX
		result.Sources = []Source{SourceBLURAY}
	}

	if rawHdExp.MatchString(normalized) && result.Modifier != ModBRDISK && result.Modifier != ModREMUX {
		result.Modifier = ModRAWHD
		result.Sources = []Source{SourceTV}
	}

	if len(sources) > 0 {
		if sourceGroups.Bluray {
			result.Sources = []Source{SourceBLURAY}
			if codec == CodecXVID {
				result.Resolution = R480P
				result.Sources = []Source{SourceDVD}
			}
			if resolution == "" {
				result.Resolution = R720P
			}
			if resolution == "" && result.Modifier == ModBRDISK {
				result.Resolution = R1080P
			}
			if resolution == "" && result.Modifier == ModREMUX {
				result.Resolution = R2160P
			}
			return result
		}

		if sourceGroups.Webdl || sourceGroups.Webrip {
			result.Sources = sources
			if resolution == "" {
				result.Resolution = R480P
			}
			if resolution == "" && strings.Contains(title, "[WEBDL]") {
				result.Resolution = R720P
			}
			return result
		}

		if sourceGroups.Hdtv {
			result.Sources = []Source{SourceTV}
			if resolution == "" {
				result.Resolution = R480P
			}
			if resolution == "" && strings.Contains(title, "[HDTV]") {
				result.Resolution = R720P
			}
			return result
		}

		if sourceGroups.Pdtv || sourceGroups.Sdtv || sourceGroups.Dsr || sourceGroups.Tvrip {
			result.Sources = []Source{SourceTV}
			if highDefPdtvExp.MatchString(normalized) {
				result.Resolution = R720P
				return result
			}
			result.Resolution = R480P
			return result
		}

		if sourceGroups.Bdrip || sourceGroups.Brrip {
			if codec == CodecXVID {
				result.Resolution = R480P
				result.Sources = []Source{SourceDVD}
				return result
			}
			if resolution == "" {
				result.Resolution = R480P
			}
			result.Sources = []Source{SourceBLURAY}
			return result
		}

		if sourceGroups.Workprint {
			result.Sources = []Source{SourceWORKPRINT}
			return result
		}
		if sourceGroups.Cam {
			result.Sources = []Source{SourceCAM}
			return result
		}
		if sourceGroups.Ts {
			result.Sources = []Source{SourceTELESYNC}
			return result
		}
		if sourceGroups.Tc {
			result.Sources = []Source{SourceTELECINE}
			return result
		}
	}

	if result.Modifier == "" &&
		(resolution == R2160P || resolution == R1080P || resolution == R720P) {
		result.Sources = []Source{SourceWEBDL}
		return result
	}

	return result
}
