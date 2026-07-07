package nfo

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTVShowNFO(t *testing.T) {
	xml := `<?xml version="1.0" encoding="utf-8"?>
<tvshow>
  <title>Breaking Bad</title>
  <originaltitle>Breaking Bad</originaltitle>
  <year>2008</year>
  <plot>A chemistry teacher diagnosed with cancer.</plot>
  <rating>9.5</rating>
  <imdb_id>tt0903747</imdb_id>
  <tmdbid>1396</tmdbid>
  <tvdbid>81189</tvdbid>
  <genre>Drama</genre>
  <genre>Crime</genre>
  <studio>AMC</studio>
  <actor>
    <name>Bryan Cranston</name>
    <role>Walter White</role>
    <sortorder>0</sortorder>
  </actor>
</tvshow>`

	parsed, err := parseNFO(strings.NewReader(xml), "tvshow")
	require.NoError(t, err)
	assert.Equal(t, "Breaking Bad", parsed.Title)
	assert.Equal(t, "2008", parsed.Year)
	assert.Equal(t, "tt0903747", parsed.IMDBID)
	assert.Equal(t, "1396", parsed.TMDBID)
	assert.Equal(t, "81189", parsed.TVDBID)
	assert.Equal(t, "tvshow", parsed.Kind)
	assert.Equal(t, []string{"Drama", "Crime"}, parsed.Genres)
	assert.Equal(t, []string{"AMC"}, parsed.Studios)
	require.Len(t, parsed.Actors, 1)
	assert.Equal(t, "Bryan Cranston", parsed.Actors[0].Name)
	assert.Equal(t, "Walter White", parsed.Actors[0].Role)
}

func TestParseMovieNFO(t *testing.T) {
	xml := `<?xml version="1.0"?>
<movie>
  <title>Inception</title>
  <year>2010</year>
  <tagline>Your mind is the scene of the crime.</tagline>
  <imdb_id>tt1375666</imdb_id>
  <tmdbid>27205</tmdbid>
  <genre>Sci-Fi</genre>
  <genre>Action</genre>
</movie>`

	parsed, err := parseNFO(strings.NewReader(xml), "movie")
	require.NoError(t, err)
	assert.Equal(t, "Inception", parsed.Title)
	assert.Equal(t, "2010", parsed.Year)
	assert.Equal(t, "tt1375666", parsed.IMDBID)
	assert.Equal(t, "27205", parsed.TMDBID)
	assert.Equal(t, "movie", parsed.Kind)
	assert.Equal(t, []string{"Sci-Fi", "Action"}, parsed.Genres)
}

func TestParseArtistNFO(t *testing.T) {
	xml := `<?xml version="1.0"?>
<artist>
  <title>Radiohead</title>
  <biography>English rock band.</biography>
  <musicbrainzartistid>a74b1b7f-71a5-4011-9441-d0b5e4122711</musicbrainzartistid>
  <genre>Alternative Rock</genre>
  <genre>Experimental</genre>
  <album><title>Radiohead - Album - 1997 - OK Computer</title></album>
</artist>`

	parsed, err := parseNFO(strings.NewReader(xml), "artist")
	require.NoError(t, err)
	assert.Equal(t, "Radiohead", parsed.Title)
	assert.Equal(t, "English rock band.", parsed.Plot)
	assert.Equal(t, "a74b1b7f-71a5-4011-9441-d0b5e4122711", parsed.MBID)
	assert.Equal(t, "artist", parsed.Kind)
	assert.Equal(t, []string{"Alternative Rock", "Experimental"}, parsed.Genres)
	assert.Equal(t, []string{"Radiohead - Album - 1997 - OK Computer"}, parsed.AlbumTitles)
}

func TestParseAlbumNFO(t *testing.T) {
	xml := `<?xml version="1.0"?>
<album>
  <title>OK Computer</title>
  <year>1997</year>
  <releasedate>1997-05-21</releasedate>
  <artist>Radiohead</artist>
  <albumartist>Radiohead</albumartist>
  <musicbrainzalbumid>0b6b4ba0-d36f-47bd-b4ea-6a5b91842d28</musicbrainzalbumid>
  <musicbrainzalbumartistid>a74b1b7f-71a5-4011-9441-d0b5e4122711</musicbrainzalbumartistid>
  <musicbrainzreleasegroupid>b1392450-e666-3926-a536-22c65f834433</musicbrainzreleasegroupid>
  <genre>Alternative Rock</genre>
  <track><disc>1</disc><position>1</position><title>Airbag</title><duration>04:44</duration></track>
  <track><disc>1</disc><position>2</position><title>Paranoid Android</title><duration>06:23</duration></track>
</album>`

	parsed, err := parseNFO(strings.NewReader(xml), "album")
	require.NoError(t, err)
	assert.Equal(t, "OK Computer", parsed.Title)
	assert.Equal(t, "1997", parsed.Year)
	assert.Equal(t, "1997-05-21", parsed.ReleaseDate)
	assert.Equal(t, "Radiohead", parsed.AlbumArtist)
	assert.Equal(t, "0b6b4ba0-d36f-47bd-b4ea-6a5b91842d28", parsed.MBAlbumID)
	assert.Equal(t, "a74b1b7f-71a5-4011-9441-d0b5e4122711", parsed.MBAlbumArtistID)
	assert.Equal(t, "b1392450-e666-3926-a536-22c65f834433", parsed.MBReleaseGroupID)
	assert.Equal(t, "album", parsed.Kind)
	assert.Equal(t, []string{"Alternative Rock"}, parsed.Genres)
	require.Len(t, parsed.Tracks, 2)
	assert.Equal(t, "Airbag", parsed.Tracks[0].Title)
	assert.Equal(t, 1, parsed.Tracks[0].Disc)
	assert.Equal(t, 1, parsed.Tracks[0].Position)
	assert.Equal(t, "06:23", parsed.Tracks[1].Duration)
}

func TestParseBOMHandling(t *testing.T) {
	bom := "\xEF\xBB\xBF"
	xml := bom + `<?xml version="1.0"?>
<movie><title>BOM Movie</title><year>2020</year></movie>`

	parsed, err := parseNFO(strings.NewReader(xml), "movie")
	require.NoError(t, err)
	assert.Equal(t, "BOM Movie", parsed.Title)
}

func TestParseMalformedXML(t *testing.T) {
	_, err := parseNFO(strings.NewReader("<not>valid<xml"), "movie")
	assert.Error(t, err)
}

func TestParseUniqueIDs(t *testing.T) {
	xml := `<?xml version="1.0"?>
<movie>
  <title>Test</title>
  <uniqueid type="tmdb" default="true">12345</uniqueid>
  <uniqueid type="imdb">tt9999999</uniqueid>
  <uniqueid type="tvdb">67890</uniqueid>
</movie>`

	parsed, err := parseNFO(strings.NewReader(xml), "movie")
	require.NoError(t, err)
	assert.Equal(t, "12345", parsed.TMDBID)
	assert.Equal(t, "tt9999999", parsed.IMDBID)
}

func TestFindAndParseWithMapFS(t *testing.T) {
	fsys := fstest.MapFS{
		"shows/tvshow.nfo": &fstest.MapFile{
			Data: []byte(`<?xml version="1.0"?><tvshow><title>Test Show</title><year>2023</year><tmdbid>999</tmdbid></tvshow>`),
		},
	}

	parsed := FindAndParse(fsys, "shows")
	require.NotNil(t, parsed)
	assert.Equal(t, "Test Show", parsed.Title)
	assert.Equal(t, "999", parsed.TMDBID)
}

func TestFindAndParseEmptyDir(t *testing.T) {
	fsys := fstest.MapFS{
		"emptydir/somefile.txt": &fstest.MapFile{Data: []byte("not nfo")},
	}

	parsed := FindAndParse(fsys, "emptydir")
	assert.Nil(t, parsed)
}

func TestFindAndParseRealFixture(t *testing.T) {
	dir := "../../testdata/library/anime/Attack on Titan (2013)"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("fixture not present")
	}
	parsed := FindAndParseInDir(dir)
	require.NotNil(t, parsed)
	assert.Equal(t, "Attack on Titan", parsed.Title)
	assert.Equal(t, "1429", parsed.TMDBID)
	assert.Equal(t, "tt2560140", parsed.IMDBID)
	assert.Equal(t, "2013", parsed.Year)
	assert.Equal(t, "tvshow", parsed.Kind)
}

func TestStripBOM(t *testing.T) {
	withBOM := []byte{0xEF, 0xBB, 0xBF, 'h', 'i'}
	assert.Equal(t, []byte("hi"), stripBOM(withBOM))

	noBOM := []byte("hello")
	assert.Equal(t, []byte("hello"), stripBOM(noBOM))
}
