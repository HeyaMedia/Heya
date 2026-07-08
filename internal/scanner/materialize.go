package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

type MovieMaterializeStore interface {
	FindMediaItemByExternalIDs(context.Context, int64, map[string]string) (sqlc.MediaItemCard, bool, error)
	FindMediaItemByIdentity(context.Context, int64, string, string) (sqlc.MediaItemCard, bool, error)
	GetMediaItemByID(context.Context, int64) (sqlc.MediaItemCard, bool, error)
	GetMovieByMediaItemID(context.Context, int64) (sqlc.Movie, bool, error)
	GetLibraryFileByPath(context.Context, int64, string) (sqlc.LibraryFile, bool, error)
}

type SQLMovieMaterializeStore struct {
	q *sqlc.Queries
}

func NewSQLMovieMaterializeStore(db sqlc.DBTX) *SQLMovieMaterializeStore {
	return &SQLMovieMaterializeStore{q: sqlc.New(db)}
}

func (s *SQLMovieMaterializeStore) FindMediaItemByExternalIDs(ctx context.Context, libraryID int64, ids map[string]string) (sqlc.MediaItemCard, bool, error) {
	for _, key := range []string{"tmdb", "imdb", "tvdb"} {
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

func (s *SQLMovieMaterializeStore) FindMediaItemByIdentity(ctx context.Context, libraryID int64, title, year string) (sqlc.MediaItemCard, bool, error) {
	item, err := s.q.FindMediaItemByIdentity(ctx, sqlc.FindMediaItemByIdentityParams{
		LibraryID:      libraryID,
		MediaType:      sqlc.MediaTypeMovie,
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

func (s *SQLMovieMaterializeStore) GetMovieByMediaItemID(ctx context.Context, mediaItemID int64) (sqlc.Movie, bool, error) {
	movie, err := s.q.GetMovieByMediaItemID(ctx, mediaItemID)
	if err == nil {
		return movie, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Movie{}, false, nil
	}
	return sqlc.Movie{}, false, err
}

func (s *SQLMovieMaterializeStore) GetMediaItemByID(ctx context.Context, mediaItemID int64) (sqlc.MediaItemCard, bool, error) {
	item, err := s.q.GetMediaItemByID(ctx, mediaItemID)
	if err == nil {
		return item, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.MediaItemCard{}, false, nil
	}
	return sqlc.MediaItemCard{}, false, err
}

func (s *SQLMovieMaterializeStore) GetLibraryFileByPath(ctx context.Context, libraryID int64, path string) (sqlc.LibraryFile, bool, error) {
	file, err := s.q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{LibraryID: libraryID, Path: path})
	if err == nil {
		return file, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.LibraryFile{}, false, nil
	}
	return sqlc.LibraryFile{}, false, err
}

type MovieMaterializePreview struct {
	Key             string                       `json:"key"`
	Action          string                       `json:"action"`
	Reason          string                       `json:"reason,omitempty"`
	Title           string                       `json:"title"`
	Year            string                       `json:"year,omitempty"`
	ProviderID      string                       `json:"provider_id,omitempty"`
	MediaItemID     int64                        `json:"media_item_id,omitempty"`
	MediaItemAction string                       `json:"media_item_action,omitempty"`
	MovieRowAction  string                       `json:"movie_row_action,omitempty"`
	FileActions     []MovieMaterializeFileAction `json:"file_actions,omitempty"`
	ExternalIDs     map[string]string            `json:"external_ids,omitempty"`
	MetadataFields  []string                     `json:"metadata_fields,omitempty"`
	Collection      string                       `json:"collection,omitempty"`
	LocalAssets     int                          `json:"local_assets,omitempty"`
	RemoteArtwork   int                          `json:"remote_artwork,omitempty"`
	Cast            int                          `json:"cast,omitempty"`
	Crew            int                          `json:"crew,omitempty"`
	Issues          []string                     `json:"issues,omitempty"`
}

type MovieMaterializeExistingItem struct {
	ID           int64             `json:"id"`
	Title        string            `json:"title"`
	Year         string            `json:"year,omitempty"`
	MediaType    string            `json:"media_type,omitempty"`
	ProviderKind string            `json:"provider_kind,omitempty"`
	ExternalIDs  map[string]string `json:"external_ids,omitempty"`
}

type MovieMaterializeFileAction struct {
	RelPath             string                        `json:"rel_path"`
	Path                string                        `json:"path,omitempty"`
	Action              string                        `json:"action"`
	FileID              int64                         `json:"file_id,omitempty"`
	ExistingMediaItemID int64                         `json:"existing_media_item_id,omitempty"`
	Status              string                        `json:"status,omitempty"`
	Reason              string                        `json:"reason,omitempty"`
	ExistingItem        *MovieMaterializeExistingItem `json:"existing_item,omitempty"`
}

func PlanMovieMaterialization(ctx context.Context, lib sqlc.Library, result Result, store MovieMaterializeStore, emit Emitter) ([]MovieMaterializePreview, error) {
	if store == nil {
		return nil, fmt.Errorf("movie materialize store is required")
	}

	matches := map[string]MovieMatch{}
	for _, match := range result.MovieMatches {
		matches[match.Key] = match
	}
	metadata := map[string]MovieFetchPreview{}
	for _, preview := range result.MovieMetadata {
		metadata[preview.Key] = preview
	}
	filesByRel := inventoryFilesByRel(result.Inventory)
	useLocalData := libraryUsesLocalData(lib)

	previews := make([]MovieMaterializePreview, 0, len(result.MovieSearch))
	for _, search := range result.MovieSearch {
		if err := ctx.Err(); err != nil {
			return previews, err
		}
		match := matches[search.Key]
		preview := MovieMaterializePreview{
			Key:        search.Key,
			Action:     "blocked",
			ProviderID: search.ProviderID,
			Title:      firstNonEmpty(search.Title, match.Title, search.Query.Title),
			Year:       firstNonEmpty(search.Year, match.Year, search.Query.Year),
		}
		if !search.Accepted {
			preview.Reason = "search_rejected"
			preview.Issues = append(preview.Issues, search.Reason)
			previews = append(previews, preview)
			emitMovieMaterializePreview(preview, emit)
			continue
		}

		meta, ok := metadata[search.Key]
		if !ok {
			preview.Reason = "metadata_not_fetched"
			previews = append(previews, preview)
			emitMovieMaterializePreview(preview, emit)
			continue
		}
		if meta.Error != "" {
			preview.Reason = "metadata_fetch_failed"
			preview.Issues = append(preview.Issues, meta.Error)
			previews = append(previews, preview)
			emitMovieMaterializePreview(preview, emit)
			continue
		}

		preview.Title = firstNonEmpty(meta.Title, preview.Title)
		preview.Year = firstNonEmpty(meta.Year, preview.Year)
		preview.ExternalIDs = mergeStringMaps(match.ExternalIDs, search.ExternalIDs, meta.ExternalIDs)
		preview.MetadataFields = append([]string{}, meta.WouldApply...)
		preview.Collection = meta.Collection
		preview.RemoteArtwork = meta.Artwork
		preview.Cast = meta.Cast
		preview.Crew = meta.Crew
		if useLocalData {
			preview.LocalAssets = len(match.Assets)
		}

		item, found, err := findMaterializeMediaItem(ctx, store, lib.ID, preview.ExternalIDs, preview.Title, preview.Year)
		if err != nil {
			return previews, err
		}
		if found {
			preview.MediaItemID = item.ID
			preview.MediaItemAction = "update_media_item"
			_, hasMovie, err := store.GetMovieByMediaItemID(ctx, item.ID)
			if err != nil {
				return previews, err
			}
			if hasMovie {
				preview.MovieRowAction = "update_movie_row"
			} else {
				preview.MovieRowAction = "create_movie_row"
			}
			preview.Action = "update"
		} else {
			preview.MediaItemAction = "create_media_item"
			preview.MovieRowAction = "create_movie_row"
			preview.Action = "create"
		}

		preview.FileActions = planMovieFileActions(ctx, lib.ID, match.Files, filesByRel, preview.MediaItemID, preview.Title, preview.Year, preview.ExternalIDs, store)
		if fileIssues := materializeFileIssues(preview.FileActions); len(fileIssues) > 0 {
			preview.Action = "blocked"
			preview.Reason = "file_conflict"
			preview.Issues = append(preview.Issues, fileIssues...)
		} else if hasMovieFileAction(preview.FileActions, "reassign_library_file") {
			preview.Action = "repair"
			preview.Reason = "stale_file_attachment"
		}
		previews = append(previews, preview)
		emitMovieMaterializePreview(preview, emit)
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
	emit.Emit(Event{Event: "materialize.preview_summary", Kind: "movie", Data: movieMaterializeSummary(previews)})
	return previews, nil
}

func findMaterializeMediaItem(ctx context.Context, store MovieMaterializeStore, libraryID int64, ids map[string]string, title, year string) (sqlc.MediaItemCard, bool, error) {
	if len(ids) > 0 {
		if item, ok, err := store.FindMediaItemByExternalIDs(ctx, libraryID, ids); err != nil || ok {
			return item, ok, err
		}
	}
	if title == "" {
		return sqlc.MediaItemCard{}, false, nil
	}
	return store.FindMediaItemByIdentity(ctx, libraryID, title, year)
}

func planMovieFileActions(ctx context.Context, libraryID int64, relPaths []string, filesByRel map[string][]InventoryFile, targetMediaItemID int64, targetTitle, targetYear string, targetExternalIDs map[string]string, store MovieMaterializeStore) []MovieMaterializeFileAction {
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
				if canRepairMovieFileAttachment(existingItem, targetTitle, targetYear, targetExternalIDs) {
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

func canRepairMovieFileAttachment(existing sqlc.MediaItemCard, targetTitle, targetYear string, targetExternalIDs map[string]string) bool {
	if existing.MediaType != sqlc.MediaTypeMovie {
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

func movieMaterializeExistingItem(item sqlc.MediaItemCard) *MovieMaterializeExistingItem {
	return &MovieMaterializeExistingItem{
		ID:           item.ID,
		Title:        item.Title,
		Year:         item.Year,
		MediaType:    string(item.MediaType),
		ProviderKind: item.ProviderKind,
		ExternalIDs:  externalIDsFromMediaItem(item),
	}
}

func externalIDsFromMediaItem(item sqlc.MediaItemCard) map[string]string {
	if len(item.ExternalIds) == 0 {
		return nil
	}
	out := map[string]string{}
	if err := json.Unmarshal(item.ExternalIds, &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}

func materializeFileIssues(actions []MovieMaterializeFileAction) []string {
	var issues []string
	for _, action := range actions {
		if action.Action == "blocked" {
			issue := action.RelPath + ":" + action.Reason
			if action.FileID != 0 {
				issue += fmt.Sprintf(" file=%d", action.FileID)
			}
			if action.ExistingMediaItemID != 0 {
				issue += fmt.Sprintf(" existing_media_item=%d", action.ExistingMediaItemID)
			}
			issues = append(issues, issue)
		}
	}
	return issues
}

func hasMovieFileAction(actions []MovieMaterializeFileAction, want string) bool {
	for _, action := range actions {
		if action.Action == want {
			return true
		}
	}
	return false
}

func emitMovieMaterializePreview(preview MovieMaterializePreview, emit Emitter) {
	event := "materialize.preview"
	severity := SeverityInfo
	if preview.Action == "blocked" {
		event = "materialize.blocked"
		severity = SeverityWarn
	}
	emit.Emit(Event{
		Event:    event,
		Severity: severity,
		Kind:     "movie",
		Reason:   preview.Reason,
		Data: map[string]any{
			"key":               preview.Key,
			"title":             preview.Title,
			"year":              preview.Year,
			"action":            preview.Action,
			"media_item_action": preview.MediaItemAction,
			"movie_row_action":  preview.MovieRowAction,
			"media_item_id":     preview.MediaItemID,
			"files":             len(preview.FileActions),
			"issues":            preview.Issues,
		},
	})
}

func inventoryFilesByRel(inv Inventory) map[string][]InventoryFile {
	out := map[string][]InventoryFile{}
	for _, root := range inv.Roots {
		for _, file := range root.Files {
			out[file.RelPath] = append(out[file.RelPath], file)
		}
	}
	return out
}

func movieMaterializeSummary(previews []MovieMaterializePreview) map[string]any {
	summary := map[string]any{
		"plans": len(previews),
	}
	for _, preview := range previews {
		summary[preview.Action] = intFromAny(summary[preview.Action]) + 1
		if preview.MediaItemAction != "" {
			summary[preview.MediaItemAction] = intFromAny(summary[preview.MediaItemAction]) + 1
		}
		if preview.MovieRowAction != "" {
			summary[preview.MovieRowAction] = intFromAny(summary[preview.MovieRowAction]) + 1
		}
		for _, file := range preview.FileActions {
			key := "file_" + file.Action
			summary[key] = intFromAny(summary[key]) + 1
		}
	}
	return summary
}

func intFromAny(v any) int {
	if n, ok := v.(int); ok {
		return n
	}
	return 0
}

func materializeActionRank(action string) int {
	switch action {
	case "blocked":
		return 0
	case "repair":
		return 1
	case "create":
		return 2
	case "update":
		return 3
	default:
		return 4
	}
}

func mergeStringMaps(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for key, value := range m {
			if value == "" {
				continue
			}
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mustJSONBytes(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
