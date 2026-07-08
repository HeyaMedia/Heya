package scanner

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/parser"
)

type BookApplyResult struct {
	Key                  string `json:"key"`
	Action               string `json:"action"`
	Reason               string `json:"reason,omitempty"`
	Title                string `json:"title"`
	Author               string `json:"author,omitempty"`
	Year                 string `json:"year,omitempty"`
	Format               string `json:"format,omitempty"`
	ProviderID           string `json:"provider_id,omitempty"`
	MediaItemID          int64  `json:"media_item_id,omitempty"`
	MediaItemAction      string `json:"media_item_action,omitempty"`
	BookRowAction        string `json:"book_row_action,omitempty"`
	FilesCreated         int    `json:"files_created,omitempty"`
	FilesAttached        int    `json:"files_attached,omitempty"`
	FilesAlreadyAttached int    `json:"files_already_attached,omitempty"`
	FilesReassigned      int    `json:"files_reassigned,omitempty"`
	LocalAssets          int    `json:"local_assets,omitempty"`
	RemoteAssets         int    `json:"remote_assets,omitempty"`
	Skipped              bool   `json:"skipped,omitempty"`
	Error                string `json:"error,omitempty"`
}

func ApplyBookMaterialization(ctx context.Context, lib sqlc.Library, result Result, db *pgxpool.Pool, emit Emitter) ([]BookApplyResult, error) {
	if db == nil {
		return nil, fmt.Errorf("book apply db is required")
	}
	if lib.MediaType != sqlc.MediaTypeBook {
		return nil, fmt.Errorf("book apply only supports book libraries (got %q)", lib.MediaType)
	}
	if len(result.BookMaterialize) == 0 {
		return nil, nil
	}

	plans := map[string]BookPlan{}
	for _, plan := range result.BookPlans {
		plans[plan.Key] = plan
	}
	metadataByKey := map[string]BookFetchPreview{}
	for _, preview := range result.BookMetadata {
		metadataByKey[preview.Key] = preview
	}
	filesByRel := inventoryFilesByRel(result.Inventory)
	useLocalData := libraryUsesLocalData(lib)

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin book apply: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)
	lookupStore := NewSQLBookMaterializeStore(tx)

	results := make([]BookApplyResult, 0, len(result.BookMaterialize))
	for _, preview := range result.BookMaterialize {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		applied := BookApplyResult{
			Key:             preview.Key,
			Action:          preview.Action,
			Reason:          preview.Reason,
			Title:           preview.Title,
			Author:          preview.Author,
			Year:            preview.Year,
			Format:          preview.Format,
			ProviderID:      preview.ProviderID,
			MediaItemID:     preview.MediaItemID,
			MediaItemAction: preview.MediaItemAction,
			BookRowAction:   preview.BookRowAction,
		}
		if preview.Action == "blocked" {
			applied.Action = "skipped"
			applied.Skipped = true
			if applied.Reason == "" {
				applied.Reason = "materialization_blocked"
			}
			results = append(results, applied)
			emitBookApplyResult(applied, emit)
			continue
		}

		meta, ok := metadataByKey[preview.Key]
		if !ok || meta.Detail == nil {
			applied.Action = "failed"
			applied.Error = "metadata detail is required for apply"
			results = append(results, applied)
			emitBookApplyResult(applied, emit)
			return results, fmt.Errorf("apply %s: metadata detail is required", preview.Key)
		}
		if meta.Error != "" {
			applied.Action = "failed"
			applied.Error = meta.Error
			results = append(results, applied)
			emitBookApplyResult(applied, emit)
			return results, fmt.Errorf("apply %s: metadata fetch failed: %s", preview.Key, meta.Error)
		}

		detail := bookApplyDetail(meta.Detail, preview)
		item, mediaAction, err := applyBookMediaItem(ctx, q, lookupStore, lib.ID, preview, detail)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitBookApplyResult(applied, emit)
			return results, fmt.Errorf("apply book media item %s: %w", preview.Key, err)
		}
		applied.MediaItemID = item.ID
		applied.MediaItemAction = mediaAction
		if applied.Action != "repair" && mediaAction == "create_media_item" {
			applied.Action = "create"
		}

		bookRow, rowAction, err := applyBookRow(ctx, q, item.ID, detail, preview)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitBookApplyResult(applied, emit)
			return results, fmt.Errorf("apply book row %s: %w", preview.Key, err)
		}
		applied.BookRowAction = firstNonEmpty(preview.BookRowAction, rowAction)

		fileCounts, err := applyBookFiles(ctx, q, lib.ID, item.ID, bookRow.ID, preview, filesByRel)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitBookApplyResult(applied, emit)
			return results, fmt.Errorf("apply book files %s: %w", preview.Key, err)
		}
		applied.FilesCreated = fileCounts.created
		applied.FilesAttached = fileCounts.attached
		applied.FilesAlreadyAttached = fileCounts.alreadyAttached
		applied.FilesReassigned = fileCounts.reassigned

		plan := plans[preview.Key]
		if useLocalData {
			localAssets, err := applyBookLocalAssets(ctx, q, item.ID, plan, filesByRel)
			if err != nil {
				applied.Action = "failed"
				applied.Error = err.Error()
				results = append(results, applied)
				emitBookApplyResult(applied, emit)
				return results, fmt.Errorf("apply book local assets %s: %w", preview.Key, err)
			}
			applied.LocalAssets = localAssets
		}

		remoteAssets, err := applyBookRemoteAssets(ctx, q, item.ID, detail)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitBookApplyResult(applied, emit)
			return results, fmt.Errorf("apply book remote assets %s: %w", preview.Key, err)
		}
		applied.RemoteAssets = remoteAssets

		if err := markMovieApplyEnriched(ctx, q, item.ID); err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitBookApplyResult(applied, emit)
			return results, fmt.Errorf("mark book enriched %s: %w", preview.Key, err)
		}

		results = append(results, applied)
		emitBookApplyResult(applied, emit)
	}

	if err := tx.Commit(ctx); err != nil {
		return results, fmt.Errorf("commit book apply: %w", err)
	}
	emitBookApplySummary(results, emit)
	return results, nil
}

func bookApplyDetail(detail *metadata.MediaDetail, preview BookMaterializePreview) *metadata.MediaDetail {
	d := *detail
	d.ExternalIDs = mergeStringMaps(preview.ExternalIDs, detail.ExternalIDs)
	if d.ExternalIDs["ol_work_id"] != "" && d.ExternalIDs["openlibrary"] == "" {
		d.ExternalIDs["openlibrary"] = d.ExternalIDs["ol_work_id"]
	}
	if d.Title == "" {
		d.Title = preview.Title
	}
	if d.Year == "" {
		d.Year = preview.Year
	}
	if d.AuthorName == "" {
		d.AuthorName = preview.Author
	}
	if d.SortTitle == "" {
		d.SortTitle = strings.ToLower(d.Title)
	}
	if d.ProviderKind == "" {
		d.ProviderKind = providerKindFromID(preview.ProviderID)
	}
	return &d
}

func applyBookMediaItem(ctx context.Context, q *sqlc.Queries, lookupStore BookMaterializeStore, libraryID int64, preview BookMaterializePreview, detail *metadata.MediaDetail) (sqlc.MediaItemCard, string, error) {
	var existing sqlc.MediaItemCard
	found := false
	if preview.MediaItemID != 0 {
		item, err := q.GetMediaItemByID(ctx, preview.MediaItemID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return sqlc.MediaItemCard{}, "", err
		}
		if err == nil {
			existing = item
			found = true
		}
	}
	if !found {
		item, ok, err := findBookMaterializeMediaItem(ctx, lookupStore, libraryID, detail.ExternalIDs, detail.Title, detail.Year)
		if err != nil {
			return sqlc.MediaItemCard{}, "", err
		}
		if ok {
			existing = item
			found = true
		}
	}
	if found {
		updated, err := q.UpdateMediaItem(ctx, bookUpdateMediaItemParams(existing, detail))
		if err != nil {
			return sqlc.MediaItemCard{}, "", err
		}
		if updated.Slug == "" {
			if err := updateMovieSlug(ctx, q, updated.ID, updated.Title, updated.Year); err != nil {
				return sqlc.MediaItemCard{}, "", err
			}
		}
		if err := q.MarkMatched(ctx, updated.ID); err != nil {
			return sqlc.MediaItemCard{}, "", err
		}
		return updated, "update_media_item", nil
	}
	item, err := q.CreateMediaItem(ctx, bookCreateMediaItemParams(libraryID, detail))
	if err != nil {
		return sqlc.MediaItemCard{}, "", err
	}
	if err := updateMovieSlug(ctx, q, item.ID, item.Title, item.Year); err != nil {
		return sqlc.MediaItemCard{}, "", err
	}
	if err := q.MarkMatched(ctx, item.ID); err != nil {
		return sqlc.MediaItemCard{}, "", err
	}
	return item, "create_media_item", nil
}

func bookCreateMediaItemParams(libraryID int64, detail *metadata.MediaDetail) sqlc.CreateMediaItemParams {
	title := firstNonEmpty(detail.Title, "Untitled")
	return sqlc.CreateMediaItemParams{
		LibraryID:        libraryID,
		MediaType:        sqlc.MediaTypeBook,
		Title:            title,
		SortTitle:        firstNonEmpty(detail.SortTitle, strings.ToLower(title)),
		Year:             detail.Year,
		Description:      detail.Description,
		PosterPath:       detail.PosterURL,
		ExternalIds:      mustJSONBytes(detail.ExternalIDs),
		OriginalLanguage: detail.Language,
		ProviderKind:     firstNonEmpty(detail.ProviderKind, "heya"),
		HeyaSlug:         detail.HeyaSlug,
	}
}

func bookUpdateMediaItemParams(existing sqlc.MediaItemCard, detail *metadata.MediaDetail) sqlc.UpdateMediaItemParams {
	title := firstNonEmpty(detail.Title, existing.Title, "Untitled")
	return sqlc.UpdateMediaItemParams{
		ID:               existing.ID,
		Title:            title,
		SortTitle:        firstNonEmpty(detail.SortTitle, strings.ToLower(title)),
		Year:             firstNonEmpty(detail.Year, existing.Year),
		Description:      firstNonEmpty(detail.Description, existing.Description),
		PosterPath:       firstNonEmpty(detail.PosterURL, existing.PosterPath),
		BackdropPath:     existing.BackdropPath,
		ExternalIds:      mustJSONBytes(mergeStringMaps(externalIDsFromMediaItem(existing), detail.ExternalIDs)),
		Tagline:          existing.Tagline,
		OriginalTitle:    existing.OriginalTitle,
		OriginalLanguage: firstNonEmpty(detail.Language, existing.OriginalLanguage),
		Status:           existing.Status,
		ProviderKind:     firstNonEmpty(detail.ProviderKind, existing.ProviderKind, "heya"),
		HeyaSlug:         firstNonEmpty(detail.HeyaSlug, existing.HeyaSlug),
	}
}

func applyBookRow(ctx context.Context, q *sqlc.Queries, mediaItemID int64, detail *metadata.MediaDetail, preview BookMaterializePreview) (sqlc.Book, string, error) {
	authorID, err := applyBookAuthor(ctx, q, detail)
	if err != nil {
		return sqlc.Book{}, "", err
	}
	filePath := firstBookFilePath(preview)
	bookFormat := bookDatabaseFormat(preview)
	openLibraryID := firstNonEmpty(detail.ExternalIDs["openlibrary"], detail.ExternalIDs["ol_work_id"])
	existing, err := q.GetBookByMediaItemID(ctx, mediaItemID)
	if err == nil {
		updated, err := q.UpdateBook(ctx, sqlc.UpdateBookParams{
			ID:            existing.ID,
			AuthorID:      authorID,
			Isbn:          detail.ISBN,
			OpenlibraryID: openLibraryID,
			PageCount:     int32(detail.PageCount),
			Publisher:     detail.Publisher,
			PublishDate:   pgDateFromStringLocal(detail.PublishDate),
			FilePath:      firstNonEmpty(filePath, existing.FilePath),
			Subjects:      detail.Subjects,
			Language:      detail.Language,
			SeriesName:    detail.SeriesName,
			SeriesNumber:  int32(detail.SeriesNum),
			Format:        firstNonEmpty(bookFormat, existing.Format),
			Description:   detail.Description,
		})
		return updated, "update_book_row", err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Book{}, "", err
	}
	created, err := q.CreateBook(ctx, sqlc.CreateBookParams{
		MediaItemID:   mediaItemID,
		AuthorID:      authorID,
		Isbn:          detail.ISBN,
		OpenlibraryID: openLibraryID,
		PageCount:     int32(detail.PageCount),
		Publisher:     detail.Publisher,
		PublishDate:   pgDateFromStringLocal(detail.PublishDate),
		FilePath:      filePath,
		Subjects:      detail.Subjects,
		Language:      detail.Language,
		SeriesName:    detail.SeriesName,
		SeriesNumber:  int32(detail.SeriesNum),
		Format:        bookFormat,
		Description:   detail.Description,
	})
	return created, "create_book_row", err
}

func bookDatabaseFormat(preview BookMaterializePreview) string {
	if preview.Format == "audiobook" {
		return "audiobook"
	}
	return firstNonEmpty(preview.FileFormat, preview.Format)
}

func applyBookAuthor(ctx context.Context, q *sqlc.Queries, detail *metadata.MediaDetail) (pgtype.Int8, error) {
	if detail.AuthorName == "" {
		return pgtype.Int8{}, nil
	}
	authorOpenLibraryID := detail.ExternalIDs["openlibrary_author"]
	if authorOpenLibraryID != "" {
		author, err := q.GetAuthorByOpenLibraryID(ctx, authorOpenLibraryID)
		if err == nil {
			return pgInt8(author.ID), nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return pgtype.Int8{}, err
		}
	}
	author, err := q.GetAuthorByName(ctx, detail.AuthorName)
	if err == nil {
		return pgInt8(author.ID), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return pgtype.Int8{}, err
	}
	created, err := q.CreateAuthor(ctx, sqlc.CreateAuthorParams{
		Name:          detail.AuthorName,
		OpenlibraryID: authorOpenLibraryID,
		Biography:     detail.AuthorBio,
		BirthDate:     detail.AuthorBirthDate,
		DeathDate:     detail.AuthorDeathDate,
	})
	if err != nil {
		return pgtype.Int8{}, err
	}
	return pgInt8(created.ID), nil
}

type bookFileApplyCounts = movieFileApplyCounts

func applyBookFiles(ctx context.Context, q *sqlc.Queries, libraryID, mediaItemID, bookID int64, preview BookMaterializePreview, filesByRel map[string][]InventoryFile) (bookFileApplyCounts, error) {
	var counts bookFileApplyCounts
	for index, action := range preview.FileActions {
		if action.Action == "blocked" {
			return counts, fmt.Errorf("%s blocked: %s", action.RelPath, action.Reason)
		}
		fileID := action.FileID
		switch action.Action {
		case "create_library_file_and_attach":
			invFile, ok := singleInventoryFile(filesByRel, action.RelPath)
			if !ok {
				return counts, fmt.Errorf("%s inventory file missing", action.RelPath)
			}
			file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
				LibraryID:   libraryID,
				Path:        invFile.Path,
				Size:        invFile.Size,
				Mtime:       pgtype.Timestamptz{Time: invFile.MTime, Valid: !invFile.MTime.IsZero()},
				ParseResult: bookLibraryFileParseResult(preview, action.RelPath),
				Status:      sqlc.FileStatusPending,
			})
			if err != nil {
				return counts, err
			}
			fileID = file.ID
			counts.created++
			counts.attached++
		case "attach_existing_library_file":
			counts.attached++
		case "already_attached":
			counts.alreadyAttached++
		case "reassign_library_file":
			counts.reassigned++
		default:
			return counts, fmt.Errorf("%s unsupported file action %q", action.RelPath, action.Action)
		}
		if fileID == 0 {
			return counts, fmt.Errorf("%s has no library file id", action.RelPath)
		}
		if err := q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
			ID:          fileID,
			Status:      sqlc.FileStatusMatched,
			MediaItemID: pgInt8(mediaItemID),
		}); err != nil {
			return counts, err
		}
		relationType := "primary"
		var partIndex pgtype.Int4
		if len(preview.FileActions) > 1 {
			relationType = "part"
			partIndex = pgInt4(int32(index + 1))
		}
		if err := replaceBookLibraryFileLink(ctx, q, fileID, mediaItemID, bookID, relationType, partIndex); err != nil {
			return counts, err
		}
	}
	return counts, nil
}

func replaceBookLibraryFileLink(ctx context.Context, q *sqlc.Queries, libraryFileID, mediaItemID, _ int64, relationType string, partIndex pgtype.Int4) error {
	if err := q.DeleteLibraryFileLinksByFile(ctx, libraryFileID); err != nil {
		return err
	}
	_, err := q.CreateLibraryFileLink(ctx, sqlc.CreateLibraryFileLinkParams{
		LibraryFileID: libraryFileID,
		MediaItemID:   mediaItemID,
		RelationType:  relationType,
		PartIndex:     partIndex,
		Title:         "",
		Source:        "scanner",
		Confidence:    1,
	})
	return err
}

func firstBookFilePath(preview BookMaterializePreview) string {
	for _, action := range preview.FileActions {
		if action.Path != "" {
			return action.Path
		}
	}
	return ""
}

func bookLibraryFileParseResult(preview BookMaterializePreview, relPath string) []byte {
	parsed := parser.ParseStoragePath(relPath)
	return mustJSONBytes(map[string]any{
		"scanner":     "scanner",
		"parsed":      parsed,
		"match_key":   preview.Key,
		"provider_id": preview.ProviderID,
		"format":      preview.Format,
	})
}

func applyBookLocalAssets(ctx context.Context, q *sqlc.Queries, mediaItemID int64, plan BookPlan, filesByRel map[string][]InventoryFile) (int, error) {
	created := 0
	sortOrders := map[sqlc.AssetType]int32{}
	for _, asset := range plan.Assets {
		assetType, ok := movieLocalAssetType(asset.Type)
		if !ok {
			continue
		}
		invFile, ok := singleInventoryFile(filesByRel, asset.RelPath)
		if !ok {
			continue
		}
		sortOrder := sortOrders[assetType]
		sortOrders[assetType]++
		ok, err := createMovieAsset(ctx, q, sqlc.CreateMediaAssetParams{
			MediaItemID: mediaItemID,
			AssetType:   assetType,
			Source:      "local",
			LocalPath:   invFile.Path,
			SortOrder:   sortOrder,
			FileSize:    invFile.Size,
		})
		if err != nil {
			return created, err
		}
		if ok {
			created++
		}
	}
	return created, nil
}

func applyBookRemoteAssets(ctx context.Context, q *sqlc.Queries, mediaItemID int64, detail *metadata.MediaDetail) (int, error) {
	if detail.PosterURL == "" {
		return 0, nil
	}
	ok, err := createMovieAsset(ctx, q, sqlc.CreateMediaAssetParams{
		MediaItemID: mediaItemID,
		AssetType:   sqlc.AssetTypePoster,
		Source:      "remote",
		RemoteUrl:   detail.PosterURL,
		SortOrder:   0,
	})
	if err != nil || !ok {
		return 0, err
	}
	return 1, nil
}

func pgDateFromStringLocal(value string) pgtype.Date {
	value = strings.TrimSpace(value)
	if value == "" {
		return pgtype.Date{}
	}
	for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(layout, value); err == nil {
			return pgtype.Date{Time: t, Valid: true}
		}
	}
	return pgtype.Date{}
}

func emitBookApplyResult(result BookApplyResult, emit Emitter) {
	if emit == nil {
		return
	}
	event := "materialize.apply"
	severity := SeverityInfo
	switch result.Action {
	case "skipped":
		event = "materialize.apply_skipped"
	case "failed":
		event = "materialize.apply_failed"
		severity = SeverityWarn
	}
	emit.Emit(Event{
		Event:    event,
		Severity: severity,
		Kind:     "book",
		Reason:   result.Reason,
		Message:  result.Error,
		Data: map[string]any{
			"key":              result.Key,
			"title":            result.Title,
			"author":           result.Author,
			"year":             result.Year,
			"format":           result.Format,
			"action":           result.Action,
			"media_item_id":    result.MediaItemID,
			"files_created":    result.FilesCreated,
			"files_attached":   result.FilesAttached,
			"files_reassigned": result.FilesReassigned,
			"local_assets":     result.LocalAssets,
			"remote_assets":    result.RemoteAssets,
		},
	})
}

func emitBookApplySummary(results []BookApplyResult, emit Emitter) {
	if emit == nil {
		return
	}
	emit.Emit(Event{Event: "materialize.apply_summary", Kind: "book", Data: bookApplySummary(results)})
}

func bookApplySummary(results []BookApplyResult) map[string]any {
	summary := map[string]any{"plans": len(results)}
	for _, result := range results {
		summary[result.Action] = intFromAny(summary[result.Action]) + 1
		if result.MediaItemAction != "" {
			summary[result.MediaItemAction] = intFromAny(summary[result.MediaItemAction]) + 1
		}
		summary["files_created"] = intFromAny(summary["files_created"]) + result.FilesCreated
		summary["files_attached"] = intFromAny(summary["files_attached"]) + result.FilesAttached
		summary["files_already_attached"] = intFromAny(summary["files_already_attached"]) + result.FilesAlreadyAttached
		summary["files_reassigned"] = intFromAny(summary["files_reassigned"]) + result.FilesReassigned
		summary["local_assets"] = intFromAny(summary["local_assets"]) + result.LocalAssets
		summary["remote_assets"] = intFromAny(summary["remote_assets"]) + result.RemoteAssets
	}
	return summary
}

func sortBookApplyResults(items []BookApplyResult) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Action == items[j].Action {
			if items[i].Author == items[j].Author {
				if items[i].Title == items[j].Title {
					return items[i].Year < items[j].Year
				}
				return items[i].Title < items[j].Title
			}
			return items[i].Author < items[j].Author
		}
		return movieApplyActionRank(items[i].Action) < movieApplyActionRank(items[j].Action)
	})
}
