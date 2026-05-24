package heyamedia

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFlexIDsUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want map[string]string
	}{
		{
			name: "all string values (legacy shape)",
			raw:  `{"tmdb":"27205","imdb":"tt1375666"}`,
			want: map[string]string{"tmdb": "27205", "imdb": "tt1375666"},
		},
		{
			name: "all numeric values (revamped shape)",
			raw:  `{"tmdb":1429,"tvdb":267440}`,
			want: map[string]string{"tmdb": "1429", "tvdb": "267440"},
		},
		{
			name: "mixed string and numeric",
			raw:  `{"tmdb":1429,"imdb":"tt2560140","mbid":"abc-123"}`,
			want: map[string]string{"tmdb": "1429", "imdb": "tt2560140", "mbid": "abc-123"},
		},
		{
			name: "booleans coerced to string",
			raw:  `{"enriched":true,"locked":false}`,
			want: map[string]string{"enriched": "true", "locked": "false"},
		},
		{
			name: "null and missing skipped",
			raw:  `{"tmdb":1429,"imdb":null}`,
			want: map[string]string{"tmdb": "1429"},
		},
		{
			name: "nested object value skipped",
			raw:  `{"tmdb":1429,"complex":{"nested":"yes"}}`,
			want: map[string]string{"tmdb": "1429"},
		},
		{
			name: "array value skipped",
			raw:  `{"tmdb":1429,"list":[1,2,3]}`,
			want: map[string]string{"tmdb": "1429"},
		},
		{
			name: "empty object",
			raw:  `{}`,
			want: map[string]string{},
		},
		{
			name: "negative numbers preserved",
			raw:  `{"score":-42}`,
			want: map[string]string{"score": "-42"},
		},
		{
			name: "float values",
			raw:  `{"popularity":1.5}`,
			want: map[string]string{"popularity": "1.5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got flexIDs
			err := json.Unmarshal([]byte(tt.raw), &got)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, map[string]string(got))
		})
	}
}

func TestFlexIDsUnmarshalNull(t *testing.T) {
	// Top-level null on the field should leave the map nil, not panic.
	var got flexIDs
	err := json.Unmarshal([]byte(`null`), &got)
	assert.NoError(t, err)
	assert.Nil(t, map[string]string(got))
}

func TestFlexIDsUnmarshalInStruct(t *testing.T) {
	// End-to-end: the field decodes correctly as part of a SearchHit
	// with the new HeyaMedia response shape.
	raw := `{
		"id": "tmdb:1429",
		"kind": "tv",
		"name": "Attack on Titan",
		"year": 2013,
		"sources": ["tmdb"],
		"external_ids": {"tmdb": 1429},
		"alt_titles": ["Shingeki no Kyojin"],
		"score": 1,
		"enriched": true
	}`
	var hit SearchHit
	err := json.Unmarshal([]byte(raw), &hit)
	assert.NoError(t, err)
	assert.Equal(t, "1429", hit.ExternalIDs["tmdb"])
	assert.Equal(t, "Attack on Titan", hit.Name)
	assert.Equal(t, []string{"Shingeki no Kyojin"}, hit.AltTitles)
	assert.True(t, hit.Enriched)
}
