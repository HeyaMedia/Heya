package scanner

import (
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/karbowiak/heya/internal/mediafile"
)

type BookPlan struct {
	Key         string            `json:"key"`
	Title       string            `json:"title"`
	Author      string            `json:"author,omitempty"`
	Year        string            `json:"year,omitempty"`
	Format      string            `json:"format"`
	FileFormat  string            `json:"file_format,omitempty"`
	FileFormats []string          `json:"file_formats,omitempty"`
	Source      string            `json:"source"`
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
	Files       []string          `json:"files"`
	Assets      []BookAssetPlan   `json:"assets,omitempty"`
	Confidence  float64           `json:"confidence"`
	Issues      []string          `json:"issues,omitempty"`
}

type BookAssetPlan struct {
	Type    string `json:"type"`
	RelPath string `json:"rel_path"`
}

type bookIdentity struct {
	title      string
	author     string
	year       string
	format     string
	fileFormat string
	source     string
	confidence float64
	issues     []string
}

var (
	bookAuthorTitleRE   = regexp.MustCompile(`^(.+?)\s+[-–—]\s+(.+)$`)
	bookTitleAuthorRE   = regexp.MustCompile(`^(.+?)\s+[-–—]\s+(.+)$`)
	bookYearTailRE      = regexp.MustCompile(`(?i)\s*[\[(]((?:18|19|20)\d{2})[\])]\s*(?:[-_. ]*(?:epub|pdf|mobi|azw3?|cbr|cbz|djvu|audiobook|unabridged|retail|ebook).*)?$`)
	bookBracketNoiseRE  = regexp.MustCompile(`(?i)\s*[\[(](?:epub|pdf|mobi|azw3?|cbr|cbz|djvu|retail|ebook|audiobook|unabridged|audible|m4b|mp3|m4a|flac|aac)[\])]\s*`)
	bookReleaseNoiseRE  = regexp.MustCompile(`(?i)\b(?:epub|pdf|mobi|azw3?|cbr|cbz|djvu|retail|ebook|audiobook|unabridged|audible|m4b|mp3|m4a|flac|aac)\b`)
	bookChapterNameRE   = regexp.MustCompile(`(?i)^(?:chapter|chap|part|cd|disc|track)\s*[\d._ -]+$`)
	bookDuplicateDashRE = regexp.MustCompile(`\s+[-–—]\s+$`)
)

func AnalyzeBooks(ctx context.Context, inv Inventory, emit Emitter) ([]BookPlan, error) {
	plansByKey := map[string]*BookPlan{}
	for _, root := range inv.Roots {
		assetsByDir := groupBookAssets(root.Files)
		for _, file := range root.Files {
			if err := ctx.Err(); err != nil {
				return bookPlansFromMap(plansByKey), err
			}
			if file.Class != ClassPrimaryMedia || !isBookScannerMediaFile(file) {
				continue
			}
			identity, ok := parseBookIdentity(file)
			if !ok {
				emit.Emit(Event{
					Event:    "book.file.unplanned",
					Severity: SeverityWarn,
					Root:     root.Root,
					Path:     file.Path,
					RelPath:  file.RelPath,
					Reason:   "no_book_identity",
					Message:  "file classified as book/audiobook media but no title could be parsed",
				})
				continue
			}
			key := bookIdentityKey(identity.author, identity.title, identity.year, identity.format)
			plan, exists := plansByKey[key]
			if !exists {
				plan = &BookPlan{
					Key:         key,
					Title:       identity.title,
					Author:      identity.author,
					Year:        identity.year,
					Format:      identity.format,
					FileFormat:  identity.fileFormat,
					Source:      identity.source,
					Confidence:  identity.confidence,
					Issues:      append([]string{}, identity.issues...),
					FileFormats: []string{},
				}
				plansByKey[key] = plan
			}
			plan.Files = append(plan.Files, file.RelPath)
			plan.FileFormats = appendUniqueString(plan.FileFormats, identity.fileFormat)
			plan.Assets = appendBookAssets(plan.Assets, assetsByDir[filepath.Dir(file.RelPath)]...)
			plan.Issues = appendUniqueStrings(plan.Issues, identity.issues...)
			if identity.confidence < plan.Confidence {
				plan.Confidence = identity.confidence
			}
			if plan.FileFormat == "" {
				plan.FileFormat = identity.fileFormat
			}
			emit.Emit(Event{
				Event:   "book.plan",
				Root:    root.Root,
				Path:    file.Path,
				RelPath: file.RelPath,
				Kind:    "would_materialize_book",
				Data: map[string]any{
					"key":        key,
					"title":      plan.Title,
					"author":     plan.Author,
					"year":       plan.Year,
					"format":     plan.Format,
					"confidence": plan.Confidence,
					"files":      len(plan.Files),
				},
			})
		}
	}

	plans := bookPlansFromMap(plansByKey)
	for i := range plans {
		sort.Strings(plans[i].Files)
		sort.Slice(plans[i].Assets, func(a, b int) bool {
			if plans[i].Assets[a].Type == plans[i].Assets[b].Type {
				return plans[i].Assets[a].RelPath < plans[i].Assets[b].RelPath
			}
			return plans[i].Assets[a].Type < plans[i].Assets[b].Type
		})
	}
	emit.Emit(Event{Event: "domain.summary", Data: map[string]any{"domain": "book", "plans": len(plans)}})
	return plans, nil
}

func bookPlansFromMap(plansByKey map[string]*BookPlan) []BookPlan {
	out := make([]BookPlan, 0, len(plansByKey))
	for _, plan := range plansByKey {
		out = append(out, *plan)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Author == out[j].Author {
			if out[i].Year == out[j].Year {
				return out[i].Title < out[j].Title
			}
			return out[i].Year < out[j].Year
		}
		return out[i].Author < out[j].Author
	})
	return out
}

func parseBookIdentity(file InventoryFile) (bookIdentity, bool) {
	format := "book"
	if mediafile.IsAudioExt(file.Ext) {
		format = "audiobook"
	}
	fileFormat := strings.TrimPrefix(strings.ToLower(file.Ext), ".")
	segments := splitRelPath(file.RelPath)
	if len(segments) == 0 {
		return bookIdentity{}, false
	}

	var title, author, year, source string
	var confidence float64
	if format == "audiobook" {
		title, author, year, source, confidence = parseAudiobookPathIdentity(segments)
	} else {
		title, author, year, source, confidence = parseEbookPathIdentity(segments)
	}
	title = cleanBookValue(title)
	author = cleanBookValue(author)
	if title == "" {
		return bookIdentity{}, false
	}
	issues := bookIdentityIssues(title, author, year)
	if len(issues) > 0 && confidence > 0.72 {
		confidence = 0.72
	}
	return bookIdentity{
		title:      title,
		author:     author,
		year:       year,
		format:     format,
		fileFormat: fileFormat,
		source:     source,
		confidence: confidence,
		issues:     issues,
	}, true
}

func parseEbookPathIdentity(segments []string) (title, author, year, source string, confidence float64) {
	leaf := strings.TrimSuffix(segments[len(segments)-1], filepath.Ext(segments[len(segments)-1]))
	parent := ""
	grandparent := ""
	if len(segments) >= 2 {
		parent = segments[len(segments)-2]
	}
	if len(segments) >= 3 {
		grandparent = segments[len(segments)-3]
	}

	if parent != "" && !looksLikeBookContainer(parent) {
		pt, py := splitBookTitleYear(parent)
		if pt != "" && !bookChapterNameRE.MatchString(cleanBookValue(leaf)) {
			title = pt
			year = py
			author = grandparent
			source = "folder"
			confidence = bookConfidence(title, author, year)
		}
	}
	if title == "" {
		a, t, y, ok := splitBookAuthorTitleYear(leaf)
		if ok {
			author, title, year = a, t, y
			source = "filename_author_title"
			confidence = bookConfidence(title, author, year)
		}
	}
	if title == "" && parent != "" && grandparent != "" {
		t, y := splitBookTitleYear(parent)
		if t != "" {
			title, year, author = t, y, grandparent
			source = "author_folder"
			confidence = bookConfidence(title, author, year)
		}
	}
	if title == "" {
		title, year = splitBookTitleYear(leaf)
		source = "filename"
		confidence = bookConfidence(title, author, year)
	}
	if author == "" && parent != "" && looksLikeAuthorFolder(parent) {
		author = parent
	}
	if author == "" {
		if _, a, ok := splitBookTitleAuthor(leaf); ok {
			author = a
		}
	}
	return title, author, year, source, confidence
}

func parseAudiobookPathIdentity(segments []string) (title, author, year, source string, confidence float64) {
	parent := ""
	grandparent := ""
	if len(segments) >= 2 {
		parent = segments[len(segments)-2]
	}
	if len(segments) >= 3 {
		grandparent = segments[len(segments)-3]
	}
	if parent != "" {
		a, t, y, ok := splitBookAuthorTitleYear(parent)
		if ok {
			return t, a, y, "audiobook_folder_author_title", bookConfidence(t, a, y)
		}
	}
	if parent != "" && grandparent != "" {
		t, y := splitBookTitleYear(parent)
		if t != "" {
			return t, grandparent, y, "audiobook_author_folder", bookConfidence(t, grandparent, y)
		}
	}
	leaf := strings.TrimSuffix(segments[len(segments)-1], filepath.Ext(segments[len(segments)-1]))
	a, t, y, ok := splitBookAuthorTitleYear(leaf)
	if ok {
		return t, a, y, "audiobook_filename_author_title", bookConfidence(t, a, y)
	}
	t, y = splitBookTitleYear(leaf)
	return t, "", y, "audiobook_filename", bookConfidence(t, "", y)
}

func splitBookAuthorTitleYear(value string) (author, title, year string, ok bool) {
	value, year = splitBookTitleYear(value)
	m := bookAuthorTitleRE.FindStringSubmatch(value)
	if len(m) != 3 {
		return "", "", year, false
	}
	author = cleanBookValue(m[1])
	title = cleanBookValue(m[2])
	if author == "" || title == "" {
		return "", "", year, false
	}
	return author, title, year, true
}

func splitBookTitleAuthor(value string) (title, author string, ok bool) {
	value, _ = splitBookTitleYear(value)
	m := bookTitleAuthorRE.FindStringSubmatch(value)
	if len(m) != 3 {
		return "", "", false
	}
	left := cleanBookValue(m[1])
	right := cleanBookValue(m[2])
	if left == "" || right == "" {
		return "", "", false
	}
	return left, right, true
}

func splitBookTitleYear(value string) (title, year string) {
	value = strings.TrimSuffix(value, filepath.Ext(value))
	value = strings.ReplaceAll(value, ".", " ")
	value = strings.ReplaceAll(value, "_", " ")
	if m := bookYearTailRE.FindStringSubmatch(value); len(m) == 2 {
		year = m[1]
		value = bookYearTailRE.ReplaceAllString(value, "")
	}
	value = bookBracketNoiseRE.ReplaceAllString(value, " ")
	value = bookReleaseNoiseRE.ReplaceAllString(value, " ")
	title = cleanBookValue(value)
	return title, year
}

func bookConfidence(title, author, year string) float64 {
	switch {
	case title != "" && author != "" && year != "":
		return 0.96
	case title != "" && author != "":
		return 0.84
	case title != "" && year != "":
		return 0.72
	case title != "":
		return 0.55
	default:
		return 0
	}
}

func bookIdentityIssues(title, author, year string) []string {
	var issues []string
	if title == "" {
		issues = append(issues, "missing_title")
	}
	if author == "" {
		issues = append(issues, "missing_author")
	}
	if year == "" {
		issues = append(issues, "missing_year")
	}
	return issues
}

func bookIdentityKey(author, title, year, format string) string {
	parts := []string{
		firstNonEmpty(format, "book"),
		normalizeSearchTitle(author),
		normalizeSearchTitle(title),
		year,
	}
	return "book:" + strings.Join(parts, "|")
}

func cleanBookValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "_", " ")
	value = bookDuplicateDashRE.ReplaceAllString(value, "")
	value = strings.Join(strings.Fields(value), " ")
	return strings.Trim(value, " -–—")
}

func looksLikeBookContainer(value string) bool {
	norm := normalizeSearchTitle(value)
	return norm == "" || norm == "books" || norm == "audiobooks" || norm == "ebooks"
}

func looksLikeAuthorFolder(value string) bool {
	value = cleanBookValue(value)
	return value != "" && !bookYearTailRE.MatchString(value) && len(strings.Fields(value)) >= 2
}

func isBookScannerMediaFile(file InventoryFile) bool {
	return isEbookExt(file.Ext) || mediafile.IsAudioExt(file.Ext)
}

func isEbookExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".epub", ".pdf", ".mobi", ".azw", ".azw3", ".cbr", ".cbz", ".djvu":
		return true
	default:
		return false
	}
}

func groupBookAssets(files []InventoryFile) map[string][]BookAssetPlan {
	out := map[string][]BookAssetPlan{}
	for _, file := range files {
		if file.Class != ClassArtwork {
			continue
		}
		assetType := file.AssetType
		if assetType == "" {
			assetType = "poster"
		}
		dir := filepath.Dir(file.RelPath)
		out[dir] = append(out[dir], BookAssetPlan{Type: assetType, RelPath: file.RelPath})
	}
	return out
}

func appendBookAssets(existing []BookAssetPlan, add ...BookAssetPlan) []BookAssetPlan {
	seen := map[string]bool{}
	for _, asset := range existing {
		seen[asset.Type+"\x00"+asset.RelPath] = true
	}
	for _, asset := range add {
		key := asset.Type + "\x00" + asset.RelPath
		if !seen[key] {
			existing = append(existing, asset)
			seen[key] = true
		}
	}
	return existing
}

func appendUniqueString(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueStrings(values []string, add ...string) []string {
	for _, value := range add {
		values = appendUniqueString(values, value)
	}
	return values
}
