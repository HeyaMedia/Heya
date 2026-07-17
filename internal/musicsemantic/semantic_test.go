package musicsemantic

import (
	"reflect"
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/metadata"
)

func TestFromRecordingBuildsFocusedMusicalDocument(t *testing.T) {
	facets := FromRecording(metadata.RecordingMetadata{
		Title: "Ignored title", ArtistName: "Ignored artist", Disambiguation: "live remix",
		Genres: []string{"J-Rock", "Rock"},
		Tags:   []string{"aggressive", "rock", "female vocals", "energetic"},
		Credits: []metadata.RecordingCredit{
			{Role: "instrument", Attributes: []string{"electric guitar", "drums"}, ArtistName: "Ignored musician"},
			{Role: "vocal", Attributes: []string{"lead vocals"}, ArtistName: "Ignored singer"},
			{Role: "producer", ArtistName: "Ignored producer"},
		},
	})
	doc := Document(facets)
	for _, wanted := range []string{"J-Rock", "aggressive", "energetic", "electric guitar", "drums", "lead vocals", "live", "remix"} {
		if !strings.Contains(doc, wanted) {
			t.Errorf("document %q does not contain %q", doc, wanted)
		}
	}
	for _, unwanted := range []string{"Ignored title", "Ignored artist", "Ignored musician", "Ignored producer"} {
		if strings.Contains(doc, unwanted) {
			t.Errorf("document %q contains identity/editorial context %q", doc, unwanted)
		}
	}
}

func TestSharedTermsPrioritizesCharacterFacets(t *testing.T) {
	seed := Facets{Genres: []string{"rock"}, Tags: []string{"Japanese"}, Moods: []string{"aggressive"}, Instrumentation: []string{"guitar"}}
	candidate := Facets{Genres: []string{"rock"}, Tags: []string{"Japanese"}, Moods: []string{"aggressive"}, Instrumentation: []string{"guitar"}}
	if got, want := SharedTerms(seed, candidate, 3), []string{"aggressive", "guitar", "Japanese"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SharedTerms = %#v, want %#v", got, want)
	}
}
