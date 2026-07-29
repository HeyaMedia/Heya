// Package formats implements TRaSH-compatible custom formats: release
// conditions grouped into named formats, which quality profiles score to
// decide which release wins. The condition vocabulary and matching semantics
// follow Radarr/Sonarr so the ecosystem's tuned format libraries import
// unchanged, but stored field values are canonical strings — the arr apps'
// diverging numeric enums are resolved once, at import time.
package formats

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Implementation names follow the arr vocabulary verbatim so imported
// formats read the same here as at the source.
const (
	SpecReleaseTitle    = "ReleaseTitleSpecification"
	SpecReleaseGroup    = "ReleaseGroupSpecification"
	SpecEdition         = "EditionSpecification"
	SpecLanguage        = "LanguageSpecification"
	SpecResolution      = "ResolutionSpecification"
	SpecSource          = "SourceSpecification"
	SpecQualityModifier = "QualityModifierSpecification"
	SpecReleaseType     = "ReleaseTypeSpecification"
	SpecSize            = "SizeSpecification"
	SpecYear            = "YearSpecification"
	SpecIndexerFlag     = "IndexerFlagSpecification"
)

// Canonical source values. "remux" and "rawhd" are modifiers in Radarr but
// distinct sources in Sonarr; folding both into the source vocabulary lets
// one evaluator serve formats imported from either app.
var Sources = []string{"cam", "telesync", "telecine", "workprint", "dvd", "tv", "rawhd", "webdl", "webrip", "bluray", "remux", "screener", "ppv"}

var Modifiers = []string{"none", "regional", "screener", "rawhd", "brdisk", "remux"}

var ReleaseTypes = []string{"single", "multi", "season-pack"}

var Resolutions = []int{360, 480, 540, 576, 720, 1080, 2160}

// CustomFormatSpec is one condition inside a custom format. Fields is object-form:
// {"value": …} for most implementations, plus "except" for language and
// "min"/"max" for size and year ranges.
type CustomFormatSpec struct {
	Name           string         `json:"name"`
	Implementation string         `json:"implementation"`
	Negate         bool           `json:"negate"`
	Required       bool           `json:"required"`
	Fields         map[string]any `json:"fields"`
}

// Format is a named bundle of conditions. A release matches the format when
// every implementation group is satisfied (see Matches).
type Format struct {
	ID    int64
	Name  string
	Specs []CustomFormatSpec
}

func (s CustomFormatSpec) fieldString(key string) string {
	switch v := s.Fields[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

func (s CustomFormatSpec) fieldFloat(key string) (float64, bool) {
	switch v := s.Fields[key].(type) {
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func (s CustomFormatSpec) fieldInt(key string) (int, bool) {
	f, ok := s.fieldFloat(key)
	return int(f), ok
}

func (s CustomFormatSpec) fieldBool(key string) bool {
	b, _ := s.Fields[key].(bool)
	return b
}

// Validate rejects specs the evaluator cannot honor, so a broken condition
// surfaces at save time instead of silently never matching.
func (s CustomFormatSpec) Validate() error {
	switch s.Implementation {
	case SpecReleaseTitle, SpecReleaseGroup, SpecEdition:
		expr := s.fieldString("value")
		if expr == "" {
			return fmt.Errorf("%s: empty regular expression", s.Implementation)
		}
		if _, err := compiledRegex(expr); err != nil {
			return fmt.Errorf("%s: invalid regular expression %q: %w", s.Implementation, expr, err)
		}
	case SpecResolution:
		v, ok := s.fieldInt("value")
		if !ok || v < 0 {
			return fmt.Errorf("%s: value must be a resolution height", s.Implementation)
		}
	case SpecSource:
		if !contains(Sources, s.fieldString("value")) && s.fieldString("value") != "unknown" {
			return fmt.Errorf("%s: unknown source %q", s.Implementation, s.fieldString("value"))
		}
	case SpecQualityModifier:
		if !contains(Modifiers, s.fieldString("value")) {
			return fmt.Errorf("%s: unknown modifier %q", s.Implementation, s.fieldString("value"))
		}
	case SpecReleaseType:
		if !contains(ReleaseTypes, s.fieldString("value")) && s.fieldString("value") != "unknown" {
			return fmt.Errorf("%s: unknown release type %q", s.Implementation, s.fieldString("value"))
		}
	case SpecLanguage:
		if s.fieldString("value") == "" {
			return fmt.Errorf("%s: empty language", s.Implementation)
		}
	case SpecSize, SpecYear:
		if _, ok := s.fieldFloat("min"); !ok {
			return fmt.Errorf("%s: min is required", s.Implementation)
		}
		if _, ok := s.fieldFloat("max"); !ok {
			return fmt.Errorf("%s: max is required", s.Implementation)
		}
	case SpecIndexerFlag:
		// Accepted for import fidelity; evaluates to no-match until indexer
		// flags are tracked.
	default:
		return fmt.Errorf("unknown specification implementation %q", s.Implementation)
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
