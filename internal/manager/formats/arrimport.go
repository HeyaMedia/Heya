package formats

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The arr apps store spec values as app-local numeric enums that disagree
// with each other (Sonarr's source 7 is a remux, Radarr's is WEBDL). Import
// resolves them against per-app tables so what lands in the database is
// unambiguous. TRaSH guide JSON uses the same numbering as the app it
// targets, so `kind` covers both a live API payload and a pasted guide.

var radarrSources = map[int]string{0: "unknown", 1: "cam", 2: "telesync", 3: "telecine", 4: "workprint", 5: "dvd", 6: "tv", 7: "webdl", 8: "webrip", 9: "bluray"}

var sonarrSources = map[int]string{0: "unknown", 1: "tv", 2: "rawhd", 3: "webdl", 4: "webrip", 5: "dvd", 6: "bluray", 7: "remux"}

var qualityModifiers = map[int]string{0: "none", 1: "regional", 2: "screener", 3: "rawhd", 4: "brdisk", 5: "remux"}

var releaseTypes = map[int]string{0: "unknown", 1: "single", 2: "multi", 3: "season-pack"}

var radarrLanguages = map[int]string{
	-2: "original", -1: "any", 0: "unknown", 1: "english", 2: "french", 3: "spanish",
	4: "german", 5: "italian", 6: "danish", 7: "dutch", 8: "japanese", 9: "icelandic",
	10: "chinese", 11: "russian", 12: "polish", 13: "vietnamese", 14: "swedish",
	15: "norwegian", 16: "finnish", 17: "turkish", 18: "portuguese", 19: "flemish",
	20: "greek", 21: "korean", 22: "hungarian", 23: "hebrew", 24: "lithuanian",
	25: "czech", 26: "hindi", 27: "romanian", 28: "thai", 29: "bulgarian",
	30: "portuguese (brazil)", 31: "arabic", 32: "ukrainian", 33: "persian",
	34: "bengali", 35: "slovak", 36: "latvian", 37: "spanish (latino)", 38: "catalan",
	39: "croatian", 40: "serbian", 41: "bosnian", 42: "estonian", 43: "tamil",
	44: "indonesian", 45: "telugu", 46: "macedonian", 47: "slovenian", 48: "malayalam",
	49: "kannada", 50: "albanian", 51: "afrikaans", 52: "marathi", 53: "tagalog",
	54: "urdu", 55: "romansh", 56: "mongolian", 57: "georgian",
}

var sonarrLanguages = map[int]string{
	-2: "original", -1: "any", 0: "unknown", 1: "english", 2: "french", 3: "spanish",
	4: "german", 5: "italian", 6: "danish", 7: "dutch", 8: "japanese", 9: "icelandic",
	10: "chinese", 11: "russian", 12: "polish", 13: "vietnamese", 14: "swedish",
	15: "norwegian", 16: "finnish", 17: "turkish", 18: "portuguese", 19: "flemish",
	20: "greek", 21: "korean", 22: "hungarian", 23: "hebrew", 24: "lithuanian",
	25: "czech", 26: "arabic", 27: "hindi", 28: "bulgarian", 29: "malayalam",
	30: "ukrainian", 31: "slovak", 32: "thai", 33: "portuguese (brazil)",
	34: "spanish (latino)", 35: "romanian", 36: "latvian", 37: "persian",
	38: "catalan", 39: "croatian", 40: "serbian", 41: "bosnian", 42: "estonian",
	43: "tamil", 44: "indonesian", 45: "macedonian", 46: "slovenian",
}

// ImportedFormat is a custom format normalized out of an arr payload or a
// TRaSH guide JSON, ready for storage.
type ImportedFormat struct {
	Name                string
	IncludeWhenRenaming bool
	TrashID             string
	TrashScores         map[string]int
	Specs               []CustomFormatSpec
	Warnings            []string
}

type arrFormat struct {
	Name                string         `json:"name"`
	IncludeWhenRenaming bool           `json:"includeCustomFormatWhenRenaming"`
	TrashID             string         `json:"trash_id"`
	TrashScores         map[string]int `json:"trash_scores"`
	Specifications      []arrSpec      `json:"specifications"`
}

type arrSpec struct {
	Name           string          `json:"name"`
	Implementation string          `json:"implementation"`
	Negate         bool            `json:"negate"`
	Required       bool            `json:"required"`
	Fields         json.RawMessage `json:"fields"`
}

type arrField struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

// fieldValues flattens either fields shape — the live API's array form or
// the TRaSH/export object form — into one map.
func (s arrSpec) fieldValues() (map[string]any, error) {
	if len(s.Fields) == 0 {
		return map[string]any{}, nil
	}
	var asArray []arrField
	if err := json.Unmarshal(s.Fields, &asArray); err == nil {
		values := make(map[string]any, len(asArray))
		for _, field := range asArray {
			values[field.Name] = field.Value
		}
		return values, nil
	}
	var asObject map[string]any
	if err := json.Unmarshal(s.Fields, &asObject); err != nil {
		return nil, fmt.Errorf("unrecognized fields shape: %w", err)
	}
	return asObject, nil
}

// ParseArrFormats normalizes a custom-format payload from the given app kind
// ('radarr', 'sonarr', or 'lidarr'). Accepts a JSON array or a single object.
func ParseArrFormats(kind string, raw []byte) ([]ImportedFormat, error) {
	trimmed := strings.TrimSpace(string(raw))
	var payload []arrFormat
	if strings.HasPrefix(trimmed, "{") {
		var single arrFormat
		if err := json.Unmarshal(raw, &single); err != nil {
			return nil, fmt.Errorf("parsing custom format JSON: %w", err)
		}
		payload = []arrFormat{single}
	} else if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parsing custom format JSON: %w", err)
	}

	imported := make([]ImportedFormat, 0, len(payload))
	for _, format := range payload {
		if format.Name == "" {
			continue
		}
		result := ImportedFormat{
			Name:                format.Name,
			IncludeWhenRenaming: format.IncludeWhenRenaming,
			TrashID:             format.TrashID,
			TrashScores:         format.TrashScores,
		}
		for _, spec := range format.Specifications {
			normalized, warning, err := normalizeSpec(kind, spec)
			if err != nil {
				return nil, fmt.Errorf("format %q, condition %q: %w", format.Name, spec.Name, err)
			}
			if warning != "" {
				result.Warnings = append(result.Warnings, warning)
			}
			result.Specs = append(result.Specs, normalized)
		}
		imported = append(imported, result)
	}
	return imported, nil
}

func normalizeSpec(kind string, spec arrSpec) (CustomFormatSpec, string, error) {
	values, err := spec.fieldValues()
	if err != nil {
		return CustomFormatSpec{}, "", err
	}
	normalized := CustomFormatSpec{
		Name:           spec.Name,
		Implementation: spec.Implementation,
		Negate:         spec.Negate,
		Required:       spec.Required,
		Fields:         map[string]any{},
	}
	var warning string

	switch spec.Implementation {
	case SpecReleaseTitle, SpecReleaseGroup, SpecEdition:
		value, _ := values["value"].(string)
		normalized.Fields["value"] = value
	case SpecResolution:
		normalized.Fields["value"] = values["value"]
	case SpecSource:
		table := radarrSources
		if kind == "sonarr" {
			table = sonarrSources
		}
		normalized.Fields["value"] = resolveEnum(values["value"], table)
	case SpecQualityModifier:
		normalized.Fields["value"] = resolveEnum(values["value"], qualityModifiers)
	case SpecReleaseType:
		normalized.Fields["value"] = resolveEnum(values["value"], releaseTypes)
	case SpecLanguage:
		table := radarrLanguages
		if kind == "sonarr" {
			table = sonarrLanguages
		}
		normalized.Fields["value"] = resolveEnum(values["value"], table)
		if except, ok := values["exceptLanguage"].(bool); ok && except {
			normalized.Fields["except"] = true
		}
	case SpecSize, SpecYear:
		normalized.Fields["min"] = values["min"]
		normalized.Fields["max"] = values["max"]
	case SpecIndexerFlag:
		normalized.Fields["value"] = values["value"]
		warning = fmt.Sprintf("condition %q: indexer flags are not tracked yet; this condition will not match", spec.Name)
	default:
		normalized.Fields = values
		warning = fmt.Sprintf("condition %q: unsupported implementation %s; this condition will not match", spec.Name, spec.Implementation)
	}

	return normalized, warning, nil
}

// resolveEnum maps an app-numeric enum to its canonical string; canonical
// strings (from a re-import of our own export) pass through untouched.
func resolveEnum(value any, table map[int]string) any {
	switch v := value.(type) {
	case float64:
		if name, ok := table[int(v)]; ok {
			return name
		}
		return "unknown"
	case string:
		return strings.ToLower(v)
	default:
		return "unknown"
	}
}
