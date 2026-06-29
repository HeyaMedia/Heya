package saver

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/rs/zerolog/log"
)

type MovieNFO struct {
	XMLName       xml.Name   `xml:"movie"`
	Title         string     `xml:"title"`
	OriginalTitle string     `xml:"originaltitle,omitempty"`
	SortTitle     string     `xml:"sorttitle,omitempty"`
	Year          string     `xml:"year,omitempty"`
	Plot          string     `xml:"plot,omitempty"`
	Tagline       string     `xml:"tagline,omitempty"`
	Runtime       int32      `xml:"runtime,omitempty"`
	Rating        string     `xml:"rating,omitempty"`
	UniqueIDs     []UniqueID `xml:"uniqueid"`
	Genres        []string   `xml:"genre"`
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

// ArtistNFO mirrors the schema that internal/nfo reads back. Field order +
// element names match real Jellyfin/Emby artist.nfo so round-tripping works.
type ArtistNFO struct {
	XMLName        xml.Name      `xml:"artist"`
	Title          string        `xml:"title"`
	SortName       string        `xml:"sortname,omitempty"`
	Disambiguation string        `xml:"disambiguation,omitempty"`
	Biography      string        `xml:"biography,omitempty"`
	MBID           string        `xml:"musicbrainzartistid,omitempty"`
	Genres         []string      `xml:"genre,omitempty"`
	Albums         []AlbumRefNFO `xml:"album,omitempty"`
}

type AlbumRefNFO struct {
	Title string `xml:"title"`
}

type AlbumNFO struct {
	XMLName          xml.Name        `xml:"album"`
	Title            string          `xml:"title"`
	Artist           string          `xml:"artist,omitempty"`
	AlbumArtist      string          `xml:"albumartist,omitempty"`
	Year             string          `xml:"year,omitempty"`
	ReleaseDate      string          `xml:"releasedate,omitempty"`
	AlbumType        string          `xml:"type,omitempty"`
	Label            string          `xml:"label,omitempty"`
	Country          string          `xml:"country,omitempty"`
	Barcode          string          `xml:"barcode,omitempty"`
	MBAlbumID        string          `xml:"musicbrainzalbumid,omitempty"`
	MBAlbumArtistID  string          `xml:"musicbrainzalbumartistid,omitempty"`
	MBReleaseGroupID string          `xml:"musicbrainzreleasegroupid,omitempty"`
	Genres           []string        `xml:"genre,omitempty"`
	Tracks           []AlbumTrackNFO `xml:"track,omitempty"`
}

type AlbumTrackNFO struct {
	Disc     int    `xml:"disc"`
	Position int    `xml:"position"`
	Title    string `xml:"title"`
	Duration string `xml:"duration,omitempty"` // MM:SS
}

// WriteArtistNFO writes an artist.nfo at the artist directory. Overwrites
// any existing file (the matcher's enrichment data is canonical and replaces
// stale on-disk NFOs). artistDir is typically Library/Artist/.
func WriteArtistNFO(artistDir string, artist sqlc.Artist, mediaItem sqlc.MediaItem, albumTitles []string) error {
	nfoPath := filepath.Join(artistDir, "artist.nfo")

	refs := make([]AlbumRefNFO, 0, len(albumTitles))
	for _, t := range albumTitles {
		if t != "" {
			refs = append(refs, AlbumRefNFO{Title: t})
		}
	}

	nfo := ArtistNFO{
		Title:          artist.Name,
		SortName:       artist.SortName,
		Disambiguation: artist.Disambiguation,
		Biography:      artist.Biography,
		MBID:           artist.MusicbrainzID,
		Albums:         refs,
	}
	_ = mediaItem // reserved for future genre/external_ids merging
	return writeXML(nfoPath, nfo)
}

// WriteAlbumNFO writes album.nfo at the release directory. tracks must be
// pre-ordered by (disc, position) for stable round-tripping.
func WriteAlbumNFO(releaseDir string, artist sqlc.Artist, album sqlc.Album, tracks []sqlc.Track) error {
	nfoPath := filepath.Join(releaseDir, "album.nfo")

	trackNFOs := make([]AlbumTrackNFO, 0, len(tracks))
	for _, t := range tracks {
		disc := int(t.DiscNumber)
		if disc == 0 {
			disc = 1
		}
		trackNFOs = append(trackNFOs, AlbumTrackNFO{
			Disc:     disc,
			Position: int(t.TrackNumber),
			Title:    t.Title,
			Duration: formatDurationMinSec(int(t.Duration)),
		})
	}

	releaseDateStr := ""
	if album.ReleaseDate.Valid {
		releaseDateStr = album.ReleaseDate.Time.Format("2006-01-02")
	}

	nfo := AlbumNFO{
		Title:           album.Title,
		Artist:          artist.Name,
		AlbumArtist:     artist.Name,
		Year:            album.Year,
		ReleaseDate:     releaseDateStr,
		AlbumType:       album.AlbumType,
		Label:           album.Label,
		Country:         album.Country,
		Barcode:         album.Barcode,
		MBAlbumID:       album.MusicbrainzID,
		MBAlbumArtistID: artist.MusicbrainzID,
		Genres:          append([]string(nil), album.Genres...),
		Tracks:          trackNFOs,
	}
	return writeXML(nfoPath, nfo)
}

func formatDurationMinSec(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
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
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}
