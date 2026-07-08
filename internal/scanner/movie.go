package scanner

import (
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/nfo"
	"github.com/karbowiak/heya/internal/parser"
)

type MoviePlan struct {
	Title       string            `json:"title"`
	Year        string            `json:"year,omitempty"`
	Source      string            `json:"source"`
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
	Files       []string          `json:"files"`
	Parts       []MoviePartPlan   `json:"parts,omitempty"`
	Assets      []MovieAssetPlan  `json:"assets,omitempty"`
	NFO         string            `json:"nfo,omitempty"`
}

type MoviePartPlan struct {
	RelPath string `json:"rel_path"`
	Part    int    `json:"part"`
}

type MovieAssetPlan struct {
	Type    string `json:"type"`
	RelPath string `json:"rel_path"`
}

type movieNFOEntry struct {
	file InventoryFile
	nfo  *nfo.ParsedNFO
}

func AnalyzeMovies(ctx context.Context, inv Inventory, emit Emitter) ([]MoviePlan, error) {
	var plans []MoviePlan
	multipartPlans := map[string]int{}
	for _, root := range inv.Roots {
		if err := ctx.Err(); err != nil {
			return plans, err
		}

		nfos := parseMovieNFOs(root, emit)
		assetsByDir := groupMovieAssets(root.Files)
		for _, f := range root.Files {
			if f.Class != ClassPrimaryMedia {
				continue
			}
			parsed := parser.ParseStoragePath(f.RelPath)
			emitMovieParse(f, parsed, emit)

			if parsed.Release == nil {
				emit.Emit(Event{
					Event:    "movie.file.unplanned",
					Severity: SeverityWarn,
					Root:     root.Root,
					Path:     f.Path,
					RelPath:  f.RelPath,
					Reason:   "no_movie_identity",
					Message:  "file classified as media but no movie identity could be parsed",
				})
				continue
			}
			if !releaseFromLeaf(parsed) && dirHasLeafMovieRelease(root.Files, filepath.Dir(f.RelPath)) {
				emit.Emit(Event{
					Event:    "movie.file.unplanned",
					Severity: SeverityWarn,
					Root:     root.Root,
					Path:     f.Path,
					RelPath:  f.RelPath,
					Reason:   "secondary_media_in_movie_directory",
					Message:  "file only inherited movie identity from its folder and a stronger movie file exists beside it",
				})
				continue
			}

			nfoEntry, hasNFO := nearestMovieNFO(filepath.Dir(f.RelPath), nfos)
			identity, ok := movieIdentity(parsed, nfoEntry.nfo)
			if !ok {
				emit.Emit(Event{
					Event:    "movie.file.unplanned",
					Severity: SeverityWarn,
					Root:     root.Root,
					Path:     f.Path,
					RelPath:  f.RelPath,
					Reason:   "no_movie_identity",
					Message:  "file classified as media but no movie title/year or movie NFO could be resolved",
				})
				continue
			}

			plan := MoviePlan{
				Title:       identity.title,
				Year:        identity.year,
				Source:      identity.source,
				ExternalIDs: identity.ids,
				Files:       []string{f.RelPath},
				Assets:      assetsByDir[filepath.Dir(f.RelPath)],
			}
			multipartPart, hasMultipartPart := movieMultipartPart(f.RelPath)
			if hasMultipartPart {
				plan.Parts = []MoviePartPlan{{RelPath: f.RelPath, Part: multipartPart}}
			}
			if hasNFO {
				plan.NFO = nfoEntry.file.RelPath
				emit.Emit(Event{
					Event:   "movie.nfo.applied",
					Root:    root.Root,
					Path:    nfoEntry.file.Path,
					RelPath: nfoEntry.file.RelPath,
					Kind:    "movie",
					Data: map[string]any{
						"file":  f.RelPath,
						"title": nfoEntry.nfo.Title,
						"year":  nfoEntry.nfo.Year,
						"ids":   identity.ids,
					},
				})
			}
			if hasMultipartPart {
				key := movieMultipartKey(filepath.Dir(f.RelPath), identity)
				if idx, ok := multipartPlans[key]; ok {
					plans[idx].Files = append(plans[idx].Files, f.RelPath)
					plans[idx].Parts = append(plans[idx].Parts, MoviePartPlan{RelPath: f.RelPath, Part: multipartPart})
					sortMoviePlanFiles(&plans[idx])
					emit.Emit(Event{
						Event:   "plan.movie.multipart_joined",
						Root:    root.Root,
						Path:    f.Path,
						RelPath: f.RelPath,
						Kind:    "would_attach_movie_part",
						Data: map[string]any{
							"title": plan.Title,
							"year":  plan.Year,
							"part":  multipartPart,
							"files": len(plans[idx].Files),
						},
					})
					continue
				}
				multipartPlans[key] = len(plans)
			}
			plans = append(plans, plan)
			emit.Emit(Event{
				Event:   "plan.movie",
				Root:    root.Root,
				Path:    f.Path,
				RelPath: f.RelPath,
				Kind:    "would_materialize_movie",
				Data: map[string]any{
					"title":        plan.Title,
					"year":         plan.Year,
					"source":       plan.Source,
					"external_ids": plan.ExternalIDs,
					"assets":       len(plan.Assets),
					"files":        len(plan.Files),
				},
			})
		}
	}
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].Title == plans[j].Title {
			return plans[i].Year < plans[j].Year
		}
		return plans[i].Title < plans[j].Title
	})
	emit.Emit(Event{Event: "domain.summary", Data: map[string]any{"domain": "movie", "plans": len(plans)}})
	return plans, nil
}

func sortMoviePlanFiles(plan *MoviePlan) {
	sort.Strings(plan.Files)
	sort.Slice(plan.Parts, func(i, j int) bool {
		if plan.Parts[i].Part == plan.Parts[j].Part {
			return plan.Parts[i].RelPath < plan.Parts[j].RelPath
		}
		return plan.Parts[i].Part < plan.Parts[j].Part
	})
}

func movieMultipartKey(dir string, identity movieIdent) string {
	keyParts := []string{dir, strings.ToLower(identity.title), identity.year}
	for _, provider := range []string{"tmdb", "imdb", "tvdb"} {
		if value := identity.ids[provider]; value != "" {
			keyParts = append(keyParts, provider+":"+strings.ToLower(value))
			break
		}
	}
	return strings.Join(keyParts, "\x00")
}

var movieMultipartRE = regexp.MustCompile(`(?i)(?:^|[\s._([{-])(?:cd|disc|disk|part)[\s._-]*(\d{1,2})(?:$|[\s._)\]}-])`)

func movieMultipartPart(relPath string) (int, bool) {
	base := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))
	m := movieMultipartRE.FindStringSubmatch(base)
	if len(m) != 2 {
		return 0, false
	}
	part, err := strconv.Atoi(m[1])
	if err != nil || part <= 0 {
		return 0, false
	}
	return part, true
}

func dirHasLeafMovieRelease(files []InventoryFile, dir string) bool {
	for _, f := range files {
		if f.Class != ClassPrimaryMedia || filepath.Dir(f.RelPath) != dir {
			continue
		}
		parsed := parser.ParseStoragePath(f.RelPath)
		if parsed.Release != nil && releaseFromLeaf(parsed) {
			return true
		}
	}
	return false
}

func releaseFromLeaf(parsed parser.ParsedStorageEntry) bool {
	if parsed.Release == nil {
		return false
	}
	return strings.EqualFold(parsed.ReleaseSegment, parsed.Basename)
}

func parseMovieNFOs(root InventoryRoot, emit Emitter) map[string]movieNFOEntry {
	out := make(map[string]movieNFOEntry)
	for _, f := range root.Files {
		if f.Class != ClassNFO || f.Kind != "movie" {
			continue
		}
		parsed := nfo.ParseFile(root.FS, f.RelPath, "movie")
		if parsed == nil {
			emit.Emit(Event{Event: "nfo.parse_failed", Severity: SeverityWarn, Root: root.Root, Path: f.Path, RelPath: f.RelPath, Kind: "movie"})
			continue
		}
		dir := filepath.Dir(f.RelPath)
		out[dir] = movieNFOEntry{file: f, nfo: parsed}
		emit.Emit(Event{
			Event:   "nfo.parsed",
			Root:    root.Root,
			Path:    f.Path,
			RelPath: f.RelPath,
			Kind:    "movie",
			Data: map[string]any{
				"title": parsed.Title,
				"year":  parsed.Year,
				"ids":   idsFromNFO(parsed),
			},
		})
	}
	return out
}

func groupMovieAssets(files []InventoryFile) map[string][]MovieAssetPlan {
	out := make(map[string][]MovieAssetPlan)
	for _, f := range files {
		if f.Class != ClassArtwork {
			continue
		}
		dir := filepath.Dir(f.RelPath)
		out[dir] = append(out[dir], MovieAssetPlan{Type: f.AssetType, RelPath: f.RelPath})
	}
	for dir := range out {
		sort.Slice(out[dir], func(i, j int) bool {
			if out[dir][i].Type == out[dir][j].Type {
				return out[dir][i].RelPath < out[dir][j].RelPath
			}
			return out[dir][i].Type < out[dir][j].Type
		})
	}
	return out
}

func nearestMovieNFO(dir string, nfos map[string]movieNFOEntry) (movieNFOEntry, bool) {
	for {
		if entry, ok := nfos[dir]; ok {
			return entry, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return movieNFOEntry{}, false
		}
		dir = parent
	}
}

func emitMovieParse(file InventoryFile, parsed parser.ParsedStorageEntry, emit Emitter) {
	data := map[string]any{
		"media": string(parsed.Media),
	}
	if parsed.Release != nil {
		data["title"] = parsed.Release.Title
		data["year"] = parsed.Release.Year
		data["strategy"] = string(parsed.Release.Strategy)
		data["score"] = parsed.Release.Score
		data["ids"] = idsFromRelease(parsed.Release)
	}
	emit.Emit(Event{Event: "parse.result", Root: file.Root, Path: file.Path, RelPath: file.RelPath, Kind: "movie", Data: data})
}

type movieIdent struct {
	title  string
	year   string
	source string
	ids    map[string]string
}

func movieIdentity(parsed parser.ParsedStorageEntry, localNFO *nfo.ParsedNFO) (movieIdent, bool) {
	ident := movieIdent{ids: map[string]string{}}
	if parsed.Release != nil {
		ident.title = strings.TrimSpace(parsed.Release.Title)
		ident.year = strings.TrimSpace(parsed.Release.Year)
		ident.source = "filename"
		for k, v := range idsFromRelease(parsed.Release) {
			ident.ids[k] = v
		}
	}
	if localNFO != nil {
		if strings.TrimSpace(localNFO.Title) != "" {
			ident.title = strings.TrimSpace(localNFO.Title)
			ident.source = "nfo"
		}
		if strings.TrimSpace(localNFO.Year) != "" {
			ident.year = strings.TrimSpace(localNFO.Year)
		}
		for k, v := range idsFromNFO(localNFO) {
			ident.ids[k] = v
		}
	}
	if ident.title == "" {
		return movieIdent{}, false
	}
	return ident, true
}

func idsFromRelease(r *parser.SceneReleaseParse) map[string]string {
	ids := map[string]string{}
	if r == nil {
		return ids
	}
	if r.ImdbID != "" {
		ids["imdb"] = r.ImdbID
	}
	if r.TmdbID != "" {
		ids["tmdb"] = r.TmdbID
	}
	if r.TvdbID != "" {
		ids["tvdb"] = r.TvdbID
	}
	if r.AnidbID != "" {
		ids["anidb"] = r.AnidbID
	}
	if r.AnilistID != "" {
		ids["anilist"] = r.AnilistID
	}
	if r.MalID != "" {
		ids["mal"] = r.MalID
	}
	return ids
}

func idsFromNFO(n *nfo.ParsedNFO) map[string]string {
	ids := map[string]string{}
	if n == nil {
		return ids
	}
	if n.IMDBID != "" {
		ids["imdb"] = n.IMDBID
	}
	if n.TMDBID != "" {
		ids["tmdb"] = n.TMDBID
	}
	if n.TVDBID != "" {
		ids["tvdb"] = n.TVDBID
	}
	if n.AniDBID != "" {
		ids["anidb"] = n.AniDBID
	}
	if n.AniListID != "" {
		ids["anilist"] = n.AniListID
	}
	if n.MALID != "" {
		ids["mal"] = n.MALID
	}
	if n.MBID != "" {
		ids["mbid"] = n.MBID
	}
	return ids
}
