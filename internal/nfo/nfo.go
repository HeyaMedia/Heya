package nfo

import (
	"encoding/xml"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

type TVShowNFO struct {
	XMLName       xml.Name   `xml:"tvshow"`
	Title         string     `xml:"title"`
	OriginalTitle string     `xml:"originaltitle"`
	Plot          string     `xml:"plot"`
	Year          string     `xml:"year"`
	Rating        string     `xml:"rating"`
	MPAA          string     `xml:"mpaa"`
	Premiered     string     `xml:"premiered"`
	Status        string     `xml:"status"`
	Studio        []string   `xml:"studio"`
	Genre         []string   `xml:"genre"`
	Tag           []string   `xml:"tag"`
	IMDBID        string     `xml:"imdb_id"`
	TMDBID        string     `xml:"tmdbid"`
	TVDBID        string     `xml:"tvdbid"`
	Runtime       string     `xml:"runtime"`
	Trailer       []string   `xml:"trailer"`
	UniqueIDs     []UniqueID `xml:"uniqueid"`
	Actors        []ActorNFO `xml:"actor"`
}

type MovieNFO struct {
	XMLName       xml.Name   `xml:"movie"`
	Title         string     `xml:"title"`
	OriginalTitle string     `xml:"originaltitle"`
	Plot          string     `xml:"plot"`
	Tagline       string     `xml:"tagline"`
	Year          string     `xml:"year"`
	Rating        string     `xml:"rating"`
	MPAA          string     `xml:"mpaa"`
	Runtime       string     `xml:"runtime"`
	Genre         []string   `xml:"genre"`
	Studio        []string   `xml:"studio"`
	Tag           []string   `xml:"tag"`
	IMDBID        string     `xml:"imdb_id"`
	TMDBID        string     `xml:"tmdbid"`
	UniqueIDs     []UniqueID `xml:"uniqueid"`
	Actors        []ActorNFO `xml:"actor"`
}

type ArtistNFO struct {
	XMLName   xml.Name `xml:"artist"`
	Name      string   `xml:"name"`
	Biography string   `xml:"biography"`
	Born      string   `xml:"born"`
	Died      string   `xml:"died"`
	MBID      string   `xml:"musicBrainzArtistID"`
}

type UniqueID struct {
	Type    string `xml:"type,attr"`
	Default bool   `xml:"default,attr"`
	Value   string `xml:",chardata"`
}

type ActorNFO struct {
	Name      string `xml:"name"`
	Role      string `xml:"role"`
	Type      string `xml:"type"`
	SortOrder int    `xml:"sortorder"`
	Thumb     string `xml:"thumb"`
}

type ParsedNFO struct {
	Title         string
	OriginalTitle string
	Year          string
	Plot          string
	TMDBID        string
	IMDBID        string
	TVDBID        string
	MBID          string
	Rating        string
	Genres        []string
	Tags          []string
	Studios       []string
	Actors        []ActorNFO
	Kind          string // "tvshow", "movie", "artist"
}

func FindAndParse(fsys fs.FS, dir string) *ParsedNFO {
	nfoFiles := []struct {
		name string
		kind string
	}{
		{"tvshow.nfo", "tvshow"},
		{"movie.nfo", "movie"},
		{"artist.nfo", "artist"},
	}

	for _, nf := range nfoFiles {
		path := filepath.Join(dir, nf.name)
		f, err := fsys.Open(path)
		if err != nil {
			continue
		}
		defer f.Close()

		parsed, err := parseNFO(f, nf.kind)
		if err != nil {
			log.Debug().Err(err).Str("path", path).Msg("error parsing NFO")
			continue
		}

		log.Info().Str("path", path).Str("kind", nf.kind).Str("title", parsed.Title).Str("tmdb", parsed.TMDBID).Str("imdb", parsed.IMDBID).Msg("found NFO")
		return parsed
	}

	return nil
}

func FindAndParseInDir(dir string) *ParsedNFO {
	nfoFiles := []struct {
		name string
		kind string
	}{
		{"tvshow.nfo", "tvshow"},
		{"movie.nfo", "movie"},
		{"artist.nfo", "artist"},
	}

	for _, nf := range nfoFiles {
		path := filepath.Join(dir, nf.name)
		data, err := readFileBytes(path)
		if err != nil {
			continue
		}

		parsed, err := parseNFOBytes(data, nf.kind)
		if err != nil {
			log.Debug().Err(err).Str("path", path).Msg("error parsing NFO")
			continue
		}

		log.Info().Str("path", path).Str("kind", nf.kind).Str("title", parsed.Title).Str("tmdb", parsed.TMDBID).Str("imdb", parsed.IMDBID).Msg("found NFO")
		return parsed
	}

	return nil
}

func parseNFO(r io.Reader, kind string) (*ParsedNFO, error) {
	data, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return nil, err
	}
	return parseNFOBytes(data, kind)
}

func parseNFOBytes(data []byte, kind string) (*ParsedNFO, error) {
	data = stripBOM(data)

	switch kind {
	case "tvshow":
		var tv TVShowNFO
		if err := xml.Unmarshal(data, &tv); err != nil {
			return nil, err
		}
		p := &ParsedNFO{
			Title:         tv.Title,
			OriginalTitle: tv.OriginalTitle,
			Year:          tv.Year,
			Plot:          tv.Plot,
			IMDBID:        tv.IMDBID,
			TMDBID:        tv.TMDBID,
			TVDBID:        tv.TVDBID,
			Rating:        tv.Rating,
			Genres:        tv.Genre,
			Tags:          tv.Tag,
			Studios:       tv.Studio,
			Actors:        tv.Actors,
			Kind:          "tvshow",
		}
		fillFromUniqueIDs(p, tv.UniqueIDs)
		return p, nil

	case "movie":
		var m MovieNFO
		if err := xml.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		p := &ParsedNFO{
			Title:         m.Title,
			OriginalTitle: m.OriginalTitle,
			Year:          m.Year,
			Plot:          m.Plot,
			IMDBID:        m.IMDBID,
			TMDBID:        m.TMDBID,
			Rating:        m.Rating,
			Genres:        m.Genre,
			Tags:          m.Tag,
			Studios:       m.Studio,
			Actors:        m.Actors,
			Kind:          "movie",
		}
		fillFromUniqueIDs(p, m.UniqueIDs)
		return p, nil

	case "artist":
		var a ArtistNFO
		if err := xml.Unmarshal(data, &a); err != nil {
			return nil, err
		}
		return &ParsedNFO{
			Title: a.Name,
			Plot:  a.Biography,
			MBID:  a.MBID,
			Kind:  "artist",
		}, nil
	}

	return nil, nil
}

func fillFromUniqueIDs(p *ParsedNFO, ids []UniqueID) {
	for _, uid := range ids {
		val := strings.TrimSpace(uid.Value)
		if val == "" {
			continue
		}
		switch strings.ToLower(uid.Type) {
		case "tmdb":
			if p.TMDBID == "" {
				p.TMDBID = val
			}
		case "imdb":
			if p.IMDBID == "" {
				p.IMDBID = val
			}
		case "tvdb":
			if p.TVDBID == "" {
				p.TVDBID = val
			}
		}
	}
}

func stripBOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func readFileBytes(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 1<<20))
}
