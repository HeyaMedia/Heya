package scanner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/karbowiak/heya/internal/audiotags"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/karbowiak/heya/internal/musicconsensus"
	"github.com/karbowiak/heya/internal/nfo"
	"github.com/karbowiak/heya/internal/titlematch"
)

func TestMusicFixtureProducesLocalPlans(t *testing.T) {
	musicDir := filepath.Join(testdataRoot(t), "library", "music")
	if _, err := os.Stat(musicDir); os.IsNotExist(err) {
		t.Skip("testdata/library/music not found")
	}

	emit := &captureEmitter{}
	inv, err := WalkInventory(context.Background(), []string{musicDir}, emit)
	if err != nil {
		t.Fatalf("walk inventory: %v", err)
	}

	tracks, albums, artists, err := AnalyzeMusic(context.Background(), inv, emit)
	if err != nil {
		t.Fatalf("analyze music: %v", err)
	}

	if got := countInventoryFiles(inv); got != 190 {
		t.Fatalf("classified inventory files: got %d, want 190", got)
	}
	if got := len(inventoryFilesByClass(inv, ClassArtwork)); got != 47 {
		t.Fatalf("local artwork: got %d, want 47", got)
	}
	if got := len(inventoryFilesByClass(inv, ClassLyrics)); got != 19 {
		t.Fatalf("local lyrics: got %d, want 19", got)
	}
	if got := len(tracks); got != 94 {
		t.Fatalf("music track plans: got %d, want 94", got)
	}
	if got := len(albums); got != 22 {
		t.Fatalf("music album plans: got %d, want 22", got)
	}
	if got := len(artists); got != 16 {
		t.Fatalf("music artist plans: got %d, want 16", got)
	}
	if got := countEvents(emit.events, "music.file.unplanned"); got != 6 {
		t.Fatalf("unplanned music files: got %d, want 6", got)
	}
	if got := countEvents(emit.events, "nfo.parse_failed"); got != 1 {
		t.Fatalf("NFO failures: got %d, want 1", got)
	}

	byAlbum := indexMusicAlbums(albums)
	assertMusicAlbum(t, byAlbum, "Various Artists", "Trainspotting", "1996", "compilation", 3)
	assertMusicAlbum(t, byAlbum, "The Seatbelts", "Cowboy Bebop OST 1", "1998", "", 2)
	assertMusicAlbum(t, byAlbum, "Aphex Twin", "Selected Ambient Works 85-92", "1992", "", 3)
	assertMusicAlbum(t, byAlbum, "Nujabes", "Metaphorical Music", "2003", "", 2)
	assertMusicAlbum(t, byAlbum, "Lady Gaga & Bradley Cooper", "A Star Is Born", "2018", "", 2)
	assertMusicAlbum(t, byAlbum, "Yoshiko", "Freaks Out", "2022", "single", 1)
	yoshiko := byAlbum[musicAlbumKey("Yoshiko", "Freaks Out", "2022")]
	if yoshiko.ExternalIDs["itunes_album"] != "1630125755" || yoshiko.ExternalIDs["itunes_artist"] != "591024034" {
		t.Fatalf("Yoshiko durable IDs: %#v", yoshiko.ExternalIDs)
	}
	assertMusicAlbum(t, byAlbum, "Daft Punk", "Discovery", "2001", "album", 6)
	assertMusicAlbum(t, byAlbum, "ano", "ちゅ、多様性。", "2022", "single", 2)
	assertMusicAlbumAlias(t, byAlbum, "ano", "ちゅ、多様性。", "2022", "Chu,Tayousei.")

	aphex := byAlbum[musicAlbumKey("Aphex Twin", "Selected Ambient Works 85-92", "1992")]
	assertMusicTrack(t, aphex, "Blue Calx", 2, 1, nil)
	nujabes := byAlbum[musicAlbumKey("Nujabes", "Metaphorical Music", "2003")]
	assertMusicTrack(t, nujabes, "Horn in the Middle", 1, 0, []string{"missing_track_number"})

	assertMusicUnplanned(t, emit.events,
		"Absolutely Cursed Audio/2026 - totally an album maybe.wav",
		"Absolutely Cursed Audio/___FINAL_MASTER_USE_THIS_ONE.flac",
		"Absolutely Cursed Audio/track.mp3",
		"Loose Tracks/03 - Unknown Artist - Mystery.flac",
		"Loose Tracks/Daft Punk - One More Time.mp3",
		"Loose Tracks/no-track-number-song.ogg",
	)

	var report bytes.Buffer
	WriteReport(&report, sqlc.Library{ID: 1, Name: "DevMusic", MediaType: sqlc.MediaTypeMusic}, Result{
		Inventory:    inv,
		MusicTracks:  tracks,
		MusicAlbums:  albums,
		MusicArtists: artists,
	}, emit.events)
	for _, want := range []string{
		"Music scan report: DevMusic (id=1)",
		"Audio track plans:      94",
		"Local album identities: 22",
		"Local artist identities: 16",
		"Unplanned audio:        6",
		"Needs review: incomplete music tracks",
		"Nujabes/2003 - Metaphorical Music/A2 - Horn in the Middle.mp3",
		"Artist plan overview",
		"Various Artists [artist:various artists] albums=1 tracks=3",
		"album: Trainspotting (1996) tracks=3 kind=compilation",
		"Search was not run. Music matching will be artist-first",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("music report missing %q:\n%s", want, report.String())
		}
	}
}

func TestRunLibrarySupportsMusicReport(t *testing.T) {
	musicDir := filepath.Join(testdataRoot(t), "library", "music")
	if _, err := os.Stat(musicDir); os.IsNotExist(err) {
		t.Skip("testdata/library/music not found")
	}

	var out bytes.Buffer
	result, err := RunLibrary(context.Background(), sqlc.Library{
		ID:        1,
		Name:      "DevMusic",
		MediaType: sqlc.MediaTypeMusic,
		Paths:     []string{musicDir},
	}, Options{Report: true}, &out)
	if err != nil {
		t.Fatalf("run music library: %v", err)
	}
	if len(result.MusicTracks) != 94 {
		t.Fatalf("runner music tracks: got %d, want 94", len(result.MusicTracks))
	}
	if len(result.MusicAlbums) != 22 {
		t.Fatalf("runner music albums: got %d, want 22", len(result.MusicAlbums))
	}
	if len(result.MusicArtists) != 16 {
		t.Fatalf("runner music artists: got %d, want 16", len(result.MusicArtists))
	}
	report := out.String()
	for _, want := range []string{
		"Music scan report: DevMusic (id=1)",
		"Search selected:        not run",
		"Search was not run. Music matching will be artist-first",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("music runner report missing %q:\n%s", want, report)
		}
	}
}

func TestSearchMusicArtistsSelectsAndRejects(t *testing.T) {
	emit := &captureEmitter{}
	provider := &fakeMusicSearchProvider{results: map[string][]metadata.SearchResult{
		"Ado": {
			{ProviderID: "heyametadata:v2:entity:10000000-0000-4000-8000-000000000001", ProviderName: "heya", Title: "ADO", Confidence: 0.95, ExternalIDs: map[string]string{"mbid": "wrong-ado"}},
			{ProviderID: "heyametadata:v2:entity:10000000-0000-4000-8000-000000000002", ProviderName: "heya", Title: "ADO (9)", Confidence: 0.95, ExternalIDs: map[string]string{"discogs": "4017157"}},
			{ProviderID: "heyametadata:v2:entity:10000000-0000-4000-8000-000000000003", ProviderName: "heya", Title: "Ado", Confidence: 0.95, ExternalIDs: map[string]string{"apple": "123", "mbid": "ado-mbid", "deezer": "456", "discogs": "789"}},
			{ProviderID: "heyametadata:v2:entity:10000000-0000-4000-8000-000000000004", ProviderName: "heya", Title: "ADO Project", Confidence: 0.95},
			{ProviderID: "heyametadata:v2:entity:10000000-0000-4000-8000-000000000005", ProviderName: "heya", Title: "Ado Kojo", Confidence: 0.95, AltTitles: []string{"Ado"}},
		},
		"ano":             {{ProviderID: "heyametadata:v2:entity:20000000-0000-4000-8000-000000000001", ProviderName: "heya", Title: "ano", Confidence: 1, ExternalIDs: map[string]string{"mbid": "ebb4513e-4aab-4ac9-a949-14e77bb7b836"}}},
		"Heya Test Tones": nil,
	}}
	artists := []MusicArtistPlan{
		{Key: "artist:ado", Artist: "Ado", Albums: []MusicAlbumPlan{
			{Album: "Single First", Year: "2020", ReleaseKind: "single", Tracks: []MusicTrackPlan{{}}},
			{Album: "Album Short", Year: "2022", ReleaseKind: "album", Tracks: []MusicTrackPlan{{}}},
			{Album: "Album Long", Year: "2023", ReleaseKind: "album", Tracks: []MusicTrackPlan{{}, {}}},
			{Album: "EP", Year: "2021", ReleaseKind: "ep", Tracks: []MusicTrackPlan{{}}},
		}},
		{Key: "artist:ano", Artist: "ano", ExternalIDs: map[string]string{"mbid": "ebb4513e-4aab-4ac9-a949-14e77bb7b836"}},
		{Key: "artist:heya test tones", Artist: "Heya Test Tones"},
	}

	search, err := SearchMusicArtists(context.Background(), artists, provider, emit, 0)
	if err != nil {
		t.Fatalf("search music artists: %v", err)
	}
	if len(search) != 3 {
		t.Fatalf("music search rows: got %d, want 3", len(search))
	}
	byKey := map[string]MusicSearchMatch{}
	for _, item := range search {
		byKey[item.Key] = item
	}
	if !byKey["artist:ado"].Accepted || byKey["artist:ado"].ProviderID != "heyametadata:v2:entity:10000000-0000-4000-8000-000000000003" {
		t.Fatalf("Ado search: %#v", byKey["artist:ado"])
	}
	if score := musicNameSimilarity("Ado", "ADO (9)"); score >= musicArtistAutoMatchThreshold {
		t.Fatalf("numbered disambiguation scored too high: %.2f", score)
	}
	if score := musicNameSimilarity("Ado", "Ado Kojo"); score >= musicArtistAutoMatchThreshold {
		t.Fatalf("short artist substring scored too high: %.2f", score)
	}
	if score := scoreMusicSearchCandidate(MusicArtistPlan{Artist: "Ado"}, metadata.SearchResult{Title: "Ado Kojo", AltTitles: []string{"Ado"}}); score >= musicArtistAutoMatchThreshold {
		t.Fatalf("short artist alias scored too high: %.2f", score)
	}
	if !byKey["artist:ano"].Accepted || byKey["artist:ano"].ProviderID != "heyametadata:v2:entity:20000000-0000-4000-8000-000000000001" {
		t.Fatalf("ano identifier discovery: %#v", byKey["artist:ano"])
	}
	if byKey["artist:heya test tones"].Accepted || byKey["artist:heya test tones"].Reason != "no_candidates" {
		t.Fatalf("test tones search: %#v", byKey["artist:heya test tones"])
	}
	if provider.calls["ano"] != 1 || provider.queries["ano"].Identifiers["mbid"] != "ebb4513e-4aab-4ac9-a949-14e77bb7b836" {
		t.Fatalf("MBID evidence was not sent through unified discovery: calls=%#v query=%#v", provider.calls, provider.queries["ano"])
	}
	adoReleases := provider.queries["Ado"].Releases
	if len(adoReleases) != musicArtistDiscoveryReleaseHintLimit || adoReleases[0].Title != "Album Long" || adoReleases[1].Title != "Album Short" || adoReleases[2].Title != "EP" {
		t.Fatalf("bounded release evidence = %#v", adoReleases)
	}

	var report bytes.Buffer
	WriteReport(&report, sqlc.Library{ID: 1, Name: "DevMusic", MediaType: sqlc.MediaTypeMusic}, Result{
		MusicArtists: artists,
		MusicSearch:  search,
	}, emit.events)
	for _, want := range []string{
		"Search selected:        2/3",
		"Search review:          1 rejected, 0 suspicious selected",
		"Needs review: search rejected",
		"Heya Test Tones [artist:heya test tones] rejected reason=no_candidates",
		"Music search completed.",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("music search report missing %q:\n%s", want, report.String())
		}
	}
}

func TestMusicSearchPreservesStructuredCandidateConfidence(t *testing.T) {
	artist := MusicArtistPlan{Artist: "$Not"}

	// Production discovery commonly returns several exact-name provider
	// identities with no matching release evidence. Name similarity must not
	// flatten every one of those nuanced upstream scores to 1.0.
	ambiguous := metadata.SearchResult{
		Title: "$NOT", Confidence: .60, RequiresReview: true,
		Evidence: []metadata.SearchEvidence{{Field: "releases", Outcome: "0_of_1"}},
	}
	if got := scoreMusicSearchCandidate(artist, ambiguous); got != .60 {
		t.Fatalf("ambiguous exact-name score = %.2f, want .60", got)
	}

	// A query-only canonical index hit carries confidence=1 for search
	// ordering, not identity proof. Keep it below the automatic floor until
	// structured evidence or an identifier corroborates it.
	canonical := metadata.SearchResult{
		Title: "$Not", Confidence: 1, Enriched: true, RequiresReview: true,
	}
	if got := scoreMusicSearchCandidate(artist, canonical); got != musicQueryOnlyCanonicalConfidence {
		t.Fatalf("query-only canonical score = %.2f, want %.2f", got, musicQueryOnlyCanonicalConfidence)
	}

	corroborated := metadata.SearchResult{
		Title: "$Not", Confidence: .91,
		Evidence: []metadata.SearchEvidence{{Field: "releases", Outcome: "1_of_1"}},
	}
	if got := scoreMusicSearchCandidate(artist, corroborated); got != .91 {
		t.Fatalf("corroborated exact-name score = %.2f, want .91", got)
	}
}

func TestMusicSearchUsesConvergedFingerprintRecordingEvidence(t *testing.T) {
	const canonical = "20000000-0000-4000-8000-000000000001"
	search := &fakeMusicSearchProvider{results: map[string][]metadata.SearchResult{
		"$Not": {
			{ProviderID: "opaque-mb", ProviderName: "heya", Title: "$Not", Confidence: .60, Recommendation: "ambiguous", RequiresReview: true},
			{ProviderID: "opaque-apple", ProviderName: "heya", Title: "$NOT", Confidence: .60, Recommendation: "ambiguous", RequiresReview: true},
		},
	}}
	fingerprints := &fakeMusicFingerprintProvider{results: map[string][]MusicRecordingEvidence{
		"$Not/Ethereal/01.flac": {{
			RecordingMBID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Title: "Ethereal",
			FingerprintScore: .99, SourceDuration: 181, RecordingDuration: 180,
			Artists: []MusicRecordingArtistEvidence{{CanonicalID: canonical, Name: "$Not", MBID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"}},
		}},
	}}
	artist := MusicArtistPlan{Key: "artist:not", Artist: "$Not", Albums: []MusicAlbumPlan{{
		Album: "Ethereal", Tracks: []MusicTrackPlan{{TrackTitle: "Ethereal", RelPath: "$Not/Ethereal/01.flac"}},
	}}}

	results, err := SearchMusicArtistsWithFingerprints(context.Background(), []MusicArtistPlan{artist}, search, fingerprints, &captureEmitter{}, musicArtistAutoMatchThreshold)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Accepted || results[0].ProviderID != heyametadata.EncodeEntityProviderID(canonical) {
		t.Fatalf("fingerprint search = %#v", results)
	}
	if len(results[0].Candidates) != 1 || results[0].Candidates[0].Recommendation != "fingerprint_match" {
		t.Fatalf("fingerprint candidate = %#v", results[0].Candidates)
	}
}

func TestMusicSearchRejectsConflictingFingerprintArtists(t *testing.T) {
	search := &fakeMusicSearchProvider{results: map[string][]metadata.SearchResult{"Example": nil}}
	fingerprints := &fakeMusicFingerprintProvider{results: map[string][]MusicRecordingEvidence{
		"Example/One.flac": {{RecordingMBID: "one", Title: "One", FingerprintScore: .99, SourceDuration: 180, RecordingDuration: 180, Artists: []MusicRecordingArtistEvidence{{CanonicalID: "artist-one", Name: "Example"}}}},
		"Example/Two.flac": {{RecordingMBID: "two", Title: "Two", FingerprintScore: .99, SourceDuration: 200, RecordingDuration: 200, Artists: []MusicRecordingArtistEvidence{{CanonicalID: "artist-two", Name: "Example"}}}},
	}}
	artist := MusicArtistPlan{Key: "artist:example", Artist: "Example", Albums: []MusicAlbumPlan{{Album: "Album", Tracks: []MusicTrackPlan{
		{TrackTitle: "One", RelPath: "Example/One.flac"}, {TrackTitle: "Two", RelPath: "Example/Two.flac"},
	}}}}
	results, err := SearchMusicArtistsWithFingerprints(context.Background(), []MusicArtistPlan{artist}, search, fingerprints, &captureEmitter{}, musicArtistAutoMatchThreshold)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Accepted {
		t.Fatalf("conflicting fingerprint artists were accepted: %#v", results)
	}
}

func TestSearchMusicArtistsUsesConsistentMusicBrainzSpine(t *testing.T) {
	const providerID = "heyametadata:v2:entity:10000000-0000-4000-8000-000000000001"
	provider := &fakeMusicSearchProvider{results: map[string][]metadata.SearchResult{
		"Ado": {{ProviderID: providerID, ProviderName: "heya", Title: "Ado", Confidence: 1}},
	}}
	artist := MusicArtistPlan{
		Key: "artist:ado", Artist: "Ado",
		ExternalIDs: map[string]string{"mbid": "ado-mbid", "apple": "123"},
		Albums: []MusicAlbumPlan{{
			Album: "Kyougen", Year: "2022", ReleaseKind: "album",
			ExternalIDs: map[string]string{"musicbrainz_release_group": "release-group", "itunes_album": "456"},
		}},
	}

	results, err := SearchMusicArtists(context.Background(), []MusicArtistPlan{artist}, provider, &captureEmitter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Accepted {
		t.Fatalf("search result = %#v", results)
	}
	query := provider.queries["Ado"]
	if len(query.Identifiers) != 1 || query.Identifiers["mbid"] != "ado-mbid" {
		t.Fatalf("artist identifiers = %#v", query.Identifiers)
	}
	if len(query.Releases) != 1 || len(query.Releases[0].Identifiers) != 0 {
		t.Fatalf("release hints retained competing hard IDs = %#v", query.Releases)
	}
}

func TestSearchMusicArtistsAcceptsOnlyCanonicalCandidateConvergence(t *testing.T) {
	const (
		candidateA = "heyametadata:v2:candidate:artist:10000000-0000-4000-8000-000000000001"
		candidateB = "heyametadata:v2:candidate:artist:10000000-0000-4000-8000-000000000002"
		canonical  = "20000000-0000-4000-8000-000000000001"
	)
	search := &fakeMusicSearchProvider{results: map[string][]metadata.SearchResult{
		"$uicideboy$": {
			{ProviderID: candidateA, ProviderName: "heya", Title: "$uicideboy$", Recommendation: "conflicting_identifiers", RequiresReview: true},
			{ProviderID: candidateB, ProviderName: "heya", Title: "$uicideboy$", Recommendation: "conflicting_identifiers", RequiresReview: true},
		},
	}}
	provider := &convergingMusicSearchProvider{
		fakeMusicSearchProvider: search,
		details: map[string]*metadata.MediaDetail{
			candidateA: {CanonicalID: canonical, Title: "$uicideboy$", ArtistName: "$uicideboy$", ExternalIDs: map[string]string{"mbid": "artist-mbid"}},
			candidateB: {CanonicalID: canonical, Title: "$uicideboy$", ArtistName: "$uicideboy$", ExternalIDs: map[string]string{"mbid": "artist-mbid"}},
		},
	}

	results, err := SearchMusicArtists(context.Background(), []MusicArtistPlan{{Key: "artist:uicideboy", Artist: "$uicideboy$"}}, provider, &captureEmitter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Accepted || results[0].ProviderID != heyametadata.EncodeEntityProviderID(canonical) {
		t.Fatalf("converged search = %#v", results)
	}
	if len(results[0].Candidates) != 1 || results[0].Candidates[0].RequiresReview {
		t.Fatalf("converged candidates = %#v", results[0].Candidates)
	}

	provider.details[candidateB] = &metadata.MediaDetail{CanonicalID: "20000000-0000-4000-8000-000000000002", Title: "$uicideboy$"}
	results, err = SearchMusicArtists(context.Background(), []MusicArtistPlan{{Key: "artist:uicideboy", Artist: "$uicideboy$"}}, provider, &captureEmitter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Accepted {
		t.Fatalf("distinct same-name canonical artists were accepted = %#v", results)
	}
}

func TestSearchMusicArtistsPrefersApprovedDiscoveryOverReviewOnlyCanonicalHits(t *testing.T) {
	const selected = "heyametadata:v2:candidate:artist:10000000-0000-4000-8000-000000000001"
	provider := &fakeMusicSearchProvider{results: map[string][]metadata.SearchResult{
		"Daft Punk": {
			{ProviderID: "heyametadata:v2:entity:20000000-0000-4000-8000-000000000001", ProviderName: "heya", Title: "Daft Punk", RequiresReview: true},
			{ProviderID: "heyametadata:v2:entity:20000000-0000-4000-8000-000000000002", ProviderName: "heya", Title: "Daft Punk", RequiresReview: true},
			{ProviderID: selected, ProviderName: "heya", Title: "Daft Punk", Recommendation: "strong_match", Evidence: []metadata.SearchEvidence{{Field: "releases", Outcome: "3_of_3"}}, RequiresReview: false},
		},
	}}
	artists := []MusicArtistPlan{{
		Key: "artist:daft punk", Artist: "Daft Punk",
		Albums: []MusicAlbumPlan{{Album: "Homework", Year: "1997", ReleaseKind: "album"}},
	}}

	results, err := SearchMusicArtists(context.Background(), artists, provider, &captureEmitter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Accepted || results[0].ProviderID != selected {
		t.Fatalf("approved discovery did not outrank review-only suggestions: %#v", results)
	}
	if len(results[0].Candidates) != 3 || results[0].Candidates[0].ProviderID != selected {
		t.Fatalf("candidate order = %#v", results[0].Candidates)
	}
}

func TestSearchMusicArtistsAcceptsApprovedLocalizedAliasOverReviewOnlyNameHit(t *testing.T) {
	const selected = "heyametadata:v2:candidate:artist:10000000-0000-4000-8000-000000000001"
	provider := &fakeMusicSearchProvider{results: map[string][]metadata.SearchResult{
		"The Seatbelts": {
			{ProviderID: selected, ProviderName: "heya", Title: "シートベルツ", AltTitles: []string{"The Seatbelts"}, Recommendation: "strong_match", Evidence: []metadata.SearchEvidence{{Field: "releases", Outcome: "1_of_1"}}, RequiresReview: false},
			{ProviderID: "heyametadata:v2:entity:20000000-0000-4000-8000-000000000001", ProviderName: "heya", Title: "The Seatbelts", RequiresReview: true},
		},
	}}
	artists := []MusicArtistPlan{{
		Key: "artist:the seatbelts", Artist: "The Seatbelts",
		Albums: []MusicAlbumPlan{{Album: "Cowboy Bebop OST 1", Year: "1998"}},
	}}

	results, err := SearchMusicArtists(context.Background(), artists, provider, &captureEmitter{}, musicArtistAutoMatchThreshold)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Accepted || results[0].ProviderID != selected || results[0].Confidence != 1 {
		t.Fatalf("localized approved alias was not accepted: %#v", results)
	}
	if musicSearchSelectionLooksSuspicious(results[0]) {
		t.Fatalf("localized approved alias was re-opened for review: %#v", results[0])
	}
}

func TestSearchMusicArtistsFallsBackToPrimaryCollaborationCredit(t *testing.T) {
	const ladyGaga = "heyametadata:v2:candidate:artist:30000000-0000-4000-8000-000000000001"
	const simonAndGarfunkel = "heyametadata:v2:entity:30000000-0000-4000-8000-000000000002"
	provider := &fakeMusicSearchProvider{results: map[string][]metadata.SearchResult{
		"Lady Gaga & Bradley Cooper": nil,
		"Lady Gaga": {{
			ProviderID: ladyGaga, ProviderName: "heya", Title: "Lady Gaga",
			Recommendation: "strong_match", Evidence: []metadata.SearchEvidence{{Field: "releases", Outcome: "1_of_1"}},
		}},
		"Yoshiko And Alee": nil,
		"Yoshiko": {{
			ProviderID:   "heyametadata:v2:candidate:artist:30000000-0000-4000-8000-000000000003",
			ProviderName: "heya", Title: "Yoshiko", Recommendation: "ambiguous", RequiresReview: true,
			Evidence: []metadata.SearchEvidence{{Field: "releases", Outcome: "0_of_1"}},
		}},
		"Simon & Garfunkel": {{
			ProviderID: simonAndGarfunkel, ProviderName: "heya", Title: "Simon & Garfunkel",
			Recommendation: "strong_match", Evidence: []metadata.SearchEvidence{{Field: "releases", Outcome: "1_of_1"}},
		}},
	}}
	artists := []MusicArtistPlan{
		{Key: "artist:lady gaga bradley cooper", Artist: "Lady Gaga & Bradley Cooper", Albums: []MusicAlbumPlan{{Album: "A Star Is Born", Year: "2018"}}},
		{Key: "artist:yoshiko and alee", Artist: "Yoshiko And Alee", Albums: []MusicAlbumPlan{{Album: "Freaks Out", Year: "2022", ReleaseKind: "single"}}},
		{Key: "artist:simon garfunkel", Artist: "Simon & Garfunkel", Albums: []MusicAlbumPlan{{Album: "Bridge over Troubled Water", Year: "1970"}}},
	}

	results, err := SearchMusicArtists(context.Background(), artists, provider, &captureEmitter{}, musicArtistAutoMatchThreshold)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]MusicSearchMatch{}
	for _, result := range results {
		byKey[result.Key] = result
	}
	if result := byKey["artist:lady gaga bradley cooper"]; !result.Accepted || result.ProviderID != ladyGaga || result.Artist != "Lady Gaga" || result.Confidence != 1 {
		t.Fatalf("primary collaboration fallback = %#v", result)
	}
	if result := byKey["artist:yoshiko and alee"]; result.Accepted || result.Reason != "ambiguous_or_low_confidence" {
		t.Fatalf("ambiguous primary collaboration was accepted = %#v", result)
	}
	if result := byKey["artist:simon garfunkel"]; !result.Accepted || result.ProviderID != simonAndGarfunkel {
		t.Fatalf("literal collaboration identity = %#v", result)
	}
	if provider.calls["Lady Gaga"] != 1 || provider.queries["Lady Gaga"].Releases[0].Title != "A Star Is Born" {
		t.Fatalf("Lady Gaga fallback evidence = calls=%#v query=%#v", provider.calls, provider.queries["Lady Gaga"])
	}
	if provider.calls["Simon"] != 0 {
		t.Fatalf("resolved literal band was split: calls=%#v", provider.calls)
	}
}

func TestSearchMusicArtistsPrefersStorageOwnerForCollaborationCredit(t *testing.T) {
	const ado = "heyametadata:v2:entity:51497a0b-fd77-48dc-9cb0-bfe8ce205173"
	const aboveBeyond = "heyametadata:v2:entity:c1e3738b-8cf0-48bf-8da7-f25f32437805"
	provider := &fakeMusicSearchProvider{results: map[string][]metadata.SearchResult{
		"Jax Jones, Ado":                 {{ProviderID: "opaque-jax", ProviderName: "heya", Title: "Jax Jones", Confidence: 1, RequiresReview: true}},
		"Ado":                            {{ProviderID: ado, ProviderName: "heya", Title: "Ado", Recommendation: "strong_match"}},
		"Above & Beyond ft Zoe Johnston": {{ProviderID: "opaque-above", ProviderName: "heya", Title: "Above", Confidence: 1, RequiresReview: true}},
		"Above and Beyond":               {{ProviderID: aboveBeyond, ProviderName: "heya", Title: "Above & Beyond", Recommendation: "strong_match"}},
	}}
	artists := []MusicArtistPlan{
		{
			Key: "artist:jax jones ado", Artist: "Jax Jones, Ado",
			Files:  []string{"Ado (Japanese vocalist)/Jax Jones, Ado - Show/01.flac"},
			Albums: []MusicAlbumPlan{{Album: "Show (Jax Jones Remix)", Year: "2023"}},
		},
		{
			Key: "artist:above beyond ft zoe", Artist: "Above & Beyond ft Zoe Johnston",
			Files:  []string{"Above and Beyond/Above and Beyond - Single - 2024 - Crazy Love/01.flac"},
			Albums: []MusicAlbumPlan{{Album: "Crazy Love", Year: "2024"}},
		},
	}

	results, err := SearchMusicArtists(context.Background(), artists, provider, &captureEmitter{}, musicArtistAutoMatchThreshold)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]MusicSearchMatch{}
	for _, result := range results {
		byKey[result.Key] = result
	}
	if result := byKey["artist:jax jones ado"]; !result.Accepted || result.ProviderID != ado || result.Artist != "Ado" {
		t.Fatalf("Ado storage-owner fallback = %#v", result)
	}
	if result := byKey["artist:above beyond ft zoe"]; !result.Accepted || result.ProviderID != aboveBeyond || result.Artist != "Above & Beyond" {
		t.Fatalf("Above & Beyond storage-owner fallback = %#v", result)
	}
	if provider.calls["Above"] != 0 {
		t.Fatalf("storage owner succeeded but primary-token fallback still ran: calls=%#v", provider.calls)
	}
}

func TestMusicSearchFallbackArtistsRejectsUnrelatedStorageOwner(t *testing.T) {
	artist := MusicArtistPlan{
		Artist: "MINNIE",
		Files:  []string{"Explo/[2025] HER/MINNIE - HER - 02 - HER.flac"},
	}
	for _, fallback := range musicSearchFallbackArtists(artist) {
		if fallback == "Explo" {
			t.Fatalf("unrelated storage owner became a search fallback: %#v", musicSearchFallbackArtists(artist))
		}
	}
}

func TestMusicID3FixtureUsesEmbeddedTags(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not on PATH")
	}
	musicDir := filepath.Join(testdataRoot(t), "library", "musicidv3")
	if _, err := os.Stat(musicDir); os.IsNotExist(err) {
		t.Skip("testdata/library/musicidv3 not found")
	}

	emit := &captureEmitter{}
	inv, err := WalkInventory(context.Background(), []string{musicDir}, emit)
	if err != nil {
		t.Fatalf("walk inventory: %v", err)
	}

	tracks, albums, artists, err := AnalyzeMusicWithOptions(context.Background(), inv, emit, MusicAnalysisOptions{Probe: audiotags.ProbeFile})
	if err != nil {
		t.Fatalf("analyze tagged music: %v", err)
	}

	if got := countInventoryFiles(inv); got != 4 {
		t.Fatalf("classified tagged files: got %d, want 4", got)
	}
	if got := len(tracks); got != 4 {
		t.Fatalf("tagged track plans: got %d, want 4", got)
	}
	if got := len(albums); got != 2 {
		t.Fatalf("tagged album plans: got %d, want 2", got)
	}
	if got := len(artists); got != 2 {
		t.Fatalf("tagged artist plans: got %d, want 2", got)
	}
	if got := countEvents(emit.events, "music.tags.probed"); got != 4 {
		t.Fatalf("tag probe events: got %d, want 4", got)
	}
	if got := countEvents(emit.events, "music.file.unplanned"); got != 0 {
		t.Fatalf("unplanned tagged files: got %d, want 0", got)
	}

	byAlbum := indexMusicAlbums(albums)
	assertMusicAlbum(t, byAlbum, "ano", "ちゅ、多様性。", "2022", "", 2)
	assertMusicAlbumAlias(t, byAlbum, "ano", "ちゅ、多様性。", "2022", "Chu,Tayousei.")
	ano := byAlbum["musicbrainz_release_group:9b19bfab-7916-4ec2-b5ff-9bfa13056630"]
	if ano.ExternalIDs["musicbrainz_album"] != "a212268d-ea6f-4387-b09e-c20353130bb4" {
		t.Fatalf("ano album MBID: got %#v", ano.ExternalIDs)
	}
	assertMusicTrack(t, ano, "ちゅ、多様性。", 1, 1, nil)
	assertMusicTrack(t, ano, "Chu,Tayousei.", 1, 1, nil)

	assertMusicAlbum(t, byAlbum, "Ado", "狂言", "2022", "", 2)
	ado := byAlbum["musicbrainz_release_group:22222222-2222-4222-8222-222222222222"]
	assertMusicTrack(t, ado, "うっせぇわ", 1, 1, nil)
	assertMusicTrack(t, ado, "踊", 1, 2, nil)
}

func TestMusicTagTitleRejectsSyntheticProbeTitles(t *testing.T) {
	if shouldUseMusicTagTitle("Track 1", "brown (flac)", 1, "Track 1") {
		t.Fatalf("synthetic probe title should not replace placeholder path title")
	}
	if !shouldUseMusicTagTitle("Track 1", "Actual Song", 1, "Track 1") {
		t.Fatalf("real tag title should replace placeholder path title")
	}
}

func TestApplyMusicReleaseConsensusRejectsAsacoOutlierAndPoisonedNFO(t *testing.T) {
	evidence := make([]musicconsensus.Evidence, 0, 10)
	for range 9 {
		evidence = append(evidence, musicconsensus.Evidence{Artist: "Asaco", Album: "Nomake Story", Year: "2020"})
	}
	evidence = append(evidence, musicconsensus.Evidence{Artist: "DJ Paul", Album: "To Kill Again...The Mixtape", Year: "2010"})
	consensus := musicconsensus.Build(evidence)
	inherited := inheritMusicReleaseConsensus(audiotags.Tags{Title: "Untagged Track"}, consensus)
	if inherited.AlbumArtist != "Asaco" || inherited.Album != "Nomake Story" || inherited.Year != "2020" {
		t.Fatalf("missing release tags were not inherited: %#v", inherited)
	}
	badNFO := musicNFOEntry{nfo: &nfo.ParsedNFO{
		Kind:             "album",
		Title:            "Nomake Story",
		AlbumArtist:      "DJ Paul",
		Year:             "2010",
		MBAlbumID:        "97470000-0000-4000-8000-000000000000",
		MBAlbumArtistID:  "43906e48-a7c0-4b80-a5dd-37d1fe6ccdb9",
		MBReleaseGroupID: "fd292250-2220-490c-a277-0885bb5ca64d",
	}}
	outlierTags := audiotags.Tags{
		Artist:           "DJ Paul",
		AlbumArtist:      "DJ Paul",
		Album:            "To Kill Again...The Mixtape",
		Year:             "2010",
		ArtistMBID:       "43906e48-a7c0-4b80-a5dd-37d1fe6ccdb9",
		AlbumArtistMBID:  "43906e48-a7c0-4b80-a5dd-37d1fe6ccdb9",
		AlbumMBID:        "97470000-0000-4000-8000-000000000000",
		ReleaseGroupMBID: "fd292250-2220-490c-a277-0885bb5ca64d",
	}
	plan := MusicTrackPlan{
		Artist:      "DJ Paul",
		Album:       "Nomake Story",
		Year:        "2010",
		TrackTitle:  "Outro",
		RelPath:     "Asaco/Asaco - Nomake Story/06 - Outro.flac",
		Source:      "nfo",
		Confidence:  0.96,
		NFO:         "Asaco/Asaco - Nomake Story/album.nfo",
		ExternalIDs: musicExternalIDsFromNFO(badNFO.nfo),
	}

	got := applyMusicReleaseConsensus(plan, outlierTags, badNFO, consensus)
	if got.Artist != "Asaco" || got.Album != "Nomake Story" || got.Year != "2020" {
		t.Fatalf("consensus identity = %q / %q / %q", got.Artist, got.Album, got.Year)
	}
	if len(got.ExternalIDs) != 0 {
		t.Fatalf("outlier hard identifiers survived: %#v", got.ExternalIDs)
	}
	if got.NFO != "" || !contains(got.Issues, "nfo_rejected_by_folder_consensus") {
		t.Fatalf("poisoned NFO remained trusted: nfo=%q issues=%#v", got.NFO, got.Issues)
	}
	if !contains(got.Issues, "tag_outlier_rejected_by_folder_consensus") {
		t.Fatalf("outlier was not marked: %#v", got.Issues)
	}
}

func TestPlanMusicTrackDoesNotLeakFolderDisambiguationToReplacementArtist(t *testing.T) {
	file := InventoryFile{
		Name:    "0101 - Here With Me.flac",
		RelPath: "ATRIP (Patrick Alexander Pache)/d4vd - Album - 2024 - Petals to Thorns/0101 - Here With Me.flac",
		Ext:     ".flac",
	}
	plan, ok := planMusicTrack(file, nil, audiotags.Tags{
		AlbumArtist: "d4vd",
		Album:       "Petals to Thorns",
		Year:        "2024",
		Title:       "Here With Me",
		TrackNumber: 1,
	})
	if !ok {
		t.Fatal("track was not planned")
	}
	if plan.Artist != "d4vd" || plan.ArtistDisambiguation != "" {
		t.Fatalf("replacement artist inherited folder disambiguation: %#v", plan)
	}
}

func TestApplyMusicStorageOwnerCollapsesContainedCollaborationCredits(t *testing.T) {
	cases := []struct {
		path, credited, owner string
	}{
		{"Ado (Japanese vocalist)/Jax Jones, Ado - Show/01.flac", "Jax Jones, Ado", "Ado"},
		{"Above and Beyond/Above and Beyond - Crazy Love/01.flac", "Above & Beyond ft Zoe Johnston", "Above and Beyond"},
		{"Atarashii Gakko!/Tokyo Calling/01.flac", "88rising, ATARASHII GAKKO!, Warren Hue", "Atarashii Gakko!"},
	}
	for _, tc := range cases {
		plan := applyMusicStorageOwner(MusicTrackPlan{
			Artist: tc.credited, Album: "Release", Year: "2024", RelPath: tc.path,
			ExternalIDs: map[string]string{
				"musicbrainz_artist":       "artist-one;artist-two",
				"musicbrainz_album_artist": "artist-one;artist-two",
				"musicbrainz_album":        "release-id",
			},
		})
		if plan.Artist != tc.owner || !contains(plan.Issues, "artist_collaboration_collapsed_to_storage_owner") {
			t.Errorf("storage owner for %q = %#v", tc.credited, plan)
		}
		if plan.ExternalIDs["musicbrainz_artist"] != "" || plan.ExternalIDs["musicbrainz_album_artist"] != "" || plan.ExternalIDs["musicbrainz_album"] != "release-id" {
			t.Errorf("unsafe artist IDs were not isolated for %q: %#v", tc.credited, plan.ExternalIDs)
		}
	}
}

func TestApplyMusicStorageOwnerDoesNotRewriteMisfiledArtist(t *testing.T) {
	plan := MusicTrackPlan{
		Artist: "d4vd", Album: "Petals to Thorns", RelPath: "ATRIP (Patrick Alexander Pache)/d4vd - Album - 2024 - Petals to Thorns/01.flac",
		ExternalIDs: map[string]string{"musicbrainz_artist": "d4vd-id"},
	}
	got := applyMusicStorageOwner(plan)
	if got.Artist != "d4vd" || got.ExternalIDs["musicbrainz_artist"] != "d4vd-id" || len(got.Issues) != 0 {
		t.Fatalf("misfiled artist was rewritten to storage owner: %#v", got)
	}
	extended := applyMusicStorageOwner(MusicTrackPlan{
		Artist: "The Beatles Tribute Band", Album: "Tribute", RelPath: "The Beatles/Tribute/01.flac",
	})
	if extended.Artist != "The Beatles Tribute Band" {
		t.Fatalf("non-collaboration artist containing the owner was rewritten: %#v", extended)
	}
}

func TestGroupMusicArtistsRejectsInconsistentProviderIDs(t *testing.T) {
	albums := []MusicAlbumPlan{
		{Artist: "Example", Album: "One", ExternalIDs: map[string]string{"musicbrainz_album_artist": "mbid-one", "itunes_artist": "apple-one"}, Confidence: 1},
		{Artist: "Example", Album: "Two", ExternalIDs: map[string]string{"musicbrainz_album_artist": "mbid-two", "itunes_artist": "apple-one"}, Confidence: 1},
	}
	artists := groupMusicArtists(albums)
	if len(artists) != 1 {
		t.Fatalf("artists = %#v", artists)
	}
	if artists[0].ExternalIDs["mbid"] != "" || artists[0].ExternalIDs["apple"] != "apple-one" {
		t.Fatalf("consistent IDs = %#v", artists[0].ExternalIDs)
	}
	if !contains(artists[0].Issues, "conflicting_artist_mbid_ids") {
		t.Fatalf("artist issues = %#v", artists[0].Issues)
	}
}

func TestGroupMusicArtistsIntersectsMultiArtistProviderIDs(t *testing.T) {
	const primary = "6497cbed-bcc0-4492-95e9-6fe7e4826b5e"
	albums := []MusicAlbumPlan{
		{Artist: "Alex Mind", Album: "Solo", ExternalIDs: map[string]string{"musicbrainz_album_artist": primary}, Confidence: 1},
		{Artist: "Alex Mind", Album: "Collab One", ExternalIDs: map[string]string{"musicbrainz_album_artist": primary + ";1ed70d86-9f5d-4b82-9835-024e26c9ed05"}, Confidence: 1},
		{Artist: "Alex Mind", Album: "Collab Two", ExternalIDs: map[string]string{"musicbrainz_album_artist": primary + ";ddfbbf71-e5a3-4bad-84ea-d5f88ec3df77"}, Confidence: 1},
	}
	artists := groupMusicArtists(albums)
	if len(artists) != 1 || artists[0].ExternalIDs["mbid"] != primary {
		t.Fatalf("multi-artist MBID intersection = %#v", artists)
	}
	if contains(artists[0].Issues, "conflicting_artist_mbid_ids") {
		t.Fatalf("shared primary MBID was marked conflicting: %#v", artists[0].Issues)
	}
}

func TestMusicReleaseDirScopesConsensusToAlbumFolder(t *testing.T) {
	cases := map[string]string{
		"Asaco/Asaco - Nomake Story/01.flac":        "Asaco/Asaco - Nomake Story",
		"Asaco/Asaco - Nomake Story/Disc 2/01.flac": "Asaco/Asaco - Nomake Story",
		"DJ Paul/Other Album/01.flac":               "DJ Paul/Other Album",
	}
	for path, want := range cases {
		if got := musicReleaseDir(path); got != want {
			t.Errorf("musicReleaseDir(%q) = %q, want %q", path, got, want)
		}
	}
}

func assertMusicAlbum(t *testing.T, albums map[string]MusicAlbumPlan, artist, album, year, releaseKind string, tracks int) {
	t.Helper()
	key := musicAlbumKey(artist, album, year)
	plan, ok := albums[key]
	if !ok {
		t.Fatalf("missing music album %s / %s (%s)", artist, album, year)
	}
	if plan.Artist != artist {
		t.Fatalf("%s artist: got %q, want %q", album, plan.Artist, artist)
	}
	if !musicAlbumHasTitle(plan, album) {
		t.Fatalf("%s album: got %q aliases=%#v, want %q", key, plan.Album, plan.Aliases, album)
	}
	if plan.Year != year {
		t.Fatalf("%s year: got %q, want %q", album, plan.Year, year)
	}
	if plan.ReleaseKind != releaseKind {
		t.Fatalf("%s release kind: got %q, want %q", album, plan.ReleaseKind, releaseKind)
	}
	if len(plan.Tracks) != tracks {
		t.Fatalf("%s tracks: got %d, want %d", album, len(plan.Tracks), tracks)
	}
}

func assertMusicAlbumAlias(t *testing.T, albums map[string]MusicAlbumPlan, artist, album, year, alias string) {
	t.Helper()
	key := musicAlbumKey(artist, album, year)
	plan, ok := albums[key]
	if !ok {
		t.Fatalf("missing music album %s / %s (%s)", artist, album, year)
	}
	if !musicAlbumHasTitle(plan, alias) {
		t.Fatalf("%s aliases: got canonical=%q aliases=%#v, want %q", album, plan.Album, plan.Aliases, alias)
	}
}

func indexMusicAlbums(albums []MusicAlbumPlan) map[string]MusicAlbumPlan {
	out := map[string]MusicAlbumPlan{}
	for _, album := range albums {
		out[album.Key] = album
		out[musicAlbumKey(album.Artist, album.Album, album.Year)] = album
		for _, alias := range album.Aliases {
			out[musicAlbumKey(album.Artist, alias, album.Year)] = album
		}
		for key, value := range album.ExternalIDs {
			switch key {
			case "musicbrainz_release_group":
				out["musicbrainz_release_group:"+value] = album
			case "musicbrainz_album":
				out["musicbrainz_album:"+value] = album
			case "itunes_album":
				out["itunes_album:"+value] = album
			case "audiodb_album":
				out["audiodb_album:"+value] = album
			}
		}
	}
	return out
}

func musicAlbumHasTitle(plan MusicAlbumPlan, title string) bool {
	if plan.Album == title || titlematch.FuzzyEqual(plan.Album, title) {
		return true
	}
	for _, alias := range plan.Aliases {
		if alias == title || titlematch.FuzzyEqual(alias, title) {
			return true
		}
	}
	return false
}

func assertMusicTrack(t *testing.T, album MusicAlbumPlan, title string, disc, track int, issues []string) {
	t.Helper()
	for _, plan := range album.Tracks {
		if plan.TrackTitle != title {
			continue
		}
		if plan.DiscNumber != disc {
			t.Fatalf("%s disc: got %d, want %d", title, plan.DiscNumber, disc)
		}
		if plan.TrackNumber != track {
			t.Fatalf("%s track: got %d, want %d", title, plan.TrackNumber, track)
		}
		for _, issue := range issues {
			if !contains(plan.Issues, issue) {
				t.Fatalf("%s issues: got %#v, want %s", title, plan.Issues, issue)
			}
		}
		return
	}
	t.Fatalf("missing track %q in album %#v", title, album)
}

func assertMusicUnplanned(t *testing.T, events []Event, relPaths ...string) {
	t.Helper()
	got := musicUnplannedPaths(events)
	sort.Strings(got)
	sort.Strings(relPaths)
	if strings.Join(got, "\n") != strings.Join(relPaths, "\n") {
		t.Fatalf("unplanned music files:\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(relPaths, "\n"))
	}
}

func musicUnplannedPaths(events []Event) []string {
	events = eventsByName(events, "music.file.unplanned")
	got := make([]string, len(events))
	for i, ev := range events {
		got[i] = ev.RelPath
	}
	sort.Strings(got)
	return got
}

type fakeMusicSearchProvider struct {
	mu      sync.Mutex
	results map[string][]metadata.SearchResult
	calls   map[string]int
	queries map[string]metadata.SearchQuery
}

type convergingMusicSearchProvider struct {
	*fakeMusicSearchProvider
	details map[string]*metadata.MediaDetail
}

type fakeMusicFingerprintProvider struct {
	results map[string][]MusicRecordingEvidence
}

func (f *fakeMusicFingerprintProvider) MatchTrack(_ context.Context, track MusicTrackPlan) ([]MusicRecordingEvidence, error) {
	return f.results[track.RelPath], nil
}

func (f *convergingMusicSearchProvider) GetDetail(_ context.Context, providerID string, _ *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	return f.details[providerID], nil
}

func (f *fakeMusicSearchProvider) Search(_ context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	if kind != metadata.KindMusic {
		return nil, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	if f.queries == nil {
		f.queries = map[string]metadata.SearchQuery{}
	}
	f.calls[query.Title]++
	f.queries[query.Title] = query
	return f.results[query.Title], nil
}
