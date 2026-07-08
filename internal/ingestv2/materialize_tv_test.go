package ingestv2

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

type fakeTVMaterializeStore struct {
	itemsByExternal  map[string]sqlc.MediaItemCard
	itemsByIdentity  map[string]sqlc.MediaItemCard
	itemsByID        map[int64]sqlc.MediaItemCard
	seriesByMediaID  map[int64]sqlc.TvSeries
	seasonsBySeries  map[int64][]sqlc.TvSeason
	episodesBySeries map[int64][]sqlc.TvEpisode
	files            map[string]sqlc.LibraryFile
}

func newFakeTVMaterializeStore() *fakeTVMaterializeStore {
	return &fakeTVMaterializeStore{
		itemsByExternal:  map[string]sqlc.MediaItemCard{},
		itemsByIdentity:  map[string]sqlc.MediaItemCard{},
		itemsByID:        map[int64]sqlc.MediaItemCard{},
		seriesByMediaID:  map[int64]sqlc.TvSeries{},
		seasonsBySeries:  map[int64][]sqlc.TvSeason{},
		episodesBySeries: map[int64][]sqlc.TvEpisode{},
		files:            map[string]sqlc.LibraryFile{},
	}
}

func (f *fakeTVMaterializeStore) FindMediaItemByExternalIDs(_ context.Context, _ int64, ids map[string]string) (sqlc.MediaItemCard, bool, error) {
	for _, key := range []string{"tmdb", "imdb", "tvdb", "anidb", "mal"} {
		if value := ids[key]; value != "" {
			item, ok := f.itemsByExternal[key+":"+value]
			if ok {
				return item, true, nil
			}
		}
	}
	return sqlc.MediaItemCard{}, false, nil
}

func (f *fakeTVMaterializeStore) FindMediaItemByIdentity(_ context.Context, _ int64, mediaType sqlc.MediaType, title, year string) (sqlc.MediaItemCard, bool, error) {
	item, ok := f.itemsByIdentity[string(mediaType)+"\x00"+title+"\x00"+year]
	return item, ok, nil
}

func (f *fakeTVMaterializeStore) GetMediaItemByID(_ context.Context, mediaItemID int64) (sqlc.MediaItemCard, bool, error) {
	item, ok := f.itemsByID[mediaItemID]
	return item, ok, nil
}

func (f *fakeTVMaterializeStore) GetTVSeriesByMediaItemID(_ context.Context, mediaItemID int64) (sqlc.TvSeries, bool, error) {
	series, ok := f.seriesByMediaID[mediaItemID]
	return series, ok, nil
}

func (f *fakeTVMaterializeStore) ListTVSeasonsBySeries(_ context.Context, seriesID int64) ([]sqlc.TvSeason, error) {
	return f.seasonsBySeries[seriesID], nil
}

func (f *fakeTVMaterializeStore) ListTVEpisodesBySeries(_ context.Context, seriesID int64) ([]sqlc.TvEpisode, error) {
	return f.episodesBySeries[seriesID], nil
}

func (f *fakeTVMaterializeStore) GetLibraryFileByPath(_ context.Context, _ int64, path string) (sqlc.LibraryFile, bool, error) {
	file, ok := f.files[path]
	return file, ok, nil
}

func TestPlanTVMaterializationCreatesUniqueTargetsAndBlocksRejected(t *testing.T) {
	detail := &metadata.MediaDetail{
		Title:       "The Bear",
		Year:        "2022",
		Description: "Kitchen pressure.",
		ExternalIDs: map[string]string{"tmdb": "136315"},
		PosterURL:   "poster.webp",
		BackdropURL: "backdrop.webp",
		Networks:    []metadata.NetworkDetail{{Name: "Hulu"}},
		Artwork:     []metadata.ArtworkResult{{URL: "logo.webp", AssetType: "logo"}},
		Cast:        []metadata.CastMember{{Name: "Jeremy Allen White"}},
		Crew:        []metadata.CrewMember{{Name: "Christopher Storer", Job: "Creator"}},
		Seasons: []metadata.SeasonDetail{
			{Number: 1, Title: "Season 1", Episodes: []metadata.EpisodeDetail{{Number: 1, Title: "System"}}},
			{Number: 3, Title: "Season 3", Episodes: []metadata.EpisodeDetail{{Number: 1, Title: "Tomorrow"}}},
		},
	}
	result := Result{
		Inventory: Inventory{Roots: []InventoryRoot{{Files: []InventoryFile{
			{RelPath: "The Bear (2022)/Season 01/The.Bear.S01E01.mkv", Path: "/tv/The Bear (2022)/Season 01/The.Bear.S01E01.mkv"},
			{RelPath: "Loose/The.Bear.S03E01.mkv", Path: "/tv/Loose/The.Bear.S03E01.mkv"},
		}}}},
		TVMatches: []TVMatch{
			{
				Key:      "tmdb:136315",
				Title:    "The Bear",
				Year:     "2022",
				Files:    []string{"The Bear (2022)/Season 01/The.Bear.S01E01.mkv"},
				Episodes: []TVEpisodeRef{{Season: 1, Episode: 1}},
			},
			{
				Key:      "title:bear",
				Title:    "The Bear",
				Files:    []string{"Loose/The.Bear.S03E01.mkv"},
				Episodes: []TVEpisodeRef{{Season: 3, Episode: 1}},
			},
		},
		TVSearch: []TVSearchMatch{
			{Accepted: true, Key: "tmdb:136315", ProviderID: "heya:tv:tmdb:136315", Title: "The Bear", Year: "2022", ExternalIDs: map[string]string{"tmdb": "136315"}},
			{Accepted: true, Key: "title:bear", ProviderID: "heya:tv:tmdb:136315", Title: "The Bear", Year: "2022", ExternalIDs: map[string]string{"tmdb": "136315"}},
			{Accepted: false, Key: "title:poker face", Query: TVSearchQuery{Title: "Poker Face"}, Reason: "ambiguous_or_low_confidence"},
		},
		TVMetadata: []TVFetchPreview{{
			Key:             "title:bear,tmdb:136315",
			Keys:            []string{"title:bear", "tmdb:136315"},
			ProviderID:      "heya:tv:tmdb:136315",
			Title:           "The Bear",
			Year:            "2022",
			ExternalIDs:     map[string]string{"tmdb": "136315"},
			Networks:        []string{"Hulu"},
			Seasons:         2,
			RemoteEpisodes:  2,
			PlannedEpisodes: 2,
			MappedEpisodes:  2,
			PlannedFiles:    2,
			Artwork:         1,
			Cast:            1,
			Crew:            1,
			WouldApply:      []string{"episodes", "external_ids", "seasons", "title", "year"},
			Detail:          detail,
		}},
	}

	emit := &captureEmitter{}
	previews, err := PlanTVMaterialization(context.Background(), sqlc.Library{ID: 2, Name: "DevTV", MediaType: sqlc.MediaTypeTv}, result, newFakeTVMaterializeStore(), emit)
	if err != nil {
		t.Fatalf("plan TV materialization: %v", err)
	}

	blocked, _, create, update := splitTVMaterializePreviews(previews)
	if len(create) != 1 || len(update) != 0 || len(blocked) != 1 {
		t.Fatalf("materialize counts: create=%d update=%d blocked=%d previews=%#v", len(create), len(update), len(blocked), previews)
	}
	created := create[0]
	if created.ProviderID != "heya:tv:tmdb:136315" || created.SeasonsCreate != 2 || created.EpisodesCreate != 2 {
		t.Fatalf("created target: %#v", created)
	}
	if len(created.FileActions) != 2 || !hasMovieFileAction(created.FileActions, "create_library_file_and_attach") {
		t.Fatalf("file actions: %#v", created.FileActions)
	}
	if blocked[0].Reason != "search_rejected" {
		t.Fatalf("blocked reason: %#v", blocked[0])
	}
	if !eventSeen(emit.events, "materialize.preview") || !eventSeen(emit.events, "materialize.blocked") {
		t.Fatalf("expected materialize preview and blocked events: %#v", emit.events)
	}

	var report bytes.Buffer
	result.TVMaterialize = previews
	WriteReport(&report, sqlc.Library{ID: 2, Name: "DevTV", MediaType: sqlc.MediaTypeTv}, result, emit.events)
	for _, want := range []string{
		"Materialize:           1 create, 0 update, 0 repair, 1 blocked",
		"blocked Poker Face reason=search_rejected",
		"create The Bear (2022) provider=heya:tv:tmdb:136315",
		"seasons=create=2",
		"episodes=create=2",
		"files=create_library_file_and_attach=2",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("TV materialize report missing %q:\n%s", want, report.String())
		}
	}
	if strings.Contains(report.String(), "Metadata fetch preview") {
		t.Fatalf("materialize report should not include fetch preview:\n%s", report.String())
	}
}
