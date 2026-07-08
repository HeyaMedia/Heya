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

	if got := countInventoryFiles(inv); got != 189 {
		t.Fatalf("classified inventory files: got %d, want 189", got)
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
	assertMusicAlbum(t, byAlbum, "Yoshiko And Alee", "Freaks Out", "2022", "single", 1)
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
			{ProviderID: "heya:artist:mbid:wrong-ado", ProviderName: "heya", Title: "ADO", Confidence: 0.95, ExternalIDs: map[string]string{"mbid": "wrong-ado"}},
			{ProviderID: "heya:artist:discogs:4017157", ProviderName: "heya", Title: "ADO (9)", Confidence: 0.95, ExternalIDs: map[string]string{"discogs": "4017157"}},
			{ProviderID: "heya:artist:apple:123", ProviderName: "heya", Title: "Ado", Confidence: 0.95, ExternalIDs: map[string]string{"apple": "123", "mbid": "ado-mbid", "deezer": "456", "discogs": "789"}},
			{ProviderID: "heya:artist:mbid:ado-other", ProviderName: "heya", Title: "ADO Project", Confidence: 0.95},
			{ProviderID: "heya:artist:mbid:ado-kojo", ProviderName: "heya", Title: "Ado Kojo", Confidence: 0.95, AltTitles: []string{"Ado"}},
		},
		"Heya Test Tones": nil,
	}}
	artists := []MusicArtistPlan{
		{Key: "artist:ado", Artist: "Ado"},
		{Key: "artist:ano", Artist: "ano", ExternalIDs: map[string]string{"mbid": "ebb4513e-4aab-4ac9-a949-14e77bb7b836"}},
		{Key: "artist:heya test tones", Artist: "Heya Test Tones"},
	}

	search, err := SearchMusicArtists(context.Background(), artists, provider, emit)
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
	if !byKey["artist:ado"].Accepted || byKey["artist:ado"].ProviderID != "heya:artist:mbid:ado-mbid" {
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
	if !byKey["artist:ano"].Accepted || byKey["artist:ano"].ProviderID != "heya:artist:mbid:ebb4513e-4aab-4ac9-a949-14e77bb7b836" {
		t.Fatalf("ano direct search: %#v", byKey["artist:ano"])
	}
	if byKey["artist:heya test tones"].Accepted || byKey["artist:heya test tones"].Reason != "no_candidates" {
		t.Fatalf("test tones search: %#v", byKey["artist:heya test tones"])
	}
	if provider.calls["ano"] != 0 {
		t.Fatalf("direct MBID artist should not call search provider: calls=%#v", provider.calls)
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
	f.calls[query.Title]++
	return f.results[query.Title], nil
}
