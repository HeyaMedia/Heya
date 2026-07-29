package formats

import (
	"sync"
	"time"

	"github.com/dlclark/regexp2"
)

// TRaSH regexes are written for .NET and lean on lookarounds Go's RE2
// deliberately omits, so conditions compile through regexp2 (a .NET-port
// engine). The timeout bounds catastrophic backtracking — a condition that
// trips it counts as not matched rather than wedging a search.
const regexTimeout = 2 * time.Second

var regexCache sync.Map // expr string → *regexp2.Regexp

func compiledRegex(expr string) (*regexp2.Regexp, error) {
	if cached, ok := regexCache.Load(expr); ok {
		return cached.(*regexp2.Regexp), nil
	}
	re, err := regexp2.Compile(expr, regexp2.IgnoreCase)
	if err != nil {
		return nil, err
	}
	re.MatchTimeout = regexTimeout
	regexCache.Store(expr, re)
	return re, nil
}

func regexMatch(expr, input string) bool {
	if expr == "" || input == "" {
		return false
	}
	re, err := compiledRegex(expr)
	if err != nil {
		return false
	}
	matched, err := re.MatchString(input)
	return err == nil && matched
}

// Matches reports whether a release satisfies the format, using the arr
// semantics the imported libraries were tuned against: specs are grouped by
// implementation; the format matches when every group is satisfied; a group
// is satisfied when all its required specs match and, if it has any optional
// specs, at least one of those matches too. Negate inverts a single spec.
func Matches(format Format, attrs Attrs) bool {
	if len(format.Specs) == 0 {
		return false
	}
	groups := make(map[string][]CustomFormatSpec)
	for _, spec := range format.Specs {
		groups[spec.Implementation] = append(groups[spec.Implementation], spec)
	}
	for _, specs := range groups {
		requiredOK := true
		optionalSeen := false
		optionalOK := false
		for _, spec := range specs {
			matched := specMatches(spec, attrs)
			if spec.Negate {
				matched = !matched
			}
			if spec.Required {
				requiredOK = requiredOK && matched
			} else {
				optionalSeen = true
				optionalOK = optionalOK || matched
			}
		}
		if !requiredOK || (optionalSeen && !optionalOK) {
			return false
		}
	}
	return true
}

func specMatches(spec CustomFormatSpec, attrs Attrs) bool {
	switch spec.Implementation {
	case SpecReleaseTitle:
		return regexMatch(spec.fieldString("value"), attrs.Title)
	case SpecReleaseGroup:
		return regexMatch(spec.fieldString("value"), attrs.Group)
	case SpecEdition:
		return regexMatch(spec.fieldString("value"), attrs.Edition)
	case SpecResolution:
		want, ok := spec.fieldInt("value")
		return ok && want == attrs.Resolution
	case SpecSource:
		return sourceMatches(spec.fieldString("value"), attrs)
	case SpecQualityModifier:
		return spec.fieldString("value") == attrs.Modifier
	case SpecReleaseType:
		return spec.fieldString("value") == attrs.ReleaseType
	case SpecLanguage:
		return languageMatches(spec, attrs)
	case SpecSize:
		if attrs.SizeBytes <= 0 {
			return false
		}
		minGB, _ := spec.fieldFloat("min")
		maxGB, _ := spec.fieldFloat("max")
		gb := float64(attrs.SizeBytes) / (1024 * 1024 * 1024)
		return gb >= minGB && (maxGB <= 0 || gb <= maxGB)
	case SpecYear:
		if attrs.Year == 0 {
			return false
		}
		minYear, _ := spec.fieldInt("min")
		maxYear, _ := spec.fieldInt("max")
		return attrs.Year >= minYear && (maxYear <= 0 || attrs.Year <= maxYear)
	default:
		// IndexerFlagSpecification and anything newer: not tracked yet.
		return false
	}
}

func sourceMatches(want string, attrs Attrs) bool {
	switch want {
	case "remux":
		return attrs.Modifier == "remux"
	case "rawhd":
		return attrs.Modifier == "rawhd"
	case "unknown":
		return len(attrs.Sources) == 0
	default:
		return contains(attrs.Sources, want)
	}
}

func languageMatches(spec CustomFormatSpec, attrs Attrs) bool {
	want := spec.fieldString("value")
	if want == "any" {
		return true
	}
	if want == "original" {
		want = attrs.OriginalLanguage
		if want == "" {
			want = "english"
		}
	}
	langs := attrs.effectiveLanguages()
	if want == "unknown" {
		return len(attrs.Languages) == 0
	}
	if spec.fieldBool("except") {
		for _, lang := range langs {
			if lang != want {
				return true
			}
		}
		return false
	}
	return contains(langs, want)
}
