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
	XMLName     xml.Name      `xml:"artist"`
	Title       string        `xml:"title"`
	Name        string        `xml:"name"`
	Biography   string        `xml:"biography"`
	Born        string        `xml:"born"`
	Died        string        `xml:"died"`
	MBID        string        `xml:"musicbrainzartistid"`
	AudioDBID   string        `xml:"audiodbartistid"`
	Disambig    string        `xml:"disambiguation"`
	SortName    string        `xml:"sortname"`
	Genres      []string      `xml:"genre"`
	Art         ArtNFO        `xml:"art"`
	AlbumTitles []AlbumRefNFO `xml:"album"`
}

type AlbumRefNFO struct {
	Title string `xml:"title"`
}

type AlbumNFO struct {
	XMLName          xml.Name   `xml:"album"`
	Title            string     `xml:"title"`
	Artist           string     `xml:"artist"`
	AlbumArtist      string     `xml:"albumartist"`
	Year             string     `xml:"year"`
	Premiered        string     `xml:"premiered"`
	ReleaseDate      string     `xml:"releasedate"`
	Runtime          string     `xml:"runtime"`
	Review           string     `xml:"review"`
	Outline          string     `xml:"outline"`
	Genres           []string   `xml:"genre"`
	Studios          []string   `xml:"studio"`
	Tags             []string   `xml:"tag"`
	Label            string     `xml:"label"`
	Country          string     `xml:"country"`
	Barcode          string     `xml:"barcode"`
	AlbumType        string     `xml:"type"`
	MBAlbumID        string     `xml:"musicbrainzalbumid"`
	MBAlbumArtistID  string     `xml:"musicbrainzalbumartistid"`
	MBReleaseGroupID string     `xml:"musicbrainzreleasegroupid"`
	AudioDBAlbumID   string     `xml:"audiodbalbumid"`
	AudioDBArtistID  string     `xml:"audiodbartistid"`
	Art              ArtNFO     `xml:"art"`
	Tracks           []TrackNFO `xml:"track"`
}

type TrackNFO struct {
	Disc     int    `xml:"disc"`
	Position int    `xml:"position"`
	Title    string `xml:"title"`
	Duration string `xml:"duration"`
}

type ArtNFO struct {
	Poster    string   `xml:"poster"`
	Fanart    []string `xml:"fanart"`
	Banner    string   `xml:"banner"`
	Clearart  string   `xml:"clearart"`
	Clearlogo string   `xml:"clearlogo"`
	Disc      string   `xml:"disc"`
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
	Title            string
	OriginalTitle    string
	Year             string
	Plot             string
	TMDBID           string
	IMDBID           string
	TVDBID           string
	MBID             string
	MBAlbumID        string
	MBAlbumArtistID  string
	MBReleaseGroupID string
	AlbumArtist      string
	ReleaseDate      string
	Disambiguation   string
	SortName         string
	Label            string
	Country          string
	Barcode          string
	AlbumType        string
	Rating           string
	Genres           []string
	Tags             []string
	Studios          []string
	Actors           []ActorNFO
	Tracks           []TrackNFO
	Art              ArtNFO
	AlbumTitles      []string
	Kind             string // "tvshow", "movie", "artist", "album"
}

func FindAndParse(fsys fs.FS, dir string) *ParsedNFO {
	nfoFiles := []struct {
		name string
		kind string
	}{
		{"tvshow.nfo", "tvshow"},
		{"movie.nfo", "movie"},
		{"artist.nfo", "artist"},
		{"album.nfo", "album"},
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
		{"album.nfo", "album"},
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
		title := a.Title
		if title == "" {
			title = a.Name
		}
		albumTitles := make([]string, 0, len(a.AlbumTitles))
		for _, al := range a.AlbumTitles {
			if al.Title != "" {
				albumTitles = append(albumTitles, al.Title)
			}
		}
		return &ParsedNFO{
			Title:          title,
			Plot:           a.Biography,
			MBID:           a.MBID,
			Disambiguation: a.Disambig,
			SortName:       a.SortName,
			Genres:         a.Genres,
			Art:            a.Art,
			AlbumTitles:    albumTitles,
			Kind:           "artist",
		}, nil

	case "album":
		var al AlbumNFO
		if err := xml.Unmarshal(data, &al); err != nil {
			return nil, err
		}
		artistName := al.AlbumArtist
		if artistName == "" {
			artistName = al.Artist
		}
		return &ParsedNFO{
			Title:            al.Title,
			Year:             al.Year,
			Plot:             al.Outline,
			AlbumArtist:      artistName,
			ReleaseDate:      firstNonEmpty(al.ReleaseDate, al.Premiered),
			Label:            al.Label,
			Country:          al.Country,
			Barcode:          al.Barcode,
			AlbumType:        al.AlbumType,
			Genres:           al.Genres,
			Tags:             al.Tags,
			Studios:          al.Studios,
			MBAlbumID:        al.MBAlbumID,
			MBAlbumArtistID:  al.MBAlbumArtistID,
			MBReleaseGroupID: al.MBReleaseGroupID,
			Tracks:           al.Tracks,
			Art:              al.Art,
			Kind:             "album",
		}, nil
	}

	return nil, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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
