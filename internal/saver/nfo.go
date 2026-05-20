package saver

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/rs/zerolog/log"
)

type MovieNFO struct {
	XMLName          xml.Name   `xml:"movie"`
	Title            string     `xml:"title"`
	OriginalTitle    string     `xml:"originaltitle,omitempty"`
	SortTitle        string     `xml:"sorttitle,omitempty"`
	Year             string     `xml:"year,omitempty"`
	Plot             string     `xml:"plot,omitempty"`
	Tagline          string     `xml:"tagline,omitempty"`
	Runtime          int32      `xml:"runtime,omitempty"`
	Rating           string     `xml:"rating,omitempty"`
	UniqueIDs        []UniqueID `xml:"uniqueid"`
	Genres           []string   `xml:"genre"`
}

type TVShowNFO struct {
	XMLName       xml.Name   `xml:"tvshow"`
	Title         string     `xml:"title"`
	OriginalTitle string     `xml:"originaltitle,omitempty"`
	SortTitle     string     `xml:"sorttitle,omitempty"`
	Year          string     `xml:"year,omitempty"`
	Plot          string     `xml:"plot,omitempty"`
	Status        string     `xml:"status,omitempty"`
	UniqueIDs     []UniqueID `xml:"uniqueid"`
	Genres        []string   `xml:"genre"`
}

type UniqueID struct {
	XMLName xml.Name `xml:"uniqueid"`
	Type    string   `xml:"type,attr"`
	Default bool     `xml:"default,attr,omitempty"`
	Value   string   `xml:",chardata"`
}

func WriteMovieNFO(mediaDir string, item sqlc.MediaItem, movie sqlc.Movie) error {
	nfoPath := filepath.Join(mediaDir, "movie.nfo")
	if _, err := os.Stat(nfoPath); err == nil {
		log.Debug().Str("path", nfoPath).Msg("NFO already exists, skipping")
		return nil
	}

	rating := ""
	if f, err := movie.Rating.Float64Value(); err == nil && f.Valid {
		rating = fmt.Sprintf("%.1f", f.Float64)
	}

	nfo := MovieNFO{
		Title:         item.Title,
		OriginalTitle: movie.OriginalTitle,
		SortTitle:     item.SortTitle,
		Year:          item.Year,
		Plot:          item.Description,
		Tagline:       movie.Tagline,
		Runtime:       movie.RuntimeMinutes,
		Rating:        rating,
		Genres:        movie.Genres,
		UniqueIDs:     buildUniqueIDs(item),
	}

	return writeXML(nfoPath, nfo)
}

func WriteTVShowNFO(mediaDir string, item sqlc.MediaItem, series sqlc.TvSeries) error {
	nfoPath := filepath.Join(mediaDir, "tvshow.nfo")
	if _, err := os.Stat(nfoPath); err == nil {
		log.Debug().Str("path", nfoPath).Msg("NFO already exists, skipping")
		return nil
	}

	nfo := TVShowNFO{
		Title:         item.Title,
		OriginalTitle: series.OriginalName,
		SortTitle:     item.SortTitle,
		Year:          item.Year,
		Plot:          item.Description,
		Status:        series.Status,
		Genres:        series.Genres,
		UniqueIDs:     buildUniqueIDs(item),
	}

	return writeXML(nfoPath, nfo)
}

func buildUniqueIDs(item sqlc.MediaItem) []UniqueID {
	ids := parseExternalIDs(item.ExternalIds)
	var result []UniqueID

	order := []struct{ key, label string }{
		{"tmdb", "tmdb"},
		{"imdb", "imdb"},
		{"tvdb", "tvdb"},
		{"anidb", "anidb"},
	}

	first := true
	for _, o := range order {
		if v, ok := ids[o.key]; ok && v != "" {
			result = append(result, UniqueID{
				Type:    o.label,
				Default: first,
				Value:   v,
			})
			first = false
		}
	}
	return result
}

func MediaDir(filePath string) string {
	dir := filepath.Dir(filePath)
	base := strings.ToLower(filepath.Base(dir))
	if strings.HasPrefix(base, "season") || strings.HasPrefix(base, "disc") {
		dir = filepath.Dir(dir)
	}
	return dir
}

func writeXML(path string, v any) error {
	data, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	content := xml.Header + string(data) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	log.Info().Str("path", path).Msg("NFO written")
	return nil
}

func parseExternalIDs(data []byte) map[string]string {
	if len(data) == 0 {
		return nil
	}
	m := map[string]string{}
	for _, part := range strings.Split(strings.Trim(string(data), "{}\""), ",") {
		kv := strings.SplitN(part, ":", 2)
		if len(kv) == 2 {
			k := strings.Trim(kv[0], "\" ")
			v := strings.Trim(kv[1], "\" ")
			if k != "" && v != "" {
				m[k] = v
			}
		}
	}
	return m
}
