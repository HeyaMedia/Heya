package scanner

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediatype"
)

type TVMaterializeStore interface {
	FindMediaItemByExternalIDs(context.Context, int64, map[string]string) (sqlc.MediaItemCard, bool, error)
	FindMediaItemByIdentity(context.Context, int64, sqlc.MediaType, string, string) (sqlc.MediaItemCard, bool, error)
	GetMediaItemByID(context.Context, int64) (sqlc.MediaItemCard, bool, error)
	GetTVSeriesByMediaItemID(context.Context, int64) (sqlc.TvSeries, bool, error)
	ListTVSeasonsBySeries(context.Context, int64) ([]sqlc.TvSeason, error)
	ListTVEpisodesBySeries(context.Context, int64) ([]sqlc.TvEpisode, error)
	GetLibraryFileByPath(context.Context, int64, string) (sqlc.LibraryFile, bool, error)
}

type SQLTVMaterializeStore struct {
	q *sqlc.Queries
}

func NewSQLTVMaterializeStore(db sqlc.DBTX) *SQLTVMaterializeStore {
	return &SQLTVMaterializeStore{q: sqlc.New(db)}
}

func (s *SQLTVMaterializeStore) FindMediaItemByExternalIDs(ctx context.Context, libraryID int64, ids map[string]string) (sqlc.MediaItemCard, bool, error) {
	for _, key := range []string{"tmdb", "imdb", "tvdb", "anidb", "mal"} {
		if value := ids[key]; value != "" {
			item, err := s.q.GetMediaItemByNormalizedExternalID(ctx, sqlc.GetMediaItemByNormalizedExternalIDParams{
				LibraryID:  libraryID,
				Provider:   key,
				ExternalID: value,
			})
			if err == nil {
				return item, true, nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return sqlc.MediaItemCard{}, false, err
			}
		}
	}
	if len(ids) == 0 {
		return sqlc.MediaItemCard{}, false, nil
	}
	item, err := s.q.GetMediaItemByExternalID(ctx, sqlc.GetMediaItemByExternalIDParams{
		LibraryID: libraryID,
		ExtFilter: mustJSONBytes(ids),
	})
	if err == nil {
		return item, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.MediaItemCard{}, false, nil
	}
	return sqlc.MediaItemCard{}, false, err
}

func (s *SQLTVMaterializeStore) FindMediaItemByIdentity(ctx context.Context, libraryID int64, mediaType sqlc.MediaType, title, year string) (sqlc.MediaItemCard, bool, error) {
	item, err := s.q.FindMediaItemByIdentity(ctx, sqlc.FindMediaItemByIdentityParams{
		LibraryID:      libraryID,
		MediaType:      mediaType,
		Title:          title,
		Year:           year,
		IncludeMatched: true,
	})
	if err == nil {
		return item, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.MediaItemCard{}, false, nil
	}
	return sqlc.MediaItemCard{}, false, err
}

func (s *SQLTVMaterializeStore) GetMediaItemByID(ctx context.Context, mediaItemID int64) (sqlc.MediaItemCard, bool, error) {
	item, err := s.q.GetMediaItemByID(ctx, mediaItemID)
	if err == nil {
		return item, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.MediaItemCard{}, false, nil
	}
	return sqlc.MediaItemCard{}, false, err
}

func (s *SQLTVMaterializeStore) GetTVSeriesByMediaItemID(ctx context.Context, mediaItemID int64) (sqlc.TvSeries, bool, error) {
	series, err := s.q.GetTVSeriesByMediaItemID(ctx, mediaItemID)
	if err == nil {
		return series, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.TvSeries{}, false, nil
	}
	return sqlc.TvSeries{}, false, err
}

func (s *SQLTVMaterializeStore) ListTVSeasonsBySeries(ctx context.Context, seriesID int64) ([]sqlc.TvSeason, error) {
	return s.q.ListTVSeasonsBySeries(ctx, seriesID)
}

func (s *SQLTVMaterializeStore) ListTVEpisodesBySeries(ctx context.Context, seriesID int64) ([]sqlc.TvEpisode, error) {
	return s.q.ListTVEpisodesBySeries(ctx, seriesID)
}

func (s *SQLTVMaterializeStore) GetLibraryFileByPath(ctx context.Context, libraryID int64, path string) (sqlc.LibraryFile, bool, error) {
	file, err := s.q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{LibraryID: libraryID, Path: path})
	if err == nil {
		return file, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.LibraryFile{}, false, nil
	}
	return sqlc.LibraryFile{}, false, err
}

type TVMaterializePreview struct {
	Key              string                       `json:"key"`
	Keys             []string                     `json:"keys,omitempty"`
	Action           string                       `json:"action"`
	Reason           string                       `json:"reason,omitempty"`
	Title            string                       `json:"title"`
	Year             string                       `json:"year,omitempty"`
	ProviderID       string                       `json:"provider_id,omitempty"`
	MediaItemID      int64                        `json:"media_item_id,omitempty"`
	TVSeriesID       int64                        `json:"tv_series_id,omitempty"`
	MediaItemAction  string                       `json:"media_item_action,omitempty"`
	TVSeriesAction   string                       `json:"tv_series_action,omitempty"`
	FileActions      []MovieMaterializeFileAction `json:"file_actions,omitempty"`
	ExternalIDs      map[string]string            `json:"external_ids,omitempty"`
	MetadataFields   []string                     `json:"metadata_fields,omitempty"`
	LocalAssets      int                          `json:"local_assets,omitempty"`
	RemoteArtwork    int                          `json:"remote_artwork,omitempty"`
	Cast             int                          `json:"cast,omitempty"`
	Crew             int                          `json:"crew,omitempty"`
	Networks         int                          `json:"networks,omitempty"`
	RemoteSeasons    int                          `json:"remote_seasons,omitempty"`
	RemoteEpisodes   int                          `json:"remote_episodes,omitempty"`
	SeasonsCreate    int                          `json:"seasons_create,omitempty"`
	SeasonsExisting  int                          `json:"seasons_existing,omitempty"`
	EpisodesCreate   int                          `json:"episodes_create,omitempty"`
	EpisodesExisting int                          `json:"episodes_existing,omitempty"`
	PlannedEpisodes  int                          `json:"planned_episodes,omitempty"`
	MappedEpisodes   int                          `json:"mapped_episodes,omitempty"`
	MissingEpisodes  []TVEpisodeRef               `json:"missing_episodes,omitempty"`
	Issues           []string                     `json:"issues,omitempty"`
}

func PlanTVMaterialization(ctx context.Context, lib sqlc.Library, result Result, store TVMaterializeStore, emit Emitter) ([]TVMaterializePreview, error) {
	domain := tvLikeDomainForMediaType(lib.MediaType)
	if store == nil {
		return nil, fmt.Errorf("%s materialize store is required", domain)
	}

	matches := map[string]TVMatch{}
	for _, match := range result.TVMatches {
		matches[match.Key] = match
	}
	metadataByKey := map[string]TVFetchPreview{}
	for _, preview := range result.TVMetadata {
		keys := preview.Keys
		if len(keys) == 0 && preview.Key != "" {
			keys = strings.Split(preview.Key, ",")
		}
		for _, key := range keys {
			metadataByKey[key] = preview
		}
	}
	searchByKey := map[string]TVSearchMatch{}
	for _, search := range result.TVSearch {
		searchByKey[search.Key] = search
	}
	filesByRel := inventoryFilesByRel(result.Inventory)

	handled := map[string]bool{}
	previews := make([]TVMaterializePreview, 0, len(result.TVSearch))
	for _, search := range result.TVSearch {
		if err := ctx.Err(); err != nil {
			return previews, err
		}
		if !search.Accepted {
			match := matches[search.Key]
			preview := TVMaterializePreview{
				Key:        search.Key,
				Keys:       []string{search.Key},
				Action:     "blocked",
				Reason:     "search_rejected",
				ProviderID: search.ProviderID,
				Title:      firstNonEmpty(search.Title, match.Title, search.Query.Title),
				Year:       firstNonEmpty(search.Year, match.Year, search.Query.Year),
				Issues:     []string{search.Reason},
			}
			previews = append(previews, preview)
			emitTVMaterializePreview(preview, domain, emit)
			continue
		}
		if handled[search.Key] {
			continue
		}

		meta, ok := metadataByKey[search.Key]
		if !ok {
			preview := TVMaterializePreview{
				Key:        search.Key,
				Keys:       []string{search.Key},
				Action:     "blocked",
				Reason:     "metadata_not_fetched",
				ProviderID: search.ProviderID,
				Title:      firstNonEmpty(search.Title, search.Query.Title),
				Year:       firstNonEmpty(search.Year, search.Query.Year),
			}
			previews = append(previews, preview)
			emitTVMaterializePreview(preview, domain, emit)
			continue
		}
		keys := meta.Keys
		if len(keys) == 0 {
			keys = []string{search.Key}
		}
		for _, key := range keys {
			handled[key] = true
		}

		preview, err := planTVMaterializeTarget(ctx, lib, meta, keys, matches, searchByKey, filesByRel, store)
		if err != nil {
			return previews, err
		}
		previews = append(previews, preview)
		emitTVMaterializePreview(preview, domain, emit)
	}

	sort.Slice(previews, func(i, j int) bool {
		if previews[i].Action == previews[j].Action {
			if previews[i].Title == previews[j].Title {
				return previews[i].Year < previews[j].Year
			}
			return previews[i].Title < previews[j].Title
		}
		return materializeActionRank(previews[i].Action) < materializeActionRank(previews[j].Action)
	})
	emit.Emit(Event{Event: "materialize.preview_summary", Kind: domain, Data: tvMaterializeSummary(previews)})
	return previews, nil
}

func tvLikeDomainForMediaType(mediaType sqlc.MediaType) string {
	if mediaType == sqlc.MediaTypeAnime {
		return "anime"
	}
	return "tv"
}

func planTVMaterializeTarget(ctx context.Context, lib sqlc.Library, meta TVFetchPreview, keys []string, matches map[string]TVMatch, searchByKey map[string]TVSearchMatch, filesByRel map[string][]InventoryFile, store TVMaterializeStore) (TVMaterializePreview, error) {
	localMatches := make([]TVMatch, 0, len(keys))
	externalIDs := mergeStringMaps(meta.ExternalIDs)
	for _, key := range keys {
		localMatches = append(localMatches, matches[key])
		externalIDs = mergeStringMaps(externalIDs, matches[key].ExternalIDs, searchByKey[key].ExternalIDs)
	}
	localMatch := combineTVFetchMatches(localMatches)
	preview := TVMaterializePreview{
		Key:             strings.Join(keys, ","),
		Keys:            append([]string{}, keys...),
		Action:          "blocked",
		ProviderID:      meta.ProviderID,
		Title:           firstNonEmpty(meta.Title, localMatch.Title, searchByKey[keys[0]].Title, searchByKey[keys[0]].Query.Title),
		Year:            firstNonEmpty(meta.Year, localMatch.Year, searchByKey[keys[0]].Year, searchByKey[keys[0]].Query.Year),
		ExternalIDs:     externalIDs,
		MetadataFields:  append([]string{}, meta.WouldApply...),
		RemoteArtwork:   meta.Artwork,
		Cast:            meta.Cast,
		Crew:            meta.Crew,
		Networks:        len(meta.Networks),
		RemoteSeasons:   meta.Seasons,
		RemoteEpisodes:  meta.RemoteEpisodes,
		PlannedEpisodes: meta.PlannedEpisodes,
		MappedEpisodes:  meta.MappedEpisodes,
		MissingEpisodes: append([]TVEpisodeRef{}, meta.MissingEpisodes...),
	}
	if libraryUsesLocalData(lib) {
		preview.LocalAssets = len(localMatch.Assets)
	}

	if meta.Error != "" {
		preview.Reason = "metadata_fetch_failed"
		preview.Issues = append(preview.Issues, meta.Error)
		return preview, nil
	}
	if meta.Detail == nil {
		preview.Reason = "metadata_detail_missing"
		return preview, nil
	}

	item, found, err := findTVMaterializeMediaItem(ctx, store, lib.ID, lib.MediaType, preview.ExternalIDs, preview.Title, preview.Year)
	if err != nil {
		return preview, err
	}
	if found {
		if !mediatype.IsTVLike(item.MediaType) {
			preview.Reason = "media_type_conflict"
			preview.Issues = append(preview.Issues, fmt.Sprintf("existing_media_item=%d type=%s", item.ID, item.MediaType))
			return preview, nil
		}
		preview.MediaItemID = item.ID
		preview.MediaItemAction = "update_media_item"
		series, hasSeries, err := store.GetTVSeriesByMediaItemID(ctx, item.ID)
		if err != nil {
			return preview, err
		}
		if hasSeries {
			preview.TVSeriesID = series.ID
			preview.TVSeriesAction = "update_tv_series"
		} else {
			preview.TVSeriesAction = "create_tv_series"
		}
		preview.Action = "update"
	} else {
		preview.MediaItemAction = "create_media_item"
		preview.TVSeriesAction = "create_tv_series"
		preview.Action = "create"
	}

	if err := planTVStructureCounts(ctx, store, meta, &preview); err != nil {
		return preview, err
	}

	preview.FileActions = planTVFileActions(ctx, lib.ID, localMatch.Files, filesByRel, preview.MediaItemID, preview.Title, preview.Year, preview.ExternalIDs, store)
	if len(preview.MissingEpisodes) > 0 {
		// A metadata source can lag behind an actively airing show by an
		// episode. Apply the series and every known episode now; the file keeps
		// its parsed season/episode link without a canonical episode ID and will
		// converge on a later scan once upstream metadata catches up.
		preview.Issues = append(preview.Issues, "missing local episodes in fetched metadata: "+formatTVEpisodeRefs(preview.MissingEpisodes, 8))
	}
	if fileIssues := materializeFileIssues(preview.FileActions); len(fileIssues) > 0 {
		preview.Action = "blocked"
		preview.Reason = "file_conflict"
		preview.Issues = append(preview.Issues, fileIssues...)
	} else if preview.Action != "blocked" && hasMovieFileAction(preview.FileActions, "reassign_library_file") {
		preview.Action = "repair"
		preview.Reason = "stale_file_attachment"
	}
	return preview, nil
}

func findTVMaterializeMediaItem(ctx context.Context, store TVMaterializeStore, libraryID int64, mediaType sqlc.MediaType, ids map[string]string, title, year string) (sqlc.MediaItemCard, bool, error) {
	if len(ids) > 0 {
		if item, ok, err := store.FindMediaItemByExternalIDs(ctx, libraryID, ids); err != nil || ok {
			return item, ok, err
		}
	}
	if title == "" {
		return sqlc.MediaItemCard{}, false, nil
	}
	return store.FindMediaItemByIdentity(ctx, libraryID, mediaType, title, year)
}

func planTVStructureCounts(ctx context.Context, store TVMaterializeStore, meta TVFetchPreview, preview *TVMaterializePreview) error {
	detail := meta.Detail
	if detail == nil {
		return nil
	}
	if preview.TVSeriesID == 0 {
		preview.SeasonsCreate = len(detail.Seasons)
		preview.EpisodesCreate = tvRemoteEpisodeCount(detail)
		return nil
	}

	seasons, err := store.ListTVSeasonsBySeries(ctx, preview.TVSeriesID)
	if err != nil {
		return err
	}
	seasonByNumber := map[int]int64{}
	seasonNumberByID := map[int64]int{}
	for _, season := range seasons {
		seasonByNumber[int(season.SeasonNumber)] = season.ID
		seasonNumberByID[season.ID] = int(season.SeasonNumber)
	}
	episodes, err := store.ListTVEpisodesBySeries(ctx, preview.TVSeriesID)
	if err != nil {
		return err
	}
	existingEpisodes := map[TVEpisodeRef]bool{}
	for _, episode := range episodes {
		seasonNumber := seasonNumberByID[episode.SeasonID]
		existingEpisodes[TVEpisodeRef{Season: seasonNumber, Episode: int(episode.EpisodeNumber)}] = true
	}

	for _, season := range detail.Seasons {
		if _, ok := seasonByNumber[season.Number]; ok {
			preview.SeasonsExisting++
		} else {
			preview.SeasonsCreate++
		}
		for _, episode := range season.Episodes {
			if episode.Number <= 0 {
				continue
			}
			ref := TVEpisodeRef{Season: season.Number, Episode: episode.Number}
			if existingEpisodes[ref] {
				preview.EpisodesExisting++
			} else {
				preview.EpisodesCreate++
			}
		}
	}
	return nil
}

func planTVFileActions(ctx context.Context, libraryID int64, relPaths []string, filesByRel map[string][]InventoryFile, targetMediaItemID int64, targetTitle, targetYear string, targetExternalIDs map[string]string, store TVMaterializeStore) []MovieMaterializeFileAction {
	var out []MovieMaterializeFileAction
	for _, relPath := range relPaths {
		action := MovieMaterializeFileAction{RelPath: relPath}
		files := filesByRel[relPath]
		if len(files) != 1 {
			action.Action = "blocked"
			action.Reason = "ambiguous_inventory_file"
			if len(files) == 0 {
				action.Reason = "inventory_file_missing"
			}
			out = append(out, action)
			continue
		}
		action.Path = files[0].Path
		existing, found, err := store.GetLibraryFileByPath(ctx, libraryID, files[0].Path)
		if err != nil {
			action.Action = "blocked"
			action.Reason = err.Error()
			out = append(out, action)
			continue
		}
		if !found {
			action.Action = "create_library_file_and_attach"
			out = append(out, action)
			continue
		}
		action.FileID = existing.ID
		action.Status = string(existing.Status)
		if existing.MediaItemID.Valid {
			action.ExistingMediaItemID = existing.MediaItemID.Int64
			if targetMediaItemID != 0 && existing.MediaItemID.Int64 == targetMediaItemID {
				action.Action = "already_attached"
			} else {
				existingItem, found, err := store.GetMediaItemByID(ctx, existing.MediaItemID.Int64)
				if err != nil {
					action.Action = "blocked"
					action.Reason = err.Error()
					out = append(out, action)
					continue
				}
				if !found {
					action.Action = "blocked"
					action.Reason = "existing_media_item_missing"
					out = append(out, action)
					continue
				}
				action.ExistingItem = movieMaterializeExistingItem(existingItem)
				if canRepairTVFileAttachment(existingItem, targetTitle, targetYear, targetExternalIDs) {
					action.Action = "reassign_library_file"
					action.Reason = "stale_attachment"
				} else {
					action.Action = "blocked"
					action.Reason = "file_attached_elsewhere"
				}
			}
			out = append(out, action)
			continue
		}
		action.Action = "attach_existing_library_file"
		out = append(out, action)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out
}

func canRepairTVFileAttachment(existing sqlc.MediaItemCard, targetTitle, targetYear string, targetExternalIDs map[string]string) bool {
	if !mediatype.IsTVLike(existing.MediaType) {
		return false
	}
	existingIDs := externalIDsFromMediaItem(existing)
	if sharedExternalID(existingIDs, targetExternalIDs) {
		return false
	}
	if normalizeSearchTitle(existing.Title) == normalizeSearchTitle(targetTitle) && existing.Year == targetYear {
		return false
	}
	return true
}

func emitTVMaterializePreview(preview TVMaterializePreview, domain string, emit Emitter) {
	event := "materialize.preview"
	severity := SeverityInfo
	if preview.Action == "blocked" {
		event = "materialize.blocked"
		severity = SeverityWarn
	}
	emit.Emit(Event{
		Event:    event,
		Severity: severity,
		Kind:     domain,
		Reason:   preview.Reason,
		Data: map[string]any{
			"key":               preview.Key,
			"title":             preview.Title,
			"year":              preview.Year,
			"action":            preview.Action,
			"media_item_action": preview.MediaItemAction,
			"tv_series_action":  preview.TVSeriesAction,
			"media_item_id":     preview.MediaItemID,
			"tv_series_id":      preview.TVSeriesID,
			"files":             len(preview.FileActions),
			"issues":            preview.Issues,
		},
	})
}

func tvMaterializeSummary(previews []TVMaterializePreview) map[string]any {
	summary := map[string]any{
		"plans": len(previews),
	}
	for _, preview := range previews {
		summary[preview.Action] = intFromAny(summary[preview.Action]) + 1
		if preview.MediaItemAction != "" {
			summary[preview.MediaItemAction] = intFromAny(summary[preview.MediaItemAction]) + 1
		}
		if preview.TVSeriesAction != "" {
			summary[preview.TVSeriesAction] = intFromAny(summary[preview.TVSeriesAction]) + 1
		}
		summary["seasons_create"] = intFromAny(summary["seasons_create"]) + preview.SeasonsCreate
		summary["seasons_existing"] = intFromAny(summary["seasons_existing"]) + preview.SeasonsExisting
		summary["episodes_create"] = intFromAny(summary["episodes_create"]) + preview.EpisodesCreate
		summary["episodes_existing"] = intFromAny(summary["episodes_existing"]) + preview.EpisodesExisting
		for _, file := range preview.FileActions {
			key := "file_" + file.Action
			summary[key] = intFromAny(summary[key]) + 1
		}
	}
	return summary
}
