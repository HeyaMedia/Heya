package saver

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/karbowiak/heya/internal/atomicfile"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/generatedwrite"
	"github.com/karbowiak/heya/internal/vfs"
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

func WriteMovieNFO(mediaDir string, item sqlc.MediaItemCard, movie sqlc.Movie) error {
	_, err := WriteMovieNFOWithResult(mediaDir, item, movie)
	return err
}

func WriteMovieNFOWithResult(mediaDir string, item sqlc.MediaItemCard, movie sqlc.Movie) (generatedwrite.Output, error) {
	nfoPath := MovieNFOPath(mediaDir)

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

	return writeXMLWithResult(nfoPath, nfo)
}

func PrepareMovieNFO(mediaDir string, item sqlc.MediaItemCard, movie sqlc.Movie) (*generatedwrite.Prepared, error) {
	rating := ""
	if f, err := movie.Rating.Float64Value(); err == nil && f.Valid {
		rating = fmt.Sprintf("%.1f", f.Float64)
	}
	return prepareXML(MovieNFOPath(mediaDir), MovieNFO{
		Title: item.Title, OriginalTitle: movie.OriginalTitle, SortTitle: item.SortTitle,
		Year: item.Year, Plot: item.Description, Tagline: movie.Tagline,
		Runtime: movie.RuntimeMinutes, Rating: rating, Genres: movie.Genres,
		UniqueIDs: buildUniqueIDs(item),
	})
}

func WriteTVShowNFO(mediaDir string, item sqlc.MediaItemCard, series sqlc.TvSeries) error {
	_, err := WriteTVShowNFOWithResult(mediaDir, item, series)
	return err
}

func WriteTVShowNFOWithResult(mediaDir string, item sqlc.MediaItemCard, series sqlc.TvSeries) (generatedwrite.Output, error) {
	nfoPath := TVShowNFOPath(mediaDir)

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

	return writeXMLWithResult(nfoPath, nfo)
}

func PrepareTVShowNFO(mediaDir string, item sqlc.MediaItemCard, series sqlc.TvSeries) (*generatedwrite.Prepared, error) {
	return prepareXML(TVShowNFOPath(mediaDir), TVShowNFO{
		Title: item.Title, OriginalTitle: series.OriginalName, SortTitle: item.SortTitle,
		Year: item.Year, Plot: item.Description, Status: series.Status,
		Genres: series.Genres, UniqueIDs: buildUniqueIDs(item),
	})
}

func buildUniqueIDs(item sqlc.MediaItemCard) []UniqueID {
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

// WriteArtistNFO writes an artist.nfo at the artist directory. An existing
// mismatch is always treated as user-owned source data and is never replaced.
// artistDir is typically Library/Artist/.
func WriteArtistNFO(artistDir string, artist sqlc.Artist, mediaItem sqlc.MediaItemCard, albumTitles []string) error {
	_, err := WriteArtistNFOWithResult(artistDir, artist, mediaItem, albumTitles)
	return err
}

func WriteArtistNFOWithResult(artistDir string, artist sqlc.Artist, mediaItem sqlc.MediaItemCard, albumTitles []string) (generatedwrite.Output, error) {
	nfoPath := ArtistNFOPath(artistDir)

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
	return writeXMLWithResult(nfoPath, nfo)
}

func PrepareArtistNFO(artistDir string, artist sqlc.Artist, mediaItem sqlc.MediaItemCard, albumTitles []string) (*generatedwrite.Prepared, error) {
	refs := make([]AlbumRefNFO, 0, len(albumTitles))
	for _, title := range albumTitles {
		if title != "" {
			refs = append(refs, AlbumRefNFO{Title: title})
		}
	}
	_ = mediaItem
	return prepareXML(ArtistNFOPath(artistDir), ArtistNFO{
		Title: artist.Name, SortName: artist.SortName, Disambiguation: artist.Disambiguation,
		Biography: artist.Biography, MBID: artist.MusicbrainzID, Albums: refs,
	})
}

// WriteAlbumNFO writes album.nfo at the release directory. tracks must be
// pre-ordered by (disc, position) for stable round-tripping.
func WriteAlbumNFO(releaseDir string, artist sqlc.Artist, album sqlc.Album, tracks []sqlc.Track) error {
	_, err := WriteAlbumNFOWithResult(releaseDir, artist, album, tracks)
	return err
}

func WriteAlbumNFOWithResult(releaseDir string, artist sqlc.Artist, album sqlc.Album, tracks []sqlc.Track) (generatedwrite.Output, error) {
	nfoPath := AlbumNFOPath(releaseDir)

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
	return writeXMLWithResult(nfoPath, nfo)
}

func PrepareAlbumNFO(releaseDir string, artist sqlc.Artist, album sqlc.Album, tracks []sqlc.Track) (*generatedwrite.Prepared, error) {
	trackNFOs := make([]AlbumTrackNFO, 0, len(tracks))
	for _, track := range tracks {
		disc := int(track.DiscNumber)
		if disc == 0 {
			disc = 1
		}
		trackNFOs = append(trackNFOs, AlbumTrackNFO{
			Disc: disc, Position: int(track.TrackNumber), Title: track.Title,
			Duration: formatDurationMinSec(int(track.Duration)),
		})
	}
	releaseDate := ""
	if album.ReleaseDate.Valid {
		releaseDate = album.ReleaseDate.Time.Format("2006-01-02")
	}
	return prepareXML(AlbumNFOPath(releaseDir), AlbumNFO{
		Title: album.Title, Artist: artist.Name, AlbumArtist: artist.Name,
		Year: album.Year, ReleaseDate: releaseDate, AlbumType: album.AlbumType,
		Label: album.Label, Country: album.Country, Barcode: album.Barcode,
		MBAlbumID: album.MusicbrainzID, MBAlbumArtistID: artist.MusicbrainzID,
		Genres: append([]string(nil), album.Genres...), Tracks: trackNFOs,
	})
}

func MovieNFOPath(mediaDir string) string   { return filepath.Join(mediaDir, "movie.nfo") }
func TVShowNFOPath(mediaDir string) string  { return filepath.Join(mediaDir, "tvshow.nfo") }
func ArtistNFOPath(artistDir string) string { return filepath.Join(artistDir, "artist.nfo") }
func AlbumNFOPath(releaseDir string) string { return filepath.Join(releaseDir, "album.nfo") }

var mediaContainerDir = regexp.MustCompile(`(?i)^(?:season|disc|cd)\s*\d+$`)

func formatDurationMinSec(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

func MediaDir(filePath string) string {
	dir := filepath.Dir(filePath)
	if mediaContainerDir.MatchString(filepath.Base(dir)) {
		dir = filepath.Dir(dir)
	}
	return dir
}

func writeXML(path string, v any) error {
	_, err := writeXMLWithResult(path, v)
	return err
}

func writeXMLWithResult(path string, v any) (generatedwrite.Output, error) {
	data, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return generatedwrite.Output{}, err
	}
	content := xml.Header + string(data) + "\n"
	contentBytes := []byte(content)
	if output, exists, err := attestExistingBytes(path, contentBytes); err != nil {
		return generatedwrite.Output{}, err
	} else if exists {
		if output.Attested {
			log.Debug().Str("path", vfs.RedactPath(path)).Msg("exact NFO already present; attesting without rewrite")
		} else {
			log.Debug().Str("path", vfs.RedactPath(path)).Msg("different NFO already exists; preserving user-owned file")
		}
		return output, nil
	}
	created, err := atomicfile.WriteIfAbsent(path, 0o644, func(writer io.Writer) error {
		_, err := writer.Write(contentBytes)
		return err
	})
	if err != nil {
		return generatedwrite.Output{}, err
	}
	if !created {
		// A file won the name after the initial check. Never replace it; attest
		// only when it contains the exact desired bytes (e.g. a concurrent retry).
		output, _, err := attestExistingBytes(path, contentBytes)
		return output, err
	}
	log.Info().Str("path", vfs.RedactPath(path)).Msg("NFO written")
	return generatedwrite.FromBytes(path, contentBytes), nil
}

func prepareXML(path string, v any) (*generatedwrite.Prepared, error) {
	data, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	content := append([]byte(xml.Header), data...)
	content = append(content, '\n')
	return generatedwrite.PrepareBytes(path, 0o644, content)
}

// attestExistingBytes distinguishes an exact retry from user-owned content.
// exists is true for every path occupant, including non-regular files.
func attestExistingBytes(path string, desired []byte) (output generatedwrite.Output, exists bool, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return generatedwrite.Output{}, false, nil
		}
		return generatedwrite.Output{}, false, err
	}
	if !info.Mode().IsRegular() || info.Size() != int64(len(desired)) {
		return generatedwrite.Output{Path: path}, true, nil
	}
	//nolint:gosec // size equality above bounds this read to desired content length.
	existing, err := os.ReadFile(path)
	if err != nil {
		return generatedwrite.Output{}, true, err
	}
	if !bytes.Equal(existing, desired) {
		return generatedwrite.Output{Path: path}, true, nil
	}
	return generatedwrite.AttestBytes(path, desired), true, nil
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
