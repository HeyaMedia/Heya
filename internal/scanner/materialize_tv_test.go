package scanner

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
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
				Key:      "title:the bear",
				Title:    "The Bear",
				Files:    []string{"Loose/The.Bear.S03E01.mkv"},
				Episodes: []TVEpisodeRef{{Season: 3, Episode: 1}},
			},
		},
		TVSearch: []TVSearchMatch{
			{Accepted: true, Key: "tmdb:136315", ProviderID: "heya:tv:tmdb:136315", Title: "The Bear", Year: "2022", ExternalIDs: map[string]string{"tmdb": "136315"}},
			{Accepted: true, Key: "title:the bear", ProviderID: "heya:tv:tmdb:136315", Title: "The Bear", Year: "2022", ExternalIDs: map[string]string{"tmdb": "136315"}},
			{Accepted: false, Key: "title:poker face", Query: TVSearchQuery{Title: "Poker Face"}, Reason: "ambiguous_or_low_confidence"},
		},
		TVMetadata: []TVFetchPreview{{
			Key:             "title:the bear,tmdb:136315",
			Keys:            []string{"title:the bear", "tmdb:136315"},
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

func TestTVEpisodeLinkTargetsPreservesAbsoluteNumbers(t *testing.T) {
	index := tvEpisodeLinkIndex{
		byNumber: map[tvEpisodeNumberKey]tvEpisodeLinkTarget{
			{season: 1, episode: 2}: {episodeID: 2002, seasonNumber: 1, episodeNumber: 2, absoluteNumber: 2},
		},
		byAbsolute: map[int32]tvEpisodeLinkTarget{
			2:  {episodeID: 2002, seasonNumber: 1, episodeNumber: 2, absoluteNumber: 2},
			24: {episodeID: 9024, seasonNumber: 2, episodeNumber: 13, absoluteNumber: 24},
		},
	}

	paired := tvEpisodeLinkTargetsForPlan(TVPlan{
		Season:           1,
		Episodes:         []int{2},
		AbsoluteEpisodes: []int{2},
	}, index)
	if len(paired) != 1 {
		t.Fatalf("paired targets: got %d, want 1", len(paired))
	}
	if paired[0].episodeID != 2002 || paired[0].seasonNumber != 1 || paired[0].episodeNumber != 2 || paired[0].absoluteNumber != 2 {
		t.Fatalf("paired target: %#v", paired[0])
	}

	fallback := tvEpisodeLinkTargetsForPlan(TVPlan{
		Season:           1,
		Episodes:         []int{24},
		AbsoluteEpisodes: []int{24},
	}, index)
	if len(fallback) != 1 {
		t.Fatalf("fallback targets: got %d, want 1", len(fallback))
	}
	if fallback[0].episodeID != 9024 || fallback[0].seasonNumber != 2 || fallback[0].episodeNumber != 13 || fallback[0].absoluteNumber != 24 {
		t.Fatalf("fallback target: %#v", fallback[0])
	}

	absoluteOnly := tvEpisodeLinkTargetsForPlan(TVPlan{
		AbsoluteEpisodes: []int{24},
	}, index)
	if len(absoluteOnly) != 1 {
		t.Fatalf("absolute targets: got %d, want 1", len(absoluteOnly))
	}
	if absoluteOnly[0].episodeID != 9024 || absoluteOnly[0].seasonNumber != 2 || absoluteOnly[0].episodeNumber != 13 || absoluteOnly[0].absoluteNumber != 24 {
		t.Fatalf("absolute target: %#v", absoluteOnly[0])
	}
}

func TestTVEpisodeLinkTargetsMapFlattened86AliasToCanonicalSeason(t *testing.T) {
	index := tvEpisodeLinkIndex{
		byNumber:   map[tvEpisodeNumberKey]tvEpisodeLinkTarget{},
		byAbsolute: map[int32]tvEpisodeLinkTarget{},
	}
	for season := 1; season <= 2; season++ {
		for episode := 1; episode <= map[int]int{1: 11, 2: 12}[season]; episode++ {
			absolute := episode
			if season == 2 {
				absolute += 11
			}
			target := tvEpisodeLinkTarget{
				episodeID:      int64(absolute),
				seasonNumber:   int32(season),
				episodeNumber:  int32(episode),
				absoluteNumber: int32(absolute),
			}
			index.byNumber[tvEpisodeNumberKey{season: int32(season), episode: int32(episode)}] = target
			index.byAbsolute[int32(absolute)] = target
		}
	}
	addTVEpisodeLinkAliases(&index, eightySixMetadataDetail())

	targets := tvEpisodeLinkTargetsForPlan(TVPlan{Season: 1, Episodes: []int{12, 23}}, index)
	if len(targets) != 2 {
		t.Fatalf("targets: got %d, want 2", len(targets))
	}
	if targets[0].episodeID != 12 || targets[0].seasonNumber != 2 || targets[0].episodeNumber != 1 || targets[0].absoluteNumber != 12 {
		t.Fatalf("S01E12 target: %#v", targets[0])
	}
	if targets[1].episodeID != 23 || targets[1].seasonNumber != 2 || targets[1].episodeNumber != 12 || targets[1].absoluteNumber != 23 {
		t.Fatalf("S01E23 target: %#v", targets[1])
	}
	seasons, episodes := tvCanonicalEpisodeArrays(targets)
	if len(seasons) != 1 || seasons[0] != 2 || len(episodes) != 2 || episodes[0] != 1 || episodes[1] != 12 {
		t.Fatalf("canonical parse arrays: seasons=%#v episodes=%#v", seasons, episodes)
	}
}

func TestStaleCanonicalTVRowsDetectOld86Layout(t *testing.T) {
	detail := eightySixMetadataDetail()
	currentS2E1 := uuid.MustParse(detail.Seasons[1].Episodes[0].CanonicalID)
	staleEpisode := uuid.MustParse("30000000-0000-4000-8000-000000000012")
	episodeRows := []sqlc.ListCanonicalTVEpisodeRowsBySeriesRow{
		{ID: 11, SeasonNumber: 1, EpisodeNumber: 11, EntityID: uuid.MustParse(detail.Seasons[0].Episodes[10].CanonicalID)},
		{ID: 12, SeasonNumber: 1, EpisodeNumber: 12, EntityID: staleEpisode},
		{ID: 13, SeasonNumber: 1, EpisodeNumber: 12, EntityID: currentS2E1},
		{ID: 14, SeasonNumber: 2, EpisodeNumber: 1, EntityID: currentS2E1},
	}
	staleEpisodes := staleCanonicalTVEpisodeIDs(detail, episodeRows)
	if len(staleEpisodes) != 2 || staleEpisodes[0] != 12 || staleEpisodes[1] != 13 {
		t.Fatalf("stale episode rows: %#v", staleEpisodes)
	}

	seasonRows := []sqlc.ListCanonicalTVSeasonRowsBySeriesRow{
		{ID: 21, SeasonNumber: 1, EntityID: uuid.MustParse(detail.Seasons[0].CanonicalID)},
		{ID: 22, SeasonNumber: 1, EntityID: uuid.MustParse(detail.Seasons[1].CanonicalID)},
		{ID: 23, SeasonNumber: 2, EntityID: uuid.MustParse(detail.Seasons[1].CanonicalID)},
		{ID: 24, SeasonNumber: 3, EntityID: uuid.MustParse("30000000-0000-4000-8000-000000000003")},
	}
	staleSeasons := staleCanonicalTVSeasonIDs(detail, seasonRows)
	if len(staleSeasons) != 2 || staleSeasons[0] != 22 || staleSeasons[1] != 24 {
		t.Fatalf("stale season rows: %#v", staleSeasons)
	}
}
