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
  <name>Radiohead</name>
  <biography>English rock band.</biography>
  <musicBrainzArtistID>a74b1b7f-71a5-4011-9441-d0b5e4122711</musicBrainzArtistID>
</artist>`

	parsed, err := parseNFO(strings.NewReader(xml), "artist")
	require.NoError(t, err)
	assert.Equal(t, "Radiohead", parsed.Title)
	assert.Equal(t, "English rock band.", parsed.Plot)
	assert.Equal(t, "a74b1b7f-71a5-4011-9441-d0b5e4122711", parsed.MBID)
	assert.Equal(t, "artist", parsed.Kind)
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
	if _, err := os.Stat("../../testdata/scanner/tv/Chainsaw Man (2022)"); err != nil {
		t.Skip("fixture not present")
	}
	parsed := FindAndParseInDir("../../testdata/scanner/tv/Chainsaw Man (2022)")
	require.NotNil(t, parsed)
	assert.Equal(t, "Chainsaw Man", parsed.Title)
	assert.Equal(t, "114410", parsed.TMDBID)
	assert.Equal(t, "tt13616990", parsed.IMDBID)
	assert.Equal(t, "2022", parsed.Year)
	assert.Equal(t, "tvshow", parsed.Kind)
}

func TestStripBOM(t *testing.T) {
	withBOM := []byte{0xEF, 0xBB, 0xBF, 'h', 'i'}
	assert.Equal(t, []byte("hi"), stripBOM(withBOM))

	noBOM := []byte("hello")
	assert.Equal(t, []byte("hello"), stripBOM(noBOM))
}
