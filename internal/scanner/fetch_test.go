package scanner

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

type fakeMovieDetailProvider struct {
	details map[string]*metadata.MediaDetail
	calls   []string
}

func (f *fakeMovieDetailProvider) GetDetail(_ context.Context, providerID string, _ *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	f.calls = append(f.calls, providerID)
	detail := f.details[providerID]
	if detail == nil {
		return nil, fmt.Errorf("missing detail")
	}
	return detail, nil
}

type fakeMusicDetailProvider struct {
	mu      sync.Mutex
	details map[string]*metadata.MediaDetail
	calls   []string
}

func (f *fakeMusicDetailProvider) GetDetail(_ context.Context, providerID string, _ *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, providerID)
	detail := f.details[providerID]
	if detail == nil {
		return nil, fmt.Errorf("missing detail")
	}
	return detail, nil
}

func TestFetchMovieMetadataPreviewsOnlyFetchesAcceptedMatches(t *testing.T) {
	provider := &fakeMovieDetailProvider{details: map[string]*metadata.MediaDetail{
		"heya:movie:tmdb:438631": {
			Title:          "Dune",
			Year:           "2021",
			Description:    "Spice must flow.",
			ExternalIDs:    map[string]string{"tmdb": "438631", "imdb": "tt1160419"},
			RuntimeMinutes: 155,
			Genres:         []string{"Science Fiction", "Adventure"},
			PosterURL:      "poster.webp",
			BackdropURL:    "backdrop.webp",
			Collection:     &metadata.CollectionDetail{Name: "Dune Collection"},
			Cast:           []metadata.CastMember{{Name: "Timothee Chalamet"}},
		},
	}}
	search := []MovieSearchMatch{
		{Accepted: true, Key: "tmdb:438631", ProviderID: "heya:movie:tmdb:438631", Title: "Dune", Year: "2021"},
		{Accepted: false, Key: "title_year:bad|2024", ProviderID: "heya:movie:tmdb:1", Title: "Bad", Year: "2024"},
	}

	emit := &captureEmitter{}
	previews, err := FetchMovieMetadataPreviews(context.Background(), search, provider, emit)
	if err != nil {
		t.Fatalf("fetch previews: %v", err)
	}
	if len(provider.calls) != 1 || provider.calls[0] != "heya:movie:tmdb:438631" {
		t.Fatalf("provider calls: %#v", provider.calls)
	}
	if len(previews) != 1 {
		t.Fatalf("previews: got %d, want 1", len(previews))
	}
	preview := previews[0]
	if preview.Title != "Dune" || preview.Year != "2021" {
		t.Fatalf("preview title/year: %#v", preview)
	}
	if preview.Collection != "Dune Collection" {
		t.Fatalf("collection: got %q", preview.Collection)
	}
	for _, field := range []string{"backdrop", "cast", "collection", "description", "external_ids", "genres", "poster", "runtime", "title", "year"} {
		if !contains(preview.WouldApply, field) {
			t.Fatalf("would_apply missing %q: %#v", field, preview.WouldApply)
		}
	}
	if !eventSeen(emit.events, "metadata.preview") {
		t.Fatalf("expected metadata.preview event")
	}
}

func TestFetchTVMetadataPreviewsMapsPlannedEpisodes(t *testing.T) {
	provider := &fakeMovieDetailProvider{details: map[string]*metadata.MediaDetail{
		"heya:tv:tmdb:1396": {
			Title:        "Breaking Bad",
			Year:         "2008",
			Description:  "A chemistry teacher makes choices.",
			ExternalIDs:  map[string]string{"tmdb": "1396", "tvdb": "81189"},
			Genres:       []string{"Drama"},
			PosterURL:    "poster.webp",
			BackdropURL:  "backdrop.webp",
			Status:       "Ended",
			FirstAirDate: "2008-01-20",
			LastAirDate:  "2013-09-29",
			Networks:     []metadata.NetworkDetail{{Name: "AMC"}},
			Artwork:      []metadata.ArtworkResult{{URL: "logo.webp", AssetType: "logo"}},
			Cast:         []metadata.CastMember{{Name: "Bryan Cranston"}},
			Crew:         []metadata.CrewMember{{Name: "Vince Gilligan", Job: "Creator"}},
			Seasons: []metadata.SeasonDetail{{
				Number: 1,
				Title:  "Season 1",
				Episodes: []metadata.EpisodeDetail{
					{Number: 1, Title: "Pilot"},
					{Number: 2, Title: "Cat's in the Bag..."},
				},
			}, {
				Number: 2,
				Title:  "Season 2",
				Episodes: []metadata.EpisodeDetail{
					{Number: 1, Title: "Seven Thirty-Seven"},
				},
			}},
		},
	}}
	search := []TVSearchMatch{
		{Accepted: true, Key: "tmdb:1396", ProviderID: "heya:tv:tmdb:1396", Title: "Breaking Bad", Year: "2008"},
		{Accepted: true, Key: "title:breaking bad", ProviderID: "heya:tv:tmdb:1396", Title: "Breaking Bad", Year: "2008"},
		{Accepted: false, Key: "title:poker face", ProviderID: "heya:tv:tmdb:120998", Title: "Poker Face", Year: "2023"},
	}
	matches := []TVMatch{
		{
			Key:   "tmdb:1396",
			Title: "Breaking Bad",
			Year:  "2008",
			Files: []string{
				"Breaking Bad (2008)/Season 01/Breaking.Bad.S01E01.mkv",
				"Breaking Bad (2008)/Season 01/Breaking.Bad.S01E02.mkv",
				"Breaking Bad (2008)/Season 01/Breaking.Bad.S01E99.mkv",
			},
			Episodes: []TVEpisodeRef{
				{Season: 1, Episode: 1},
				{Season: 1, Episode: 2},
				{Season: 1, Episode: 99},
			},
		},
		{
			Key:   "title:breaking bad",
			Title: "Breaking Bad",
			Files: []string{"Loose/Breaking.Bad.S02E01.mkv"},
			Episodes: []TVEpisodeRef{
				{Season: 2, Episode: 1},
			},
		},
	}

	emit := &captureEmitter{}
	previews, err := FetchTVMetadataPreviews(context.Background(), search, matches, provider, emit)
	if err != nil {
		t.Fatalf("fetch TV previews: %v", err)
	}
	if len(provider.calls) != 1 || provider.calls[0] != "heya:tv:tmdb:1396" {
		t.Fatalf("provider calls: %#v", provider.calls)
	}
	if len(previews) != 1 {
		t.Fatalf("previews: got %d, want 1", len(previews))
	}

	preview := previews[0]
	if preview.Title != "Breaking Bad" || preview.Year != "2008" {
		t.Fatalf("preview title/year: %#v", preview)
	}
	if preview.Seasons != 2 || preview.RemoteEpisodes != 3 {
		t.Fatalf("remote counts: seasons=%d episodes=%d", preview.Seasons, preview.RemoteEpisodes)
	}
	if preview.PlannedEpisodes != 4 || preview.MappedEpisodes != 3 {
		t.Fatalf("mapped episodes: got %d/%d, want 3/4", preview.MappedEpisodes, preview.PlannedEpisodes)
	}
	if preview.PlannedFiles != 4 || preview.LocalIdentities != 2 {
		t.Fatalf("local counts: files=%d identities=%d", preview.PlannedFiles, preview.LocalIdentities)
	}
	if len(preview.MissingEpisodes) != 1 || preview.MissingEpisodes[0].Episode != 99 {
		t.Fatalf("missing episodes: %#v", preview.MissingEpisodes)
	}
	for _, field := range []string{"backdrop", "cast", "crew", "description", "episodes", "external_ids", "first_air_date", "genres", "last_air_date", "networks", "poster", "seasons", "status", "title", "year"} {
		if !contains(preview.WouldApply, field) {
			t.Fatalf("would_apply missing %q: %#v", field, preview.WouldApply)
		}
	}
	if !eventSeen(emit.events, "metadata.preview") {
		t.Fatalf("expected metadata.preview event")
	}

	var report bytes.Buffer
	WriteReport(&report, sqlc.Library{ID: 2, Name: "DevTV", MediaType: sqlc.MediaTypeTv}, Result{
		TVMatches:  matches,
		TVSearch:   search,
		TVMetadata: previews,
	}, emit.events)
	for _, want := range []string{
		"Metadata fetched:      1/1",
		"Metadata fetch preview",
		"Breaking Bad (2008) provider=heya:tv:tmdb:1396",
		"mapped=3/4",
		"local_identities=2",
		"missing=S01E99",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("TV metadata report missing %q:\n%s", want, report.String())
		}
	}
}

func TestFetchMusicMetadataPreviewsMapsArtistDiscography(t *testing.T) {
	provider := &fakeMusicDetailProvider{details: map[string]*metadata.MediaDetail{
		"heya:artist:mbid:ado-mbid": {
			Title:          "Ado",
			ArtistName:     "Ado",
			ArtistSortName: "Ado",
			ExternalIDs:    map[string]string{"mbid": "ado-mbid"},
			ArtistTags:     []string{"j-pop"},
			ArtistImages:   []metadata.ArtworkResult{{URL: "ado.webp", AssetType: "poster"}},
			Albums: []metadata.AlbumEntry{
				{
					Title:       "狂言",
					Type:        "album",
					Year:        2022,
					ExternalIDs: map[string]string{"mb_release_group": "rg-kyogen", "mb_release": "release-kyogen"},
					Tracks: []metadata.TrackDetail{
						{DiscNumber: 1, TrackNumber: 1, Title: "うっせぇわ"},
						{DiscNumber: 1, TrackNumber: 2, Title: "踊"},
					},
				},
				{
					Title:       "Remote Title Drift",
					Type:        "single",
					Year:        2022,
					ExternalIDs: map[string]string{"mb_release_group": "rg-drift"},
					Tracks: []metadata.TrackDetail{
						{DiscNumber: 1, TrackNumber: 1, Title: "Remote Track"},
					},
				},
			},
		},
	}}
	artists := []MusicArtistPlan{{
		Key:    "artist:ado",
		Artist: "Ado",
		Albums: []MusicAlbumPlan{
			{
				Key:         "musicbrainz_release_group:rg-kyogen",
				Artist:      "Ado",
				Album:       "狂言",
				Year:        "2022",
				ReleaseKind: "album",
				ExternalIDs: map[string]string{"musicbrainz_release_group": "rg-kyogen"},
				Tracks: []MusicTrackPlan{
					{RelPath: "Ado/狂言/01 - うっせぇわ.mp3", Artist: "Ado", Album: "狂言", TrackTitle: "うっせぇわ", DiscNumber: 1, TrackNumber: 1},
					{RelPath: "Ado/狂言/02 - 踊.mp3", Artist: "Ado", Album: "狂言", TrackTitle: "踊", DiscNumber: 1, TrackNumber: 2},
				},
			},
			{
				Key:         "musicbrainz_release_group:rg-drift",
				Artist:      "Ado",
				Album:       "Completely Wrong Single",
				Year:        "2022",
				ReleaseKind: "single",
				ExternalIDs: map[string]string{"musicbrainz_release_group": "rg-drift"},
				Tracks: []MusicTrackPlan{
					{RelPath: "Ado/Completely Wrong Single/01 - Local Track.mp3", Artist: "Ado", Album: "Completely Wrong Single", TrackTitle: "Local Track", DiscNumber: 1, TrackNumber: 1},
				},
			},
			{
				Key:         "artist_album:ado|missing album|2024",
				Artist:      "Ado",
				Album:       "Missing Album",
				Year:        "2024",
				ReleaseKind: "single",
				Tracks: []MusicTrackPlan{
					{RelPath: "Ado/Missing Album/01 - Missing.mp3", Artist: "Ado", Album: "Missing Album", TrackTitle: "Missing", DiscNumber: 1, TrackNumber: 1},
				},
			},
		},
	}}
	search := []MusicSearchMatch{
		{Accepted: true, Key: "artist:ado", ProviderID: "heya:artist:mbid:ado-mbid", Artist: "Ado", Query: MusicSearchQuery{Artist: "Ado"}, ExternalIDs: map[string]string{"mbid": "ado-mbid"}},
		{Accepted: false, Key: "artist:bad", ProviderID: "heya:artist:mbid:bad", Artist: "Bad", Query: MusicSearchQuery{Artist: "Bad"}},
	}

	emit := &captureEmitter{}
	previews, err := FetchMusicMetadataPreviews(context.Background(), search, artists, provider, emit)
	if err != nil {
		t.Fatalf("fetch music metadata: %v", err)
	}
	if len(provider.calls) != 1 || provider.calls[0] != "heya:artist:mbid:ado-mbid" {
		t.Fatalf("provider calls: %#v", provider.calls)
	}
	if len(previews) != 1 {
		t.Fatalf("previews: got %d, want 1", len(previews))
	}
	preview := previews[0]
	if preview.MappedAlbums != 2 || preview.LocalAlbums != 3 {
		t.Fatalf("album mapping: got %d/%d, want 2/3: %#v", preview.MappedAlbums, preview.LocalAlbums, preview)
	}
	if preview.MappedTracks != 3 || preview.LocalTracks != 4 {
		t.Fatalf("track mapping: got %d/%d, want 3/4: %#v", preview.MappedTracks, preview.LocalTracks, preview)
	}
	if len(preview.Issues) == 0 {
		t.Fatalf("expected mapping issues")
	}
	if !strings.Contains(strings.Join(preview.Issues, "\n"), "remote_album_not_found") {
		t.Fatalf("missing remote album issue: %#v", preview.Issues)
	}
	if !strings.Contains(strings.Join(preview.Issues, "\n"), "album_title_mismatch") {
		t.Fatalf("missing title mismatch issue: %#v", preview.Issues)
	}

	var report bytes.Buffer
	WriteReport(&report, sqlc.Library{ID: 1, Name: "DevMusic", MediaType: sqlc.MediaTypeMusic}, Result{
		MusicArtists:  artists,
		MusicSearch:   search,
		MusicMetadata: previews,
	}, emit.events)
	for _, want := range []string{
		"Metadata fetched:       1/1 artists",
		"Discography mapped:     2/3 albums, 3/4 tracks",
		"Needs review: metadata mapping",
		"Metadata fetch preview",
		"album: 狂言 (2022) -> 狂言 (2022) reason=external_id:musicbrainz_release_group=mb_release_group",
		"album: Missing Album (2024) -> unmatched tracks=0/1",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("music fetch report missing %q:\n%s", want, report.String())
		}
	}
}

func TestFetchMusicMetadataPreviewsReranksArtistByDiscography(t *testing.T) {
	provider := &fakeMusicDetailProvider{details: map[string]*metadata.MediaDetail{
		"heya:artist:mbid:wrong-ado": {
			Title:          "ADO",
			ArtistName:     "ADO",
			ArtistSortName: "ADO",
			ExternalIDs:    map[string]string{"mbid": "wrong-ado"},
			Albums: []metadata.AlbumEntry{{
				Title: "The Ultra Bland Sessions",
				Year:  1982,
				Type:  "album",
				Tracks: []metadata.TrackDetail{
					{DiscNumber: 1, TrackNumber: 1, Title: "Not This"},
				},
			}},
		},
		"heya:artist:mbid:ado-jp": {
			Title:          "Ado",
			ArtistName:     "Ado",
			ArtistSortName: "Ado",
			ExternalIDs: map[string]string{
				"mbid":    "ado-jp",
				"apple":   "1492604670",
				"deezer":  "121146382",
				"discogs": "9278173",
			},
			ArtistTags:   []string{"j-pop"},
			ArtistImages: []metadata.ArtworkResult{{URL: "ado.webp", AssetType: "poster"}},
			Albums: []metadata.AlbumEntry{{
				Title:       "狂言",
				Type:        "album",
				Year:        2022,
				ExternalIDs: map[string]string{"mb_release_group": "rg-kyogen"},
				Tracks: []metadata.TrackDetail{
					{DiscNumber: 1, TrackNumber: 1, Title: "うっせぇわ"},
					{DiscNumber: 1, TrackNumber: 2, Title: "踊"},
				},
			}},
		},
	}}
	artists := []MusicArtistPlan{{
		Key:    "artist:ado",
		Artist: "Ado",
		Albums: []MusicAlbumPlan{{
			Key:         "musicbrainz_release_group:rg-kyogen",
			Artist:      "Ado",
			Album:       "狂言",
			Year:        "2022",
			ReleaseKind: "album",
			ExternalIDs: map[string]string{"musicbrainz_release_group": "rg-kyogen"},
			Tracks: []MusicTrackPlan{
				{RelPath: "Ado/狂言/01 - うっせぇわ.mp3", Artist: "Ado", Album: "狂言", TrackTitle: "うっせぇわ", DiscNumber: 1, TrackNumber: 1},
				{RelPath: "Ado/狂言/02 - 踊.mp3", Artist: "Ado", Album: "狂言", TrackTitle: "踊", DiscNumber: 1, TrackNumber: 2},
			},
		}},
	}}
	search := []MusicSearchMatch{{
		Accepted:   true,
		Key:        "artist:ado",
		ProviderID: "heya:artist:mbid:wrong-ado",
		Provider:   "heya",
		Artist:     "ADO",
		Query:      MusicSearchQuery{Artist: "Ado"},
		Confidence: 1,
		ExternalIDs: map[string]string{
			"mbid": "wrong-ado",
		},
		Candidates: []MusicSearchCandidate{
			{
				ProviderID:  "heya:artist:mbid:wrong-ado",
				Provider:    "heya",
				Artist:      "ADO",
				Confidence:  1,
				ExternalIDs: map[string]string{"mbid": "wrong-ado"},
			},
			{
				ProviderID: "heya:artist:mbid:ado-jp",
				Provider:   "heya",
				Artist:     "Ado",
				Confidence: 1,
				ExternalIDs: map[string]string{
					"mbid":    "ado-jp",
					"apple":   "1492604670",
					"deezer":  "121146382",
					"discogs": "9278173",
				},
			},
		},
	}}

	emit := &captureEmitter{}
	previews, err := FetchMusicMetadataPreviews(context.Background(), search, artists, provider, emit)
	if err != nil {
		t.Fatalf("fetch music metadata: %v", err)
	}
	if len(previews) != 1 {
		t.Fatalf("previews: got %d, want 1", len(previews))
	}
	preview := previews[0]
	if preview.ProviderID != "heya:artist:mbid:ado-jp" {
		t.Fatalf("provider after rerank: got %s, want ado-jp: %#v", preview.ProviderID, preview)
	}
	if preview.SearchProviderID != "heya:artist:mbid:wrong-ado" || preview.SelectionReason != "discography_reranked" {
		t.Fatalf("rerank metadata: search_provider=%q reason=%q", preview.SearchProviderID, preview.SelectionReason)
	}
	if preview.MappedAlbums != 1 || preview.LocalAlbums != 1 || preview.MappedTracks != 2 || preview.LocalTracks != 2 {
		t.Fatalf("mapping after rerank: albums=%d/%d tracks=%d/%d", preview.MappedAlbums, preview.LocalAlbums, preview.MappedTracks, preview.LocalTracks)
	}
	if len(preview.Issues) != 0 {
		t.Fatalf("clean rerank should not create mapping issues: %#v", preview.Issues)
	}
	if len(preview.CandidateEvaluations) != 2 {
		t.Fatalf("candidate evaluations: %#v", preview.CandidateEvaluations)
	}
	if !eventSeen(emit.events, "metadata.selection_replaced") {
		t.Fatalf("expected metadata.selection_replaced event")
	}

	var report bytes.Buffer
	WriteReport(&report, sqlc.Library{ID: 1, Name: "DevMusic", MediaType: sqlc.MediaTypeMusic}, Result{
		MusicArtists:  artists,
		MusicSearch:   search,
		MusicMetadata: previews,
	}, emit.events)
	for _, want := range []string{
		"selected_after_fetch=discography_reranked previous=heya:artist:mbid:wrong-ado",
		"candidates:",
		"Ado provider=heya:artist:mbid:ado-jp mapped_albums=1/1 mapped_tracks=2/2 selected",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("music rerank report missing %q:\n%s", want, report.String())
		}
	}
	if strings.Contains(report.String(), "Needs review: metadata mapping") {
		t.Fatalf("clean rerank should not be listed as metadata mapping review:\n%s", report.String())
	}
}

func TestFetchMusicMetadataPreviewsKeepsPrimaryOnEqualDiscography(t *testing.T) {
	provider := &fakeMusicDetailProvider{details: map[string]*metadata.MediaDetail{
		"heya:artist:mbid:aphex-primary": {
			Title:       "Aphex Twin",
			ArtistName:  "Aphex Twin",
			ExternalIDs: map[string]string{"mbid": "aphex-primary"},
			Albums: []metadata.AlbumEntry{{
				Title: "Selected Ambient Works 85-92",
				Year:  1992,
				Type:  "album",
				Tracks: []metadata.TrackDetail{
					{DiscNumber: 1, TrackNumber: 1, Title: "Xtal"},
					{DiscNumber: 1, TrackNumber: 2, Title: "Tha"},
				},
			}},
		},
		"heya:artist:apple:39883194": {
			Title:      "Aphex Twin",
			ArtistName: "Aphex Twin",
			ExternalIDs: map[string]string{
				"apple": "39883194",
			},
			Albums: []metadata.AlbumEntry{
				{
					Title: "Selected Ambient Works 85-92",
					Year:  1992,
					Type:  "album",
					Tracks: []metadata.TrackDetail{
						{DiscNumber: 1, TrackNumber: 1, Title: "Xtal"},
						{DiscNumber: 1, TrackNumber: 2, Title: "Tha"},
					},
				},
				{Title: "Extra Remote Album", Year: 1993, Type: "album"},
			},
		},
	}}
	artists := []MusicArtistPlan{{
		Key:    "artist:aphex twin",
		Artist: "Aphex Twin",
		Albums: []MusicAlbumPlan{{
			Key:         "artist_album:aphex twin|selected ambient works 85 92|1992",
			Artist:      "Aphex Twin",
			Album:       "Selected Ambient Works 85-92",
			Year:        "1992",
			ReleaseKind: "album",
			Tracks: []MusicTrackPlan{
				{RelPath: "Aphex Twin/SAW/01 - Xtal.mp3", Artist: "Aphex Twin", Album: "Selected Ambient Works 85-92", TrackTitle: "Xtal", DiscNumber: 1, TrackNumber: 1},
				{RelPath: "Aphex Twin/SAW/02 - Tha.mp3", Artist: "Aphex Twin", Album: "Selected Ambient Works 85-92", TrackTitle: "Tha", DiscNumber: 1, TrackNumber: 2},
			},
		}},
	}}
	search := []MusicSearchMatch{{
		Accepted:   true,
		Key:        "artist:aphex twin",
		ProviderID: "heya:artist:mbid:aphex-primary",
		Provider:   "heya",
		Artist:     "Aphex Twin",
		Query:      MusicSearchQuery{Artist: "Aphex Twin"},
		Confidence: 1,
		ExternalIDs: map[string]string{
			"mbid": "aphex-primary",
		},
		Candidates: []MusicSearchCandidate{
			{ProviderID: "heya:artist:mbid:aphex-primary", Provider: "heya", Artist: "Aphex Twin", Confidence: 1, ExternalIDs: map[string]string{"mbid": "aphex-primary"}},
			{ProviderID: "heya:artist:apple:39883194", Provider: "heya", Artist: "Aphex Twin", Confidence: 1, ExternalIDs: map[string]string{"apple": "39883194"}},
		},
	}}

	previews, err := FetchMusicMetadataPreviews(context.Background(), search, artists, provider, &captureEmitter{})
	if err != nil {
		t.Fatalf("fetch music metadata: %v", err)
	}
	if len(previews) != 1 {
		t.Fatalf("previews: got %d, want 1", len(previews))
	}
	preview := previews[0]
	if preview.ProviderID != "heya:artist:mbid:aphex-primary" {
		t.Fatalf("equal discography should keep primary: %#v", preview)
	}
	if preview.SearchProviderID != "" || preview.SelectionReason != "search_selected" {
		t.Fatalf("unexpected rerank metadata: search_provider=%q reason=%q", preview.SearchProviderID, preview.SelectionReason)
	}
	if preview.MappedAlbums != 1 || preview.MappedTracks != 2 {
		t.Fatalf("mapping: albums=%d tracks=%d", preview.MappedAlbums, preview.MappedTracks)
	}
}

func TestFetchMusicMetadataPreviewsDoesNotRerankToLowConfidenceCandidate(t *testing.T) {
	provider := &fakeMusicDetailProvider{details: map[string]*metadata.MediaDetail{
		"heya:artist:deezer:joint": {
			Title:      "Lady Gaga & Bradley Cooper",
			ArtistName: "Lady Gaga & Bradley Cooper",
			ExternalIDs: map[string]string{
				"deezer": "joint",
			},
			Albums: []metadata.AlbumEntry{{
				Title: "Unrelated Remote Album",
				Year:  2017,
				Type:  "album",
			}},
		},
		"heya:artist:mbid:bradley": {
			Title:      "Bradley Cooper",
			ArtistName: "Bradley Cooper",
			ExternalIDs: map[string]string{
				"mbid": "bradley",
			},
			Albums: []metadata.AlbumEntry{{
				Title: "A Star Is Born Soundtrack",
				Year:  2018,
				Type:  "soundtrack",
				Tracks: []metadata.TrackDetail{
					{DiscNumber: 1, TrackNumber: 1, Title: "Shallow"},
					{DiscNumber: 1, TrackNumber: 2, Title: "Always Remember Us This Way"},
				},
			}},
		},
	}}
	artists := []MusicArtistPlan{{
		Key:    "artist:lady gaga bradley cooper",
		Artist: "Lady Gaga & Bradley Cooper",
		Albums: []MusicAlbumPlan{{
			Key:         "artist_album:lady gaga bradley cooper|a star is born|2018",
			Artist:      "Lady Gaga & Bradley Cooper",
			Album:       "A Star Is Born",
			Year:        "2018",
			ReleaseKind: "soundtrack",
			Tracks: []MusicTrackPlan{
				{RelPath: "A Star Is Born/01 - Shallow.flac", Artist: "Lady Gaga & Bradley Cooper", Album: "A Star Is Born", TrackTitle: "Shallow", DiscNumber: 1, TrackNumber: 1},
				{RelPath: "A Star Is Born/02 - Always Remember Us This Way.flac", Artist: "Lady Gaga & Bradley Cooper", Album: "A Star Is Born", TrackTitle: "Always Remember Us This Way", DiscNumber: 1, TrackNumber: 2},
			},
		}},
	}}
	search := []MusicSearchMatch{{
		Accepted:   true,
		Key:        "artist:lady gaga bradley cooper",
		ProviderID: "heya:artist:deezer:joint",
		Provider:   "heya",
		Artist:     "Lady Gaga & Bradley Cooper",
		Query:      MusicSearchQuery{Artist: "Lady Gaga & Bradley Cooper"},
		Confidence: 1,
		ExternalIDs: map[string]string{
			"deezer": "joint",
		},
		Candidates: []MusicSearchCandidate{
			{ProviderID: "heya:artist:deezer:joint", Provider: "heya", Artist: "Lady Gaga & Bradley Cooper", Confidence: 1, ExternalIDs: map[string]string{"deezer": "joint"}},
			{ProviderID: "heya:artist:mbid:bradley", Provider: "heya", Artist: "Bradley Cooper", Confidence: 0.80, ExternalIDs: map[string]string{"mbid": "bradley"}},
		},
	}}

	previews, err := FetchMusicMetadataPreviews(context.Background(), search, artists, provider, &captureEmitter{})
	if err != nil {
		t.Fatalf("fetch music metadata: %v", err)
	}
	if len(previews) != 1 {
		t.Fatalf("previews: got %d, want 1", len(previews))
	}
	preview := previews[0]
	if preview.ProviderID != "heya:artist:deezer:joint" {
		t.Fatalf("low-confidence related artist should not replace search selection: %#v", preview)
	}
	if preview.SearchProviderID != "" || preview.SelectionReason != "search_selected" {
		t.Fatalf("unexpected rerank metadata: search_provider=%q reason=%q", preview.SearchProviderID, preview.SelectionReason)
	}
	if preview.MappedAlbums != 0 || preview.MappedTracks != 0 {
		t.Fatalf("primary should remain unmapped: albums=%d tracks=%d", preview.MappedAlbums, preview.MappedTracks)
	}
	if len(provider.calls) != 1 || provider.calls[0] != "heya:artist:deezer:joint" {
		t.Fatalf("low-confidence rerank candidate should not be fetched: %#v", provider.calls)
	}
}

func TestFetchMusicMetadataPreviewsKeepsPrimaryErrorWhenAlternativesDoNotMap(t *testing.T) {
	provider := &fakeMusicDetailProvider{details: map[string]*metadata.MediaDetail{
		"heya:artist:mbid:stardust": {
			Title:      "Stardust",
			ArtistName: "Stardust",
			ExternalIDs: map[string]string{
				"mbid": "stardust",
			},
			Albums: []metadata.AlbumEntry{{
				Title: "Music Sounds Better With You",
				Year:  1998,
				Type:  "single",
			}},
		},
	}}
	artists := []MusicArtistPlan{{
		Key:    "artist:daft punk",
		Artist: "Daft Punk",
		Albums: []MusicAlbumPlan{{
			Key:         "artist_album:daft punk|homework|1997",
			Artist:      "Daft Punk",
			Album:       "Homework",
			Year:        "1997",
			ReleaseKind: "album",
			Tracks: []MusicTrackPlan{
				{RelPath: "Daft Punk/Homework/01 - Daftendirekt.mp3", Artist: "Daft Punk", Album: "Homework", TrackTitle: "Daftendirekt", DiscNumber: 1, TrackNumber: 1},
			},
		}},
	}}
	search := []MusicSearchMatch{{
		Accepted:   true,
		Key:        "artist:daft punk",
		ProviderID: "heya:artist:mbid:daft-punk",
		Provider:   "heya",
		Artist:     "Daft Punk",
		Query:      MusicSearchQuery{Artist: "Daft Punk"},
		Confidence: 1,
		ExternalIDs: map[string]string{
			"mbid": "daft-punk",
		},
		Candidates: []MusicSearchCandidate{
			{ProviderID: "heya:artist:mbid:daft-punk", Provider: "heya", Artist: "Daft Punk", Confidence: 1, ExternalIDs: map[string]string{"mbid": "daft-punk"}},
			{ProviderID: "heya:artist:mbid:stardust", Provider: "heya", Artist: "Stardust", Confidence: 0.8, ExternalIDs: map[string]string{"mbid": "stardust"}},
		},
	}}

	previews, err := FetchMusicMetadataPreviews(context.Background(), search, artists, provider, &captureEmitter{})
	if err != nil {
		t.Fatalf("fetch music metadata: %v", err)
	}
	if len(previews) != 1 {
		t.Fatalf("previews: got %d, want 1", len(previews))
	}
	preview := previews[0]
	if preview.ProviderID != "heya:artist:mbid:daft-punk" || preview.Error == "" {
		t.Fatalf("zero-coverage alternative should not replace primary error: %#v", preview)
	}
	if preview.SearchProviderID != "" || preview.SelectionReason != "search_selected" {
		t.Fatalf("unexpected rerank metadata: search_provider=%q reason=%q", preview.SearchProviderID, preview.SelectionReason)
	}
	if len(provider.calls) != 1 || provider.calls[0] != "heya:artist:mbid:daft-punk" {
		t.Fatalf("primary fetch error should not trigger alternate artist fetches: %#v", provider.calls)
	}
}

func TestMapMusicTrackFetchPrefersTitleWhenDiscTrackTitleConflicts(t *testing.T) {
	mapping := mapMusicTrackFetch(
		MusicTrackPlan{RelPath: "Ado/狂言/01 - うっせぇわ.mp3", TrackTitle: "うっせぇわ", DiscNumber: 1, TrackNumber: 1},
		[]metadata.TrackDetail{
			{DiscNumber: 1, TrackNumber: 1, Title: "レディメイド"},
			{DiscNumber: 1, TrackNumber: 2, Title: "踊"},
			{DiscNumber: 1, TrackNumber: 11, Title: "うっせぇわ"},
		},
	)

	if !mapping.Matched || mapping.RemoteTrack != 11 || mapping.Reason != "title" {
		t.Fatalf("track mapping should use title instead of mismatched disc/track: %#v", mapping)
	}
	if mapping.Issue != "" {
		t.Fatalf("unexpected issue: %#v", mapping)
	}
}

func TestMapMusicTrackFetchSuppressesWeakLocalTitleMismatch(t *testing.T) {
	for _, title := range []string{"Track 1", "brown (flac)", "Bonus 1"} {
		mapping := mapMusicTrackFetch(
			MusicTrackPlan{RelPath: "Fixture/0101 - " + title + ".flac", TrackTitle: title, DiscNumber: 1, TrackNumber: 1},
			[]metadata.TrackDetail{
				{DiscNumber: 1, TrackNumber: 1, Title: "Actual Remote Title"},
			},
		)
		if !mapping.Matched || mapping.Reason != "disc_track" {
			t.Fatalf("%q should map by disc/track: %#v", title, mapping)
		}
		if mapping.Issue != "" {
			t.Fatalf("%q should not report title mismatch: %#v", title, mapping)
		}
	}
}
