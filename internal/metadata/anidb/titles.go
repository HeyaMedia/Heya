package anidb

import (
	"compress/gzip"
	"encoding/xml"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rs/zerolog/log"
	"golang.org/x/text/unicode/norm"
)

const (
	titlesURL     = "https://anidb.net/api/anime-titles.xml.gz"
	titlesFile    = "anidb-titles.xml.gz"
	refreshAge    = 7 * 24 * time.Hour
)

type titleDump struct {
	Animes []titleAnime `xml:"anime"`
}

type titleAnime struct {
	AID    int          `xml:"aid,attr"`
	Titles []titleEntry `xml:"title"`
}

type titleEntry struct {
	Lang  string `xml:"lang,attr"`
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

type titleIndex struct {
	entries []indexEntry
}

type indexEntry struct {
	AID        int
	Title      string
	Normalized string
	Lang       string
	Type       string
}

type TitleCache struct {
	mu      sync.RWMutex
	dataDir string
	index   *titleIndex
	loadedAt time.Time
}

func NewTitleCache(dataDir string) *TitleCache {
	return &TitleCache{dataDir: dataDir}
}

func (tc *TitleCache) Search(query string, limit int) []TitleMatch {
	tc.mu.RLock()
	idx := tc.index
	tc.mu.RUnlock()

	if idx == nil {
		return nil
	}

	nq := normalize(query)
	if nq == "" {
		return nil
	}

	type scored struct {
		aid   int
		title string
		score float64
	}

	seen := map[int]*scored{}

	for _, e := range idx.entries {
		s := similarity(nq, e.Normalized)
		if s < 0.5 {
			continue
		}

		if e.Type == "main" || e.Type == "official" {
			s += 0.05
		}

		if existing, ok := seen[e.AID]; ok {
			if s > existing.score {
				existing.score = s
				existing.title = e.Title
			}
		} else {
			seen[e.AID] = &scored{aid: e.AID, title: e.Title, score: s}
		}
	}

	var results []TitleMatch
	for _, s := range seen {
		results = append(results, TitleMatch{AID: s.aid, Title: s.title, Score: s.score})
	}

	sortMatches(results)

	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

type TitleMatch struct {
	AID   int
	Title string
	Score float64
}

func (tc *TitleCache) EnsureLoaded() error {
	tc.mu.RLock()
	if tc.index != nil && time.Since(tc.loadedAt) < refreshAge {
		tc.mu.RUnlock()
		return nil
	}
	tc.mu.RUnlock()

	return tc.load()
}

func (tc *TitleCache) load() error {
	path := filepath.Join(tc.dataDir, titlesFile)

	if needsDownload(path) {
		if err := downloadTitles(path); err != nil {
			if _, statErr := os.Stat(path); statErr != nil {
				return err
			}
			log.Warn().Err(err).Msg("anidb title refresh failed, using cached copy")
		}
	}

	idx, err := parseTitlesFile(path)
	if err != nil {
		return err
	}

	tc.mu.Lock()
	tc.index = idx
	tc.loadedAt = time.Now()
	tc.mu.Unlock()

	log.Info().Int("entries", len(idx.entries)).Msg("anidb title index loaded")
	return nil
}

func needsDownload(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) > refreshAge
}

func downloadTitles(path string) error {
	log.Info().Msg("downloading anidb title dump")

	req, _ := http.NewRequest(http.MethodGet, titlesURL, nil)
	req.Header.Set("User-Agent", "Heya/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &downloadError{code: resp.StatusCode}
	}

	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

type downloadError struct {
	code int
}

func (e *downloadError) Error() string {
	return "anidb title download: HTTP " + http.StatusText(e.code)
}

func parseTitlesFile(path string) (*titleIndex, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	var dump titleDump
	if err := xml.NewDecoder(gz).Decode(&dump); err != nil {
		return nil, err
	}

	var entries []indexEntry
	for _, a := range dump.Animes {
		for _, t := range a.Titles {
			entries = append(entries, indexEntry{
				AID:        a.AID,
				Title:      t.Value,
				Normalized: normalize(t.Value),
				Lang:       t.Lang,
				Type:       t.Type,
			})
		}
	}

	return &titleIndex{entries: entries}, nil
}

func normalize(s string) string {
	s = norm.NFKD.String(s)
	s = strings.ToLower(s)

	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			b.WriteRune(r)
		}
	}

	result := b.String()
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}
	return strings.TrimSpace(result)
}

func similarity(a, b string) float64 {
	if a == b {
		return 1.0
	}

	if strings.Contains(b, a) || strings.Contains(a, b) {
		shorter := len(a)
		longer := len(b)
		if shorter > longer {
			shorter, longer = longer, shorter
		}
		return float64(shorter) / float64(longer)
	}

	la := len(a)
	lb := len(b)
	if la == 0 || lb == 0 {
		return 0
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}

	maxLen := la
	if lb > maxLen {
		maxLen = lb
	}
	return 1.0 - float64(prev[lb])/float64(maxLen)
}

func sortMatches(m []TitleMatch) {
	for i := 1; i < len(m); i++ {
		for j := i; j > 0 && m[j].Score > m[j-1].Score; j-- {
			m[j], m[j-1] = m[j-1], m[j]
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
