package musicbrainz

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestProvider(t *testing.T, handler http.Handler) *Provider {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	p := NewProvider()
	p.BaseURL = ts.URL
	p.CoverArtURL = ts.URL
	return p
}

func TestSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/release-group/", func(w http.ResponseWriter, r *http.Request) {
		resp := searchResponse{
			ReleaseGroups: []releaseGroupResult{
				{
					ID:           "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
					Title:        "OK Computer",
					PrimaryType:  "Album",
					FirstRelease: "1997-05-28",
					ArtistCredit: []artistCredit{{Artist: artistRef{ID: "artist1", Name: "Radiohead"}}},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	p := newTestProvider(t, mux)
	results, err := p.Search(context.Background(), metadata.KindMusic, metadata.SearchQuery{
		Artist: "Radiohead",
		Album:  "OK Computer",
	})
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, "Radiohead - OK Computer", results[0].Title)
	assert.Equal(t, "1997", results[0].Year)
	assert.Equal(t, "musicbrainz", results[0].ProviderName)
}

func TestGetDetail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/release-group/test-mbid", func(w http.ResponseWriter, r *http.Request) {
		rg := releaseGroupDetail{
			ID:           "test-mbid",
			Title:        "OK Computer",
			PrimaryType:  "Album",
			FirstRelease: "1997-05-28",
			ArtistCredit: []artistCredit{{Artist: artistRef{ID: "artist-mbid", Name: "Radiohead", SortName: "Radiohead"}}},
			Releases: []releaseRef{{
				ID:      "release1",
				Status:  "Official",
				Country: "GB",
				Barcode: "0634904078126",
				Date:    "1997-06-16",
				LabelInfo: []labelInfo{{
					Label: labelRef{Name: "Parlophone"},
				}},
			}},
			Genres: []genreTag{{Name: "alternative rock"}},
			Tags:   []genreTag{{Name: "rock"}},
		}
		json.NewEncoder(w).Encode(rg)
	})
	mux.HandleFunc("/release/release1", func(w http.ResponseWriter, r *http.Request) {
		rel := releaseDetail{
			Media: []mediaEntry{{
				Position: 1,
				Tracks: []trackEntry{
					{Position: 1, Title: "Airbag", Length: 283000, Recording: recordingRef{Title: "Airbag"}},
					{Position: 2, Title: "Paranoid Android", Length: 386000, Recording: recordingRef{Title: "Paranoid Android"}},
				},
			}},
		}
		json.NewEncoder(w).Encode(rel)
	})
	mux.HandleFunc("/release-group/test-mbid/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	p := newTestProvider(t, mux)
	detail, err := p.GetDetail(context.Background(), "musicbrainz:test-mbid", nil)
	require.NoError(t, err)

	assert.Equal(t, "OK Computer", detail.AlbumTitle)
	assert.Equal(t, "Radiohead", detail.ArtistName)
	assert.Equal(t, "Album", detail.AlbumType)
	assert.Equal(t, "1997", detail.Year)
	assert.Equal(t, "GB", detail.Country)
	assert.Equal(t, "0634904078126", detail.Barcode)
	assert.Equal(t, "Parlophone", detail.Label)
	assert.Equal(t, []string{"alternative rock"}, detail.Genres)
	require.Len(t, detail.Tracks, 2)
	assert.Equal(t, "Airbag", detail.Tracks[0].Title)
	assert.Equal(t, 283000, detail.Tracks[0].DurationMs)
}

func TestSupports(t *testing.T) {
	p := NewProvider()
	assert.True(t, p.Supports(metadata.KindMusic))
	assert.False(t, p.Supports(metadata.KindMovie))
	assert.False(t, p.Supports(metadata.KindTV))
	assert.False(t, p.Supports(metadata.KindBook))
}

func TestProviderName(t *testing.T) {
	p := NewProvider()
	assert.Equal(t, "musicbrainz", p.Name())
}

func TestGetDetailInvalidProviderID(t *testing.T) {
	p := NewProvider()
	_, err := p.GetDetail(context.Background(), "invalid", nil)
	assert.Error(t, err)
}

func TestSearchEmptyQuery(t *testing.T) {
	p := NewProvider()
	results, err := p.Search(context.Background(), metadata.KindMusic, metadata.SearchQuery{})
	assert.NoError(t, err)
	assert.Nil(t, results)
}
