package scanner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/karbowiak/heya/internal/metadata"
)

type productionMusicReviewCorpus struct {
	CapturedAt string                      `json:"captured_at"`
	Source     string                      `json:"source"`
	Cases      []productionMusicReviewCase `json:"cases"`
}

type productionMusicReviewCase struct {
	CreditedArtist  string            `json:"credited_artist"`
	StorageArtist   string            `json:"storage_artist"`
	FallbackArtist  string            `json:"fallback_artist"`
	LiteralIdentity bool              `json:"literal_identity"`
	NeedsContext    bool              `json:"needs_context"`
	Album           string            `json:"album"`
	Year            string            `json:"year"`
	Track           string            `json:"track"`
	AlbumIDs        map[string]string `json:"album_identifiers"`
}

func TestProductionMusicReviewCorpusResolutionFlow(t *testing.T) {
	corpus := loadProductionMusicReviewCorpus(t)
	if corpus.CapturedAt != "2026-07-14" || len(corpus.Cases) < 20 {
		t.Fatalf("production corpus metadata/case count = %#v / %d", corpus.CapturedAt, len(corpus.Cases))
	}

	for index, fixture := range corpus.Cases {
		fixture := fixture
		t.Run(fixture.CreditedArtist, func(t *testing.T) {
			if fixture.CreditedArtist == "" || fixture.StorageArtist == "" || fixture.Album == "" || fixture.Year == "" || fixture.Track == "" {
				t.Fatalf("incomplete production fixture: %#v", fixture)
			}
			if fixture.NeedsContext {
				// These are deliberately retained as counterexamples: first-credit
				// parsing contradicts the storage grouping, so title parsing alone
				// must not silently choose an identity.
				primary := musicPrimaryCollaborationArtist(fixture.CreditedArtist)
				if primary == "" || normalizeMusicKeyPart(primary) == normalizeMusicKeyPart(fixture.StorageArtist) {
					t.Fatalf("context fixture stopped exercising an ambiguous credit: primary=%q fixture=%#v", primary, fixture)
				}
				return
			}

			providerID := "heyametadata:v2:entity:10000000-0000-4000-8000-" + productionCorpusID(index)
			provider := &fakeMusicSearchProvider{results: map[string][]metadata.SearchResult{}}
			expectedArtist := fixture.CreditedArtist
			if fixture.LiteralIdentity {
				provider.results[fixture.CreditedArtist] = []metadata.SearchResult{{
					ProviderID: providerID, ProviderName: "heya", Title: fixture.CreditedArtist,
					Recommendation: "strong_match", Evidence: []metadata.SearchEvidence{{Field: "releases", Outcome: "1_of_1"}},
				}}
			} else {
				if fixture.FallbackArtist == "" {
					t.Fatalf("fixture has no resolution expectation: %#v", fixture)
				}
				if got := musicPrimaryCollaborationArtist(fixture.CreditedArtist); got != fixture.FallbackArtist {
					t.Fatalf("primary collaboration credit = %q, want %q", got, fixture.FallbackArtist)
				}
				expectedArtist = fixture.FallbackArtist
				provider.results[fixture.CreditedArtist] = nil
				provider.results[fixture.FallbackArtist] = []metadata.SearchResult{{
					ProviderID: providerID, ProviderName: "heya", Title: fixture.FallbackArtist,
					Recommendation: "strong_match", Evidence: []metadata.SearchEvidence{{Field: "releases", Outcome: "1_of_1"}},
				}}
			}

			artist := MusicArtistPlan{
				Key: "artist:" + normalizeMusicKeyPart(fixture.CreditedArtist), Artist: fixture.CreditedArtist,
				Albums: []MusicAlbumPlan{{
					Artist: fixture.CreditedArtist, Album: fixture.Album, Year: fixture.Year,
					ReleaseKind: "single", ExternalIDs: fixture.AlbumIDs,
					Tracks: []MusicTrackPlan{{TrackTitle: fixture.Track}},
				}},
			}
			results, err := SearchMusicArtists(context.Background(), []MusicArtistPlan{artist}, provider, &captureEmitter{}, musicArtistAutoMatchThreshold)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || !results[0].Accepted || results[0].Artist != expectedArtist {
				t.Fatalf("production collaboration resolution = %#v", results)
			}
			if provider.calls[fixture.CreditedArtist] != 1 {
				t.Fatalf("literal identity was not tried exactly once: calls=%#v", provider.calls)
			}
			if fixture.LiteralIdentity {
				if len(provider.calls) != 1 {
					t.Fatalf("resolved literal identity was unnecessarily split: calls=%#v", provider.calls)
				}
			} else if provider.calls[fixture.FallbackArtist] != 1 {
				t.Fatalf("fallback identity was not tried: calls=%#v", provider.calls)
			}

			hints := provider.queries[fixture.CreditedArtist].Releases
			if len(hints) != 1 || hints[0].Title != fixture.Album || hints[0].Year != fixture.Year {
				t.Fatalf("release hints = %#v", hints)
			}
			if !reflect.DeepEqual(hints[0].Identifiers, fixture.AlbumIDs) {
				t.Fatalf("release identifiers = %#v, want %#v", hints[0].Identifiers, fixture.AlbumIDs)
			}
		})
	}
}

func TestMusicReleaseHintIdentifiersKeepReleaseIDsOnly(t *testing.T) {
	got := musicReleaseHintIdentifiers(map[string]string{
		"itunes_album":              "1630125755",
		"itunes_artist":             "591024034",
		"deezer_album":              "123",
		"musicbrainz_album":         "release-id",
		"musicbrainz_release_group": "group-id",
		"musicbrainz_album_artist":  "artist-id",
	})
	want := map[string]string{
		"itunes_album":              "1630125755",
		"deezer_album":              "123",
		"musicbrainz_album":         "release-id",
		"musicbrainz_release_group": "group-id",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("release-only identifiers = %#v, want %#v", got, want)
	}
}

func loadProductionMusicReviewCorpus(t *testing.T) productionMusicReviewCorpus {
	t.Helper()
	path := filepath.Join(testdataRoot(t), "scanner", "music-production-review-corpus.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read production music corpus: %v", err)
	}
	var corpus productionMusicReviewCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("decode production music corpus: %v", err)
	}
	return corpus
}

func productionCorpusID(index int) string {
	// Twelve decimal digits keep the fake UUID shape valid and deterministic.
	const digits = "000000000000"
	value := []byte(digits)
	for position, n := len(value)-1, index+1; position >= 0 && n > 0; position, n = position-1, n/10 {
		value[position] = byte('0' + n%10)
	}
	return string(value)
}
