package scanner

import (
	"context"
	"sort"
	"strings"
)

type MovieMatch struct {
	Key         string            `json:"key"`
	KeyType     string            `json:"key_type"`
	Title       string            `json:"title"`
	Year        string            `json:"year,omitempty"`
	Aliases     []string          `json:"aliases,omitempty"`
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
	Confidence  float64           `json:"confidence"`
	Evidence    []string          `json:"evidence,omitempty"`
	Plans       []MoviePlan       `json:"plans"`
	Files       []string          `json:"files"`
	Assets      []MovieAssetPlan  `json:"assets,omitempty"`
	NFOs        []string          `json:"nfos,omitempty"`
	Issues      []string          `json:"issues,omitempty"`
}

func AnalyzeMovieMatches(ctx context.Context, plans []MoviePlan, emit Emitter) ([]MovieMatch, error) {
	grouped := map[string]*MovieMatch{}
	for _, plan := range plans {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		key, keyType := movieMatchKey(plan)
		if key == "" {
			emit.Emit(Event{
				Event:    "match.movie.unmatched",
				Severity: SeverityWarn,
				Kind:     "movie",
				Reason:   "no_identity_key",
				Message:  "movie plan has no external id, title/year, or title-only identity key",
				Data: map[string]any{
					"title": plan.Title,
					"year":  plan.Year,
					"files": plan.Files,
				},
			})
			continue
		}

		match := grouped[key]
		if match == nil {
			match = &MovieMatch{
				Key:         key,
				KeyType:     keyType,
				Title:       plan.Title,
				Year:        plan.Year,
				ExternalIDs: map[string]string{},
			}
			grouped[key] = match
		}
		mergeMoviePlan(match, plan)
	}

	matches := make([]MovieMatch, 0, len(grouped))
	for _, match := range grouped {
		match.Evidence = sortedUnique(match.Evidence)
		match.Aliases = sortedMovieAliases(match.Title, match.Aliases)
		match.Files = sortedUnique(match.Files)
		match.NFOs = sortedUnique(match.NFOs)
		match.Issues = sortedUnique(match.Issues)
		sort.Slice(match.Assets, func(i, j int) bool {
			if match.Assets[i].RelPath == match.Assets[j].RelPath {
				return match.Assets[i].Type < match.Assets[j].Type
			}
			return match.Assets[i].RelPath < match.Assets[j].RelPath
		})
		match.Confidence = movieMatchConfidence(*match)
		matches = append(matches, *match)
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Title == matches[j].Title {
			if matches[i].Year == matches[j].Year {
				return matches[i].Key < matches[j].Key
			}
			return matches[i].Year < matches[j].Year
		}
		return matches[i].Title < matches[j].Title
	})

	for _, match := range matches {
		event := "match.movie.identity"
		if len(match.Plans) > 1 {
			event = "match.movie.grouped"
		}
		emit.Emit(Event{
			Event: event,
			Kind:  "movie",
			Data: map[string]any{
				"key":          match.Key,
				"key_type":     match.KeyType,
				"title":        match.Title,
				"year":         match.Year,
				"confidence":   match.Confidence,
				"plans":        len(match.Plans),
				"files":        len(match.Files),
				"external_ids": match.ExternalIDs,
				"evidence":     match.Evidence,
				"issues":       match.Issues,
			},
		})
	}
	emit.Emit(Event{Event: "match.summary", Data: map[string]any{"domain": "movie", "matches": len(matches), "plans": len(plans)}})
	return matches, nil
}

func mergeMoviePlan(match *MovieMatch, plan MoviePlan) {
	match.Plans = append(match.Plans, plan)
	match.Files = append(match.Files, plan.Files...)
	match.Aliases = append(match.Aliases, moviePlanTitleAliases(plan)...)
	match.Assets = append(match.Assets, plan.Assets...)
	if plan.NFO != "" {
		match.NFOs = append(match.NFOs, plan.NFO)
	}
	if plan.Source != "" {
		match.Evidence = append(match.Evidence, "source:"+plan.Source)
	}
	if plan.Year != "" {
		match.Evidence = append(match.Evidence, "year")
	}
	if len(plan.ExternalIDs) > 0 {
		match.Evidence = append(match.Evidence, "external_id")
	}
	for key, value := range plan.ExternalIDs {
		if value == "" {
			continue
		}
		if existing := match.ExternalIDs[key]; existing != "" && existing != value {
			match.Issues = append(match.Issues, "conflicting_"+key+"_id")
			continue
		}
		match.ExternalIDs[key] = value
	}
}

func moviePlanTitleAliases(plan MoviePlan) []string {
	var aliases []string
	for _, file := range plan.Files {
		aliases = append(aliases, movieTitleAliasesFromPath(file, plan.Year)...)
	}
	return aliases
}

func movieTitleAliasesFromPath(relPath, year string) []string {
	if year == "" {
		return nil
	}
	segments := []string{
		strings.TrimSuffix(filepathBase(relPath), filepathExt(relPath)),
		filepathBase(filepathDir(relPath)),
	}
	var aliases []string
	for _, segment := range segments {
		if alias := titleBeforeYear(segment, year); alias != "" {
			aliases = append(aliases, alias)
		}
	}
	return aliases
}

func titleBeforeYear(segment, year string) string {
	idx := strings.Index(segment, year)
	if idx <= 0 {
		return ""
	}
	title := segment[:idx]
	title = strings.TrimRight(title, " ._-([{")
	title = strings.NewReplacer(".", " ", "_", " ").Replace(title)
	return strings.Join(strings.Fields(title), " ")
}

func sortedMovieAliases(title string, aliases []string) []string {
	titleNorm := normalizeSearchTitle(title)
	seen := map[string]bool{}
	var out []string
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		norm := normalizeSearchTitle(alias)
		if alias == "" || norm == "" || norm == titleNorm || seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, alias)
	}
	sort.Slice(out, func(i, j int) bool {
		return normalizeSearchTitle(out[i]) < normalizeSearchTitle(out[j])
	})
	return out
}

func filepathBase(path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return path
	}
	return path[i+1:]
}

func filepathDir(path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return "."
	}
	return path[:i]
}

func filepathExt(path string) string {
	base := filepathBase(path)
	i := strings.LastIndex(base, ".")
	if i < 0 {
		return ""
	}
	return base[i:]
}

func movieMatchKey(plan MoviePlan) (string, string) {
	for _, provider := range []string{"tmdb", "imdb", "tvdb"} {
		if value := strings.TrimSpace(plan.ExternalIDs[provider]); value != "" {
			return provider + ":" + strings.ToLower(value), provider
		}
	}
	title := normalizeIdentityTitle(plan.Title)
	if title == "" {
		return "", ""
	}
	year := strings.TrimSpace(plan.Year)
	if year != "" {
		return "title_year:" + title + "|" + year, "title_year"
	}
	return "title:" + title, "title"
}

func movieMatchConfidence(match MovieMatch) float64 {
	switch match.KeyType {
	case "tmdb", "imdb", "tvdb":
		if contains(match.Evidence, "source:nfo") {
			return 0.99
		}
		return 0.96
	case "title_year":
		if contains(match.Evidence, "source:nfo") {
			return 0.9
		}
		return 0.82
	case "title":
		return 0.45
	default:
		return 0
	}
}

func contains(values []string, needle string) bool {
	for _, v := range values {
		if v == needle {
			return true
		}
	}
	return false
}

func sortedUnique(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
