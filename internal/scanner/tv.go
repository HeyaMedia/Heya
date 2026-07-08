package scanner

import (
	"context"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/nfo"
	"github.com/karbowiak/heya/internal/parser"
)

type TVPlan struct {
	Title            string            `json:"title"`
	Year             string            `json:"year,omitempty"`
	Source           string            `json:"source"`
	ExternalIDs      map[string]string `json:"external_ids,omitempty"`
	Season           int               `json:"season,omitempty"`
	Episodes         []int             `json:"episodes,omitempty"`
	AbsoluteEpisodes []int             `json:"absolute_episodes,omitempty"`
	Files            []string          `json:"files"`
	Assets           []TVAssetPlan     `json:"assets,omitempty"`
	Subtitles        []string          `json:"subtitles,omitempty"`
	NFO              string            `json:"nfo,omitempty"`
	Plexmatch        string            `json:"plexmatch,omitempty"`
}

type TVAssetPlan struct {
	Type    string `json:"type"`
	RelPath string `json:"rel_path"`
}

type TVMatch struct {
	Key         string            `json:"key"`
	KeyType     string            `json:"key_type"`
	Title       string            `json:"title"`
	Year        string            `json:"year,omitempty"`
	Aliases     []string          `json:"aliases,omitempty"`
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
	Confidence  float64           `json:"confidence"`
	Evidence    []string          `json:"evidence,omitempty"`
	Plans       []TVPlan          `json:"plans"`
	Files       []string          `json:"files"`
	Episodes    []TVEpisodeRef    `json:"episodes,omitempty"`
	Assets      []TVAssetPlan     `json:"assets,omitempty"`
	Subtitles   []string          `json:"subtitles,omitempty"`
	NFOs        []string          `json:"nfos,omitempty"`
	Plexmatches []string          `json:"plexmatches,omitempty"`
	Issues      []string          `json:"issues,omitempty"`
}

type TVEpisodeRef struct {
	Season   int `json:"season,omitempty"`
	Episode  int `json:"episode,omitempty"`
	Absolute int `json:"absolute,omitempty"`
}

type tvNFOEntry struct {
	file InventoryFile
	nfo  *nfo.ParsedNFO
}

type tvPlexmatchEntry struct {
	file  InventoryFile
	match tvPlexmatch
}

type tvPlexmatch struct {
	Title string
	Year  string
	IDs   map[string]string
}

type tvIdent struct {
	title  string
	year   string
	source string
	ids    map[string]string
}

type tvAnalyzeConfig struct {
	domain            string
	forceAnimeContext bool
}

func AnalyzeTV(ctx context.Context, inv Inventory, emit Emitter) ([]TVPlan, error) {
	return analyzeTVLike(ctx, inv, emit, tvAnalyzeConfig{domain: "tv"})
}

func AnalyzeAnime(ctx context.Context, inv Inventory, emit Emitter) ([]TVPlan, error) {
	return analyzeTVLike(ctx, inv, emit, tvAnalyzeConfig{domain: "anime", forceAnimeContext: true})
}

func analyzeTVLike(ctx context.Context, inv Inventory, emit Emitter, cfg tvAnalyzeConfig) ([]TVPlan, error) {
	var plans []TVPlan
	for _, root := range inv.Roots {
		if err := ctx.Err(); err != nil {
			return plans, err
		}

		nfos := parseTVNFOs(root, emit)
		plexmatches := parseTVPlexmatches(root, emit)
		assetsByDir := groupTVAssets(root.Files)
		for _, f := range root.Files {
			if f.Class != ClassPrimaryMedia {
				continue
			}

			release := parseTVReleaseFromFile(f, cfg)
			emitTVParse(f, release, cfg.domain, emit)

			showDir := tvShowDir(f.RelPath, nfos, plexmatches)
			nfoEntry, hasNFO := nearestTVNFO(showDir, nfos)
			plexEntry, hasPlexmatch := nearestTVPlexmatch(showDir, plexmatches)
			folder := parseTVShowFolder(showDir)
			identity, ok := tvIdentity(f.RelPath, release, folder, nfoEntry.nfo, plexEntry.match)
			if !ok {
				emit.Emit(Event{
					Event:    cfg.domain + ".file.unplanned",
					Severity: SeverityWarn,
					Root:     root.Root,
					Path:     f.Path,
					RelPath:  f.RelPath,
					Reason:   "no_show_identity",
					Message:  "file classified as media but no " + cfg.domain + " show identity could be parsed",
				})
				continue
			}

			season, episodes, absoluteEpisodes := tvEpisodeIdentity(f.RelPath, release)
			if len(episodes) == 0 && len(absoluteEpisodes) == 0 {
				emit.Emit(Event{
					Event:    cfg.domain + ".file.unplanned",
					Severity: SeverityWarn,
					Root:     root.Root,
					Path:     f.Path,
					RelPath:  f.RelPath,
					Reason:   "no_episode_identity",
					Message:  "file has a " + cfg.domain + " show identity but no episode or absolute episode number",
					Data: map[string]any{
						"title": identity.title,
						"year":  identity.year,
					},
				})
				continue
			}

			plan := TVPlan{
				Title:            identity.title,
				Year:             identity.year,
				Source:           identity.source,
				ExternalIDs:      identity.ids,
				Season:           season,
				Episodes:         episodes,
				AbsoluteEpisodes: absoluteEpisodes,
				Files:            []string{f.RelPath},
				Assets:           tvPlanAssets(assetsByDir, showDir, filepath.Dir(f.RelPath)),
				Subtitles:        findTVSubtitles(root.Files, f, showDir),
			}
			if hasNFO {
				plan.NFO = nfoEntry.file.RelPath
				emit.Emit(Event{
					Event:   cfg.domain + ".nfo.applied",
					Root:    root.Root,
					Path:    nfoEntry.file.Path,
					RelPath: nfoEntry.file.RelPath,
					Kind:    "tvshow",
					Data: map[string]any{
						"file":  f.RelPath,
						"title": nfoEntry.nfo.Title,
						"year":  nfoEntry.nfo.Year,
						"ids":   identity.ids,
					},
				})
			}
			if hasPlexmatch {
				plan.Plexmatch = plexEntry.file.RelPath
				emit.Emit(Event{
					Event:   cfg.domain + ".plexmatch.applied",
					Root:    root.Root,
					Path:    plexEntry.file.Path,
					RelPath: plexEntry.file.RelPath,
					Kind:    "plexmatch",
					Data: map[string]any{
						"file":  f.RelPath,
						"title": plexEntry.match.Title,
						"year":  plexEntry.match.Year,
						"ids":   plexEntry.match.IDs,
					},
				})
			}

			plans = append(plans, plan)
			emit.Emit(Event{
				Event:   "plan." + cfg.domain + "_episode",
				Root:    root.Root,
				Path:    f.Path,
				RelPath: f.RelPath,
				Kind:    "would_materialize_" + cfg.domain + "_episode",
				Data: map[string]any{
					"title":             plan.Title,
					"year":              plan.Year,
					"source":            plan.Source,
					"season":            plan.Season,
					"episodes":          plan.Episodes,
					"absolute_episodes": plan.AbsoluteEpisodes,
					"external_ids":      plan.ExternalIDs,
					"assets":            len(plan.Assets),
					"subtitles":         len(plan.Subtitles),
				},
			})
		}
	}
	sortTVPlans(plans)
	emit.Emit(Event{Event: "domain.summary", Data: map[string]any{"domain": cfg.domain, "plans": len(plans)}})
	return plans, nil
}

func AnalyzeTVMatches(ctx context.Context, plans []TVPlan, emit Emitter) ([]TVMatch, error) {
	return analyzeTVLikeMatches(ctx, plans, emit, "tv")
}

func AnalyzeAnimeMatches(ctx context.Context, plans []TVPlan, emit Emitter) ([]TVMatch, error) {
	return analyzeTVLikeMatches(ctx, plans, emit, "anime")
}

func analyzeTVLikeMatches(ctx context.Context, plans []TVPlan, emit Emitter, domain string) ([]TVMatch, error) {
	grouped := map[string]*TVMatch{}
	for _, plan := range plans {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		key, keyType := tvMatchKey(plan)
		if key == "" {
			emit.Emit(Event{
				Event:    "match." + domain + ".unmatched",
				Severity: SeverityWarn,
				Kind:     domain,
				Reason:   "no_identity_key",
				Message:  domain + " plan has no external id, title/year, or title-only identity key",
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
			match = &TVMatch{
				Key:         key,
				KeyType:     keyType,
				Title:       plan.Title,
				Year:        plan.Year,
				ExternalIDs: map[string]string{},
			}
			grouped[key] = match
		}
		mergeTVPlan(match, plan)
	}

	matches := make([]TVMatch, 0, len(grouped))
	for _, match := range grouped {
		match.Evidence = sortedUnique(match.Evidence)
		match.Aliases = sortedTVAliases(match.Title, match.Aliases)
		match.Files = sortedUnique(match.Files)
		match.Subtitles = sortedUnique(match.Subtitles)
		match.NFOs = sortedUnique(match.NFOs)
		match.Plexmatches = sortedUnique(match.Plexmatches)
		match.Issues = sortedUnique(match.Issues)
		sort.Slice(match.Assets, func(i, j int) bool {
			if match.Assets[i].RelPath == match.Assets[j].RelPath {
				return match.Assets[i].Type < match.Assets[j].Type
			}
			return match.Assets[i].RelPath < match.Assets[j].RelPath
		})
		match.Assets = uniqueTVAssets(match.Assets)
		sort.Slice(match.Episodes, func(i, j int) bool {
			if match.Episodes[i].Season == match.Episodes[j].Season {
				if match.Episodes[i].Episode == match.Episodes[j].Episode {
					return match.Episodes[i].Absolute < match.Episodes[j].Absolute
				}
				return match.Episodes[i].Episode < match.Episodes[j].Episode
			}
			return match.Episodes[i].Season < match.Episodes[j].Season
		})
		match.Episodes = uniqueTVEpisodeRefs(match.Episodes)
		match.Confidence = tvMatchConfidence(*match)
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
		event := "match." + domain + ".identity"
		if len(match.Plans) > 1 {
			event = "match." + domain + ".grouped"
		}
		emit.Emit(Event{
			Event: event,
			Kind:  domain,
			Data: map[string]any{
				"key":          match.Key,
				"key_type":     match.KeyType,
				"title":        match.Title,
				"year":         match.Year,
				"confidence":   match.Confidence,
				"plans":        len(match.Plans),
				"files":        len(match.Files),
				"episodes":     len(match.Episodes),
				"external_ids": match.ExternalIDs,
				"evidence":     match.Evidence,
				"issues":       match.Issues,
			},
		})
	}
	emit.Emit(Event{Event: "match.summary", Data: map[string]any{"domain": domain, "matches": len(matches), "plans": len(plans)}})
	return matches, nil
}

func parseTVReleaseFromFile(file InventoryFile, cfg tvAnalyzeConfig) *parser.SceneReleaseParse {
	parseOpts := parser.ParseOptions{ForceAnimeContext: cfg.forceAnimeContext}
	release := parser.ParseSceneReleaseNameWithOptions(file.Name, parser.MediaVideo, parseOpts)
	if release != nil && release.IsTv {
		imdb, tmdb, tvdb := parser.ParseProviderIDs(file.RelPath)
		anidb, anilist, mal := parser.ParseAnimeIDs(file.RelPath)
		if release.ImdbID == "" {
			release.ImdbID = imdb
		}
		if release.TmdbID == "" {
			release.TmdbID = tmdb
		}
		if release.TvdbID == "" {
			release.TvdbID = tvdb
		}
		if release.AnidbID == "" {
			release.AnidbID = anidb
		}
		if release.AnilistID == "" {
			release.AnilistID = anilist
		}
		if release.MalID == "" {
			release.MalID = mal
		}
		return release
	}

	parsed := parser.ParseStoragePathWithOptions(file.RelPath, parseOpts)
	if parsed.Release != nil && parsed.Release.IsTv && tvPathReleaseIsSafe(file.RelPath, parsed) {
		return parsed.Release
	}
	return nil
}

func tvPathReleaseIsSafe(relPath string, parsed parser.ParsedStorageEntry) bool {
	if releaseFromLeaf(parsed) {
		return true
	}
	return isSeasonDir(filepath.Base(filepath.Dir(relPath)))
}

func emitTVParse(file InventoryFile, release *parser.SceneReleaseParse, domain string, emit Emitter) {
	data := map[string]any{"media": string(parser.MediaVideo)}
	if release != nil {
		data["title"] = release.Title
		data["year"] = release.Year
		data["strategy"] = string(release.Strategy)
		data["score"] = release.Score
		data["ids"] = idsFromRelease(release)
		data["season"] = release.Seasons
		data["episodes"] = release.Episodes
		data["absolute_episodes"] = release.AbsoluteEpisodes
	}
	emit.Emit(Event{Event: "parse.result", Root: file.Root, Path: file.Path, RelPath: file.RelPath, Kind: domain, Data: data})
}

func parseTVNFOs(root InventoryRoot, emit Emitter) map[string]tvNFOEntry {
	out := make(map[string]tvNFOEntry)
	for _, f := range root.Files {
		if f.Class != ClassNFO || f.Kind != "tvshow" {
			continue
		}
		parsed := nfo.ParseFile(root.FS, f.RelPath, "tvshow")
		if parsed == nil {
			emit.Emit(Event{Event: "nfo.parse_failed", Severity: SeverityWarn, Root: root.Root, Path: f.Path, RelPath: f.RelPath, Kind: "tvshow"})
			continue
		}
		dir := filepath.Dir(f.RelPath)
		out[dir] = tvNFOEntry{file: f, nfo: parsed}
		emit.Emit(Event{
			Event:   "nfo.parsed",
			Root:    root.Root,
			Path:    f.Path,
			RelPath: f.RelPath,
			Kind:    "tvshow",
			Data: map[string]any{
				"title": parsed.Title,
				"year":  parsed.Year,
				"ids":   idsFromNFO(parsed),
			},
		})
	}
	return out
}

func parseTVPlexmatches(root InventoryRoot, emit Emitter) map[string]tvPlexmatchEntry {
	out := make(map[string]tvPlexmatchEntry)
	for _, f := range root.Files {
		if f.Class != ClassPlexmatch {
			continue
		}
		data, err := fs.ReadFile(root.FS, f.RelPath)
		if err != nil {
			emit.Emit(Event{Event: "plexmatch.parse_failed", Severity: SeverityWarn, Root: root.Root, Path: f.Path, RelPath: f.RelPath, Kind: "plexmatch", Message: err.Error()})
			continue
		}
		match := parsePlexmatchBytes(data)
		if match.Title == "" && match.Year == "" && len(match.IDs) == 0 {
			emit.Emit(Event{Event: "plexmatch.parse_failed", Severity: SeverityWarn, Root: root.Root, Path: f.Path, RelPath: f.RelPath, Kind: "plexmatch", Reason: "empty"})
			continue
		}
		dir := filepath.Dir(f.RelPath)
		out[dir] = tvPlexmatchEntry{file: f, match: match}
		emit.Emit(Event{
			Event:   "plexmatch.parsed",
			Root:    root.Root,
			Path:    f.Path,
			RelPath: f.RelPath,
			Kind:    "plexmatch",
			Data: map[string]any{
				"title": match.Title,
				"year":  match.Year,
				"ids":   match.IDs,
			},
		})
	}
	return out
}

func parsePlexmatchBytes(data []byte) tvPlexmatch {
	out := tvPlexmatch{IDs: map[string]string{}}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = plexmatchKey(key)
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch key {
		case "title":
			out.Title = value
		case "year":
			out.Year = yearString(value)
		case "imdb", "imdbid":
			out.IDs["imdb"] = value
		case "tmdb", "tmdbid":
			out.IDs["tmdb"] = value
		case "tvdb", "tvdbid":
			out.IDs["tvdb"] = value
		case "guid":
			mergeIDs(out.IDs, idsFromPlexmatchGuid(value))
		}
	}
	return out
}

func plexmatchKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(key)
	return key
}

func idsFromPlexmatchGuid(value string) map[string]string {
	ids := map[string]string{}
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == ' '
	}) {
		provider, id, ok := strings.Cut(part, "://")
		if !ok {
			provider, id, ok = strings.Cut(part, ":")
		}
		if !ok {
			continue
		}
		provider = strings.ToLower(strings.TrimSpace(provider))
		id = strings.TrimSpace(id)
		switch provider {
		case "imdb", "tmdb", "tvdb", "anidb", "anilist", "mal", "myanimelist":
			if id != "" {
				if provider == "myanimelist" {
					provider = "mal"
				}
				ids[provider] = id
			}
		}
	}
	return ids
}

func tvIdentity(relPath string, release *parser.SceneReleaseParse, folder tvFolderIdentity, localNFO *nfo.ParsedNFO, plex tvPlexmatch) (tvIdent, bool) {
	ident := tvIdent{ids: map[string]string{}}
	if release != nil {
		ident.title = cleanTVTitle(release.Title)
		ident.year = strings.TrimSpace(release.Year)
		ident.source = "filename"
		mergeIDs(ident.ids, idsFromRelease(release))
	}

	imdb, tmdb, tvdb := parser.ParseProviderIDs(relPath)
	mergeIDs(ident.ids, map[string]string{"imdb": imdb, "tmdb": tmdb, "tvdb": tvdb})
	anidb, anilist, mal := parser.ParseAnimeIDs(relPath)
	mergeIDs(ident.ids, map[string]string{"anidb": anidb, "anilist": anilist, "mal": mal})

	if folder.title != "" {
		if ident.title == "" || folder.year != "" {
			ident.title = folder.title
			if ident.source == "" {
				ident.source = "folder"
			}
		}
		if ident.year == "" && folder.year != "" {
			ident.year = folder.year
		}
		mergeIDs(ident.ids, folder.ids)
	}

	if plex.Title != "" {
		ident.title = cleanTVTitle(plex.Title)
		if ident.source == "" || ident.source == "filename" || ident.source == "folder" {
			ident.source = "plexmatch"
		}
	}
	if plex.Year != "" {
		ident.year = plex.Year
	}
	mergeIDs(ident.ids, plex.IDs)

	if localNFO != nil {
		if strings.TrimSpace(localNFO.Title) != "" {
			ident.title = cleanTVTitle(localNFO.Title)
			ident.source = "nfo"
		}
		if strings.TrimSpace(localNFO.Year) != "" {
			ident.year = strings.TrimSpace(localNFO.Year)
		}
		mergeIDs(ident.ids, idsFromNFO(localNFO))
	}

	if ident.source == "" {
		ident.source = "filename"
	}
	if ident.title == "" {
		return tvIdent{}, false
	}
	return ident, true
}

func tvEpisodeIdentity(relPath string, release *parser.SceneReleaseParse) (int, []int, []int) {
	var seasons []int
	var episodes []int
	var absoluteEpisodes []int
	if release != nil {
		seasons = append(seasons, release.Seasons...)
		episodes = append(episodes, release.Episodes...)
		absoluteEpisodes = append(absoluteEpisodes, release.AbsoluteEpisodes...)
	}
	season := 0
	if len(seasons) > 0 {
		season = seasons[0]
	} else if parsedSeason, ok := seasonFromPath(relPath); ok {
		season = parsedSeason
	}
	if len(episodes) == 0 {
		if parsedSeason, parsedEpisodes, ok := seasonEpisodesFromName(filepath.Base(relPath)); ok {
			if season == 0 || len(seasons) == 0 {
				season = parsedSeason
			}
			episodes = parsedEpisodes
		}
	}
	return season, uniqueInts(episodes), uniqueInts(absoluteEpisodes)
}

func mergeTVPlan(match *TVMatch, plan TVPlan) {
	match.Plans = append(match.Plans, plan)
	match.Files = append(match.Files, plan.Files...)
	match.Aliases = append(match.Aliases, tvPlanTitleAliases(plan)...)
	match.Assets = append(match.Assets, plan.Assets...)
	match.Subtitles = append(match.Subtitles, plan.Subtitles...)
	if plan.NFO != "" {
		match.NFOs = append(match.NFOs, plan.NFO)
	}
	if plan.Plexmatch != "" {
		match.Plexmatches = append(match.Plexmatches, plan.Plexmatch)
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
	match.Episodes = append(match.Episodes, tvEpisodeRefsForPlan(plan)...)
}

func tvEpisodeRefsForPlan(plan TVPlan) []TVEpisodeRef {
	if len(plan.Episodes) == 0 {
		refs := make([]TVEpisodeRef, 0, len(plan.AbsoluteEpisodes))
		for _, absolute := range plan.AbsoluteEpisodes {
			refs = append(refs, TVEpisodeRef{Absolute: absolute})
		}
		return refs
	}

	refs := make([]TVEpisodeRef, 0, len(plan.Episodes)+len(plan.AbsoluteEpisodes))
	pairedAbsolute := len(plan.AbsoluteEpisodes) == len(plan.Episodes)
	for idx, episode := range plan.Episodes {
		ref := TVEpisodeRef{Season: plan.Season, Episode: episode}
		if pairedAbsolute {
			ref.Absolute = plan.AbsoluteEpisodes[idx]
		}
		refs = append(refs, ref)
	}
	if !pairedAbsolute {
		for _, absolute := range plan.AbsoluteEpisodes {
			refs = append(refs, TVEpisodeRef{Absolute: absolute})
		}
	}
	return refs
}

func tvMatchKey(plan TVPlan) (string, string) {
	for _, provider := range []string{"tmdb", "tvdb", "imdb", "anidb", "mal", "anilist"} {
		if value := strings.TrimSpace(plan.ExternalIDs[provider]); value != "" {
			return provider + ":" + strings.ToLower(value), provider
		}
	}
	title := normalizeSearchTitle(plan.Title)
	if title == "" {
		return "", ""
	}
	year := strings.TrimSpace(plan.Year)
	if year != "" {
		return "title_year:" + title + "|" + year, "title_year"
	}
	return "title:" + title, "title"
}

func tvMatchConfidence(match TVMatch) float64 {
	switch match.KeyType {
	case "tmdb", "tvdb", "imdb", "anidb", "mal", "anilist":
		if contains(match.Evidence, "source:nfo") || contains(match.Evidence, "source:plexmatch") {
			return 0.99
		}
		return 0.96
	case "title_year":
		if contains(match.Evidence, "source:nfo") || contains(match.Evidence, "source:plexmatch") {
			return 0.9
		}
		return 0.82
	case "title":
		return 0.45
	default:
		return 0
	}
}

func tvPlanTitleAliases(plan TVPlan) []string {
	var aliases []string
	if alias := stripTrailingParenthetical(plan.Title); alias != "" {
		aliases = append(aliases, alias)
	}
	for _, file := range plan.Files {
		if alias := tvTitleAliasFromPath(file, plan.Year); alias != "" {
			aliases = append(aliases, alias)
			if stripped := stripTrailingParenthetical(alias); stripped != "" {
				aliases = append(aliases, stripped)
			}
		}
	}
	return aliases
}

func tvTitleAliasFromPath(relPath, year string) string {
	if year == "" {
		return ""
	}
	dir := filepath.Dir(relPath)
	if isSeasonDir(filepath.Base(dir)) {
		dir = filepath.Dir(dir)
	}
	base := filepath.Base(dir)
	if alias := titleBeforeYear(base, year); alias != "" {
		return cleanTVTitle(alias)
	}
	return ""
}

func stripTrailingParenthetical(title string) string {
	title = strings.TrimSpace(title)
	if !strings.HasSuffix(title, ")") {
		return ""
	}
	idx := strings.LastIndex(title, "(")
	if idx <= 0 {
		return ""
	}
	stripped := strings.TrimSpace(title[:idx])
	if stripped == "" || normalizeSearchTitle(stripped) == normalizeSearchTitle(title) {
		return ""
	}
	return stripped
}

func sortedTVAliases(title string, aliases []string) []string {
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

func sortTVPlans(plans []TVPlan) {
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].Title == plans[j].Title {
			if plans[i].Year == plans[j].Year {
				if plans[i].Season == plans[j].Season {
					return firstEpisode(plans[i]) < firstEpisode(plans[j])
				}
				return plans[i].Season < plans[j].Season
			}
			return plans[i].Year < plans[j].Year
		}
		return plans[i].Title < plans[j].Title
	})
}

func firstEpisode(plan TVPlan) int {
	if len(plan.Episodes) > 0 {
		return plan.Episodes[0]
	}
	if len(plan.AbsoluteEpisodes) > 0 {
		return plan.AbsoluteEpisodes[0]
	}
	return 0
}

type tvFolderIdentity struct {
	title string
	year  string
	ids   map[string]string
}

var (
	seasonDirRE      = regexp.MustCompile(`(?i)^(?:Season|Series|S)[ ._-]*(\d{1,2}|specials?)$`)
	seasonEpisodeRE  = regexp.MustCompile(`(?i)S(\d{1,2})E\d{1,3}(?:(?:E|-)\d{1,3})*`)
	tvShowFolderRE   = regexp.MustCompile(`^(.+?)\s*\((\d{4})\)(?:\s*\{.*\})?$`)
	tvReleaseTokenRE = regexp.MustCompile(`(?i)\bS\d{1,2}E\d{1,3}\b`)
)

func tvShowDir(relPath string, nfos map[string]tvNFOEntry, plexmatches map[string]tvPlexmatchEntry) string {
	dir := filepath.Dir(relPath)
	if dir == "." {
		return "."
	}
	for d := dir; ; d = filepath.Dir(d) {
		if _, ok := nfos[d]; ok {
			return d
		}
		if _, ok := plexmatches[d]; ok {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d || d == "." {
			break
		}
	}
	if isSeasonDir(filepath.Base(dir)) {
		parent := filepath.Dir(dir)
		if parent == "" {
			return "."
		}
		return parent
	}
	return dir
}

func parseTVShowFolder(dir string) tvFolderIdentity {
	if dir == "." || dir == "" {
		return tvFolderIdentity{ids: map[string]string{}}
	}
	base := filepath.Base(dir)
	if tvReleaseTokenRE.MatchString(base) {
		return tvFolderIdentity{ids: map[string]string{}}
	}
	ids := map[string]string{}
	imdb, tmdb, tvdb := parser.ParseProviderIDs(base)
	mergeIDs(ids, map[string]string{"imdb": imdb, "tmdb": tmdb, "tvdb": tvdb})
	anidb, anilist, mal := parser.ParseAnimeIDs(base)
	mergeIDs(ids, map[string]string{"anidb": anidb, "anilist": anilist, "mal": mal})
	cleaned := providerIDTagRE.ReplaceAllString(base, "")
	cleaned = strings.TrimSpace(cleaned)
	if match := tvShowFolderRE.FindStringSubmatch(cleaned); len(match) == 3 {
		return tvFolderIdentity{title: cleanTVTitle(match[1]), year: match[2], ids: ids}
	}
	if len(ids) > 0 {
		return tvFolderIdentity{title: cleanTVTitle(cleaned), ids: ids}
	}
	return tvFolderIdentity{ids: ids}
}

var providerIDTagRE = regexp.MustCompile(`(?i)[\[{(]\s*(?:imdb|tmdb|tvdb|anidb|anilist|mal|myanimelist)[-_:= ]+[a-z0-9]+(?:-[a-z0-9]+)?\s*[\]})]`)

func cleanTVTitle(title string) string {
	title = strings.TrimSpace(title)
	title = strings.ReplaceAll(title, ".", " ")
	title = strings.ReplaceAll(title, "_", " ")
	title = strings.Join(strings.Fields(title), " ")
	return title
}

func isSeasonDir(name string) bool {
	return seasonDirRE.MatchString(name)
}

func seasonFromPath(relPath string) (int, bool) {
	dir := filepath.Dir(relPath)
	for dir != "." && dir != "" {
		base := filepath.Base(dir)
		if m := seasonDirRE.FindStringSubmatch(base); len(m) == 2 {
			value := strings.ToLower(m[1])
			if strings.HasPrefix(value, "special") {
				return 0, true
			}
			season, err := strconv.Atoi(value)
			if err == nil {
				return season, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return 0, false
}

func seasonEpisodesFromName(name string) (int, []int, bool) {
	token := seasonEpisodeRE.FindString(name)
	if token == "" {
		return 0, nil, false
	}
	token = strings.ToUpper(token)
	eIdx := strings.Index(token, "E")
	if eIdx <= 1 {
		return 0, nil, false
	}
	season, err := strconv.Atoi(token[1:eIdx])
	if err != nil {
		return 0, nil, false
	}
	episodePart := token[eIdx+1:]
	rawEpisodes := strings.FieldsFunc(episodePart, func(r rune) bool {
		return r == 'E' || r == '-'
	})
	var episodes []int
	for _, raw := range rawEpisodes {
		episode, err := strconv.Atoi(raw)
		if err != nil {
			continue
		}
		episodes = append(episodes, episode)
	}
	if strings.Contains(episodePart, "-") && len(episodes) == 2 && episodes[1] > episodes[0] {
		var expanded []int
		for episode := episodes[0]; episode <= episodes[1]; episode++ {
			expanded = append(expanded, episode)
		}
		episodes = expanded
	}
	episodes = uniqueInts(episodes)
	if len(episodes) == 0 {
		return 0, nil, false
	}
	return season, episodes, true
}

func nearestTVNFO(dir string, nfos map[string]tvNFOEntry) (tvNFOEntry, bool) {
	for {
		if entry, ok := nfos[dir]; ok {
			return entry, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return tvNFOEntry{}, false
		}
		dir = parent
	}
}

func nearestTVPlexmatch(dir string, plexmatches map[string]tvPlexmatchEntry) (tvPlexmatchEntry, bool) {
	for {
		if entry, ok := plexmatches[dir]; ok {
			return entry, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return tvPlexmatchEntry{}, false
		}
		dir = parent
	}
}

func groupTVAssets(files []InventoryFile) map[string][]TVAssetPlan {
	out := make(map[string][]TVAssetPlan)
	for _, f := range files {
		if f.Class != ClassArtwork {
			continue
		}
		dir := filepath.Dir(f.RelPath)
		out[dir] = append(out[dir], TVAssetPlan{Type: f.AssetType, RelPath: f.RelPath})
	}
	for dir := range out {
		sort.Slice(out[dir], func(i, j int) bool {
			if out[dir][i].RelPath == out[dir][j].RelPath {
				return out[dir][i].Type < out[dir][j].Type
			}
			return out[dir][i].RelPath < out[dir][j].RelPath
		})
	}
	return out
}

func tvPlanAssets(assetsByDir map[string][]TVAssetPlan, dirs ...string) []TVAssetPlan {
	seen := map[string]bool{}
	var out []TVAssetPlan
	for _, dir := range dirs {
		for _, asset := range assetsByDir[dir] {
			key := asset.Type + "\x00" + asset.RelPath
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, asset)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RelPath == out[j].RelPath {
			return out[i].Type < out[j].Type
		}
		return out[i].RelPath < out[j].RelPath
	})
	return out
}

func findTVSubtitles(files []InventoryFile, media InventoryFile, showDir string) []string {
	mediaDir := filepath.Dir(media.RelPath)
	mediaBase := strings.TrimSuffix(filepath.Base(media.RelPath), filepath.Ext(media.RelPath))
	var out []string
	for _, f := range files {
		if f.Class != ClassSubtitle {
			continue
		}
		subDir := filepath.Dir(f.RelPath)
		if subDir != mediaDir && !isSubtitleSidecarDir(showDir, subDir) {
			continue
		}
		if subtitleMatchesMedia(mediaBase, strings.TrimSuffix(filepath.Base(f.RelPath), filepath.Ext(f.RelPath))) {
			out = append(out, f.RelPath)
		}
	}
	return sortedUnique(out)
}

func isSubtitleSidecarDir(showDir, subDir string) bool {
	if showDir == "." || showDir == "" {
		return false
	}
	if filepath.Dir(subDir) != showDir {
		return false
	}
	name := strings.ToLower(filepath.Base(subDir))
	return name == "subs" || name == "subtitles"
}

func subtitleMatchesMedia(mediaBase, subtitleBase string) bool {
	if subtitleBase == mediaBase || strings.HasPrefix(subtitleBase, mediaBase+".") {
		return true
	}
	normalizedMedia := normalizeSearchTitle(stripEpisodeTitle(mediaBase))
	normalizedSub := normalizeSearchTitle(stripEpisodeTitle(strings.TrimSuffix(subtitleBase, filepath.Ext(subtitleBase))))
	return normalizedMedia != "" && normalizedMedia == normalizedSub
}

func stripEpisodeTitle(value string) string {
	value = strings.TrimSpace(value)
	for _, sep := range []string{" - ", " -- "} {
		if idx := strings.Index(value, sep); idx >= 0 {
			return value[:idx]
		}
	}
	return value
}

func mergeIDs(dst map[string]string, src map[string]string) {
	for key, value := range src {
		value = strings.TrimSpace(value)
		if value == "" || dst[key] != "" {
			continue
		}
		dst[key] = value
	}
}

func uniqueInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	seen := map[int]bool{}
	var out []int
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Ints(out)
	return out
}

func uniqueTVEpisodeRefs(values []TVEpisodeRef) []TVEpisodeRef {
	if len(values) == 0 {
		return nil
	}
	type key struct {
		season   int
		episode  int
		absolute int
	}
	seen := map[key]int{}
	out := make([]TVEpisodeRef, 0, len(values))
	for _, value := range values {
		k := key{season: value.Season, episode: value.Episode}
		if value.Episode == 0 {
			k.absolute = value.Absolute
		}
		if idx, ok := seen[k]; ok {
			if out[idx].Absolute == 0 && value.Absolute > 0 {
				out[idx].Absolute = value.Absolute
			}
			continue
		}
		seen[k] = len(out)
		out = append(out, value)
	}
	return out
}

func uniqueTVAssets(values []TVAssetPlan) []TVAssetPlan {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]TVAssetPlan, 0, len(values))
	for _, value := range values {
		key := value.Type + "\x00" + value.RelPath
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func yearString(value string) string {
	for _, field := range strings.FieldsFunc(value, func(r rune) bool { return r < '0' || r > '9' }) {
		if len(field) == 4 {
			return field
		}
	}
	return ""
}
