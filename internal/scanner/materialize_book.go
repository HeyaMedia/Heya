package scanner

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

type BookMaterializeStore interface {
	FindMediaItemByExternalIDs(context.Context, int64, map[string]string) (sqlc.MediaItemCard, bool, error)
	FindMediaItemByIdentity(context.Context, int64, string, string) (sqlc.MediaItemCard, bool, error)
	GetMediaItemByID(context.Context, int64) (sqlc.MediaItemCard, bool, error)
	GetBookByMediaItemID(context.Context, int64) (sqlc.Book, bool, error)
	GetLibraryFileByPath(context.Context, int64, string) (sqlc.LibraryFile, bool, error)
}

type SQLBookMaterializeStore struct {
	q *sqlc.Queries
}

func NewSQLBookMaterializeStore(db sqlc.DBTX) *SQLBookMaterializeStore {
	return &SQLBookMaterializeStore{q: sqlc.New(db)}
}

func (s *SQLBookMaterializeStore) FindMediaItemByExternalIDs(ctx context.Context, libraryID int64, ids map[string]string) (sqlc.MediaItemCard, bool, error) {
	for _, key := range []string{"ol_work_id", "openlibrary", "isbn", "google_books", "audible"} {
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

func (s *SQLBookMaterializeStore) FindMediaItemByIdentity(ctx context.Context, libraryID int64, title, year string) (sqlc.MediaItemCard, bool, error) {
	item, err := s.q.FindMediaItemByIdentity(ctx, sqlc.FindMediaItemByIdentityParams{
		LibraryID:      libraryID,
		MediaType:      sqlc.MediaTypeBook,
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

func (s *SQLBookMaterializeStore) GetMediaItemByID(ctx context.Context, mediaItemID int64) (sqlc.MediaItemCard, bool, error) {
	item, err := s.q.GetMediaItemByID(ctx, mediaItemID)
	if err == nil {
		return item, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.MediaItemCard{}, false, nil
	}
	return sqlc.MediaItemCard{}, false, err
}

func (s *SQLBookMaterializeStore) GetBookByMediaItemID(ctx context.Context, mediaItemID int64) (sqlc.Book, bool, error) {
	book, err := s.q.GetBookByMediaItemID(ctx, mediaItemID)
	if err == nil {
		return book, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Book{}, false, nil
	}
	return sqlc.Book{}, false, err
}

func (s *SQLBookMaterializeStore) GetLibraryFileByPath(ctx context.Context, libraryID int64, path string) (sqlc.LibraryFile, bool, error) {
	file, err := s.q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{LibraryID: libraryID, Path: path})
	if err == nil {
		return file, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.LibraryFile{}, false, nil
	}
	return sqlc.LibraryFile{}, false, err
}

type BookMaterializePreview struct {
	Key             string                       `json:"key"`
	Action          string                       `json:"action"`
	Reason          string                       `json:"reason,omitempty"`
	Title           string                       `json:"title"`
	Author          string                       `json:"author,omitempty"`
	Year            string                       `json:"year,omitempty"`
	Format          string                       `json:"format,omitempty"`
	FileFormat      string                       `json:"file_format,omitempty"`
	ProviderID      string                       `json:"provider_id,omitempty"`
	MediaItemID     int64                        `json:"media_item_id,omitempty"`
	MediaItemAction string                       `json:"media_item_action,omitempty"`
	BookRowAction   string                       `json:"book_row_action,omitempty"`
	FileActions     []MovieMaterializeFileAction `json:"file_actions,omitempty"`
	ExternalIDs     map[string]string            `json:"external_ids,omitempty"`
	MetadataFields  []string                     `json:"metadata_fields,omitempty"`
	LocalAssets     int                          `json:"local_assets,omitempty"`
	RemoteArtwork   int                          `json:"remote_artwork,omitempty"`
	PageCount       int                          `json:"page_count,omitempty"`
	Subjects        int                          `json:"subjects,omitempty"`
	Issues          []string                     `json:"issues,omitempty"`
}

func PlanBookMaterialization(ctx context.Context, lib sqlc.Library, result Result, store BookMaterializeStore, emit Emitter) ([]BookMaterializePreview, error) {
	if store == nil {
		return nil, fmt.Errorf("book materialize store is required")
	}

	plans := map[string]BookPlan{}
	for _, plan := range result.BookPlans {
		plans[plan.Key] = plan
	}
	metadata := map[string]BookFetchPreview{}
	for _, preview := range result.BookMetadata {
		metadata[preview.Key] = preview
	}
	filesByRel := inventoryFilesByRel(result.Inventory)

	previews := make([]BookMaterializePreview, 0, len(result.BookSearch))
	for _, search := range result.BookSearch {
		if err := ctx.Err(); err != nil {
			return previews, err
		}
		plan := plans[search.Key]
		preview := BookMaterializePreview{
			Key:        search.Key,
			Action:     "blocked",
			ProviderID: search.ProviderID,
			Title:      firstNonEmpty(search.Title, plan.Title, search.Query.Title),
			Author:     firstNonEmpty(search.Author, plan.Author, search.Query.Author),
			Year:       firstNonEmpty(search.Year, plan.Year, search.Query.Year),
			Format:     firstNonEmpty(search.Format, plan.Format, search.Query.Format),
			FileFormat: plan.FileFormat,
		}
		if !search.Accepted {
			preview.Reason = "search_rejected"
			preview.Issues = append(preview.Issues, search.Reason)
			previews = append(previews, preview)
			emitBookMaterializePreview(preview, emit)
			continue
		}

		meta, ok := metadata[search.Key]
		if !ok {
			preview.Reason = "metadata_not_fetched"
			previews = append(previews, preview)
			emitBookMaterializePreview(preview, emit)
			continue
		}
		if meta.Error != "" {
			preview.Reason = "metadata_fetch_failed"
			preview.Issues = append(preview.Issues, meta.Error)
			previews = append(previews, preview)
			emitBookMaterializePreview(preview, emit)
			continue
		}
		if fatalBookMetadataIssues(meta.Issues) {
			preview.Reason = "metadata_mismatch"
			preview.Issues = append(preview.Issues, meta.Issues...)
			previews = append(previews, preview)
			emitBookMaterializePreview(preview, emit)
			continue
		}
		preview.Issues = append(preview.Issues, meta.Issues...)

		preview.Title = firstNonEmpty(meta.Title, preview.Title)
		preview.Author = firstNonEmpty(meta.Author, preview.Author)
		preview.Year = firstNonEmpty(meta.Year, preview.Year)
		preview.ExternalIDs = mergeStringMaps(plan.ExternalIDs, search.ExternalIDs, meta.ExternalIDs)
		preview.MetadataFields = append([]string{}, meta.WouldApply...)
		preview.PageCount = meta.PageCount
		preview.Subjects = len(meta.Subjects)
		if libraryUsesLocalData(lib) {
			preview.LocalAssets = len(plan.Assets)
		}
		if meta.PosterURL != "" {
			preview.RemoteArtwork = 1
		}

		item, found, err := findBookMaterializeMediaItem(ctx, store, lib.ID, preview.ExternalIDs, preview.Title, preview.Year)
		if err != nil {
			return previews, err
		}
		if found {
			preview.MediaItemID = item.ID
			preview.MediaItemAction = "update_media_item"
			_, hasBook, err := store.GetBookByMediaItemID(ctx, item.ID)
			if err != nil {
				return previews, err
			}
			if hasBook {
				preview.BookRowAction = "update_book_row"
			} else {
				preview.BookRowAction = "create_book_row"
			}
			preview.Action = "update"
		} else {
			preview.MediaItemAction = "create_media_item"
			preview.BookRowAction = "create_book_row"
			preview.Action = "create"
		}

		preview.FileActions = planBookFileActions(ctx, lib.ID, plan.Files, filesByRel, preview.MediaItemID, preview.Title, preview.Year, preview.ExternalIDs, store)
		if fileIssues := materializeFileIssues(preview.FileActions); len(fileIssues) > 0 {
			preview.Action = "blocked"
			preview.Reason = "file_conflict"
			preview.Issues = append(preview.Issues, fileIssues...)
		} else if hasMovieFileAction(preview.FileActions, "reassign_library_file") {
			preview.Action = "repair"
			preview.Reason = "stale_file_attachment"
		}
		previews = append(previews, preview)
		emitBookMaterializePreview(preview, emit)
	}

	sort.Slice(previews, func(i, j int) bool {
		if previews[i].Action == previews[j].Action {
			if previews[i].Author == previews[j].Author {
				if previews[i].Title == previews[j].Title {
					return previews[i].Year < previews[j].Year
				}
				return previews[i].Title < previews[j].Title
			}
			return previews[i].Author < previews[j].Author
		}
		return materializeActionRank(previews[i].Action) < materializeActionRank(previews[j].Action)
	})
	emit.Emit(Event{Event: "materialize.preview_summary", Kind: "book", Data: bookMaterializeSummary(previews)})
	return previews, nil
}

func fatalBookMetadataIssues(issues []string) bool {
	for _, issue := range issues {
		if strings.HasPrefix(issue, "author_mismatch") || strings.HasPrefix(issue, "title_mismatch") {
			return true
		}
	}
	return false
}

func findBookMaterializeMediaItem(ctx context.Context, store BookMaterializeStore, libraryID int64, ids map[string]string, title, year string) (sqlc.MediaItemCard, bool, error) {
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

func planBookFileActions(ctx context.Context, libraryID int64, relPaths []string, filesByRel map[string][]InventoryFile, targetMediaItemID int64, targetTitle, targetYear string, targetExternalIDs map[string]string, store BookMaterializeStore) []MovieMaterializeFileAction {
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
				if canRepairBookFileAttachment(existingItem, targetTitle, targetYear, targetExternalIDs) {
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

func canRepairBookFileAttachment(existing sqlc.MediaItemCard, targetTitle, targetYear string, targetExternalIDs map[string]string) bool {
	if existing.MediaType != sqlc.MediaTypeBook {
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

func emitBookMaterializePreview(preview BookMaterializePreview, emit Emitter) {
	event := "materialize.preview"
	severity := SeverityInfo
	if preview.Action == "blocked" {
		event = "materialize.blocked"
		severity = SeverityWarn
	}
	emit.Emit(Event{
		Event:    event,
		Severity: severity,
		Kind:     "book",
		Reason:   preview.Reason,
		Data: map[string]any{
			"key":               preview.Key,
			"title":             preview.Title,
			"author":            preview.Author,
			"year":              preview.Year,
			"format":            preview.Format,
			"action":            preview.Action,
			"media_item_action": preview.MediaItemAction,
			"book_row_action":   preview.BookRowAction,
			"media_item_id":     preview.MediaItemID,
			"files":             len(preview.FileActions),
			"issues":            preview.Issues,
		},
	})
}

func bookMaterializeSummary(previews []BookMaterializePreview) map[string]any {
	summary := map[string]any{"plans": len(previews)}
	for _, preview := range previews {
		summary[preview.Action] = intFromAny(summary[preview.Action]) + 1
		if preview.MediaItemAction != "" {
			summary[preview.MediaItemAction] = intFromAny(summary[preview.MediaItemAction]) + 1
		}
		if preview.BookRowAction != "" {
			summary[preview.BookRowAction] = intFromAny(summary[preview.BookRowAction]) + 1
		}
		for _, file := range preview.FileActions {
			key := "file_" + file.Action
			summary[key] = intFromAny(summary[key]) + 1
		}
	}
	return summary
}
