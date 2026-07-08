package ingestv2

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/karbowiak/heya/internal/slug"
)

type MovieApplyResult struct {
	Key                  string `json:"key"`
	Action               string `json:"action"`
	Reason               string `json:"reason,omitempty"`
	Title                string `json:"title"`
	Year                 string `json:"year,omitempty"`
	ProviderID           string `json:"provider_id,omitempty"`
	MediaItemID          int64  `json:"media_item_id,omitempty"`
	MediaItemAction      string `json:"media_item_action,omitempty"`
	MovieRowAction       string `json:"movie_row_action,omitempty"`
	FilesCreated         int    `json:"files_created,omitempty"`
	FilesAttached        int    `json:"files_attached,omitempty"`
	FilesAlreadyAttached int    `json:"files_already_attached,omitempty"`
	FilesReassigned      int    `json:"files_reassigned,omitempty"`
	LocalAssets          int    `json:"local_assets,omitempty"`
	LocalExtras          int    `json:"local_extras,omitempty"`
	RemoteAssets         int    `json:"remote_assets,omitempty"`
	RichMetadata         bool   `json:"rich_metadata,omitempty"`
	Skipped              bool   `json:"skipped,omitempty"`
	Error                string `json:"error,omitempty"`
}

func ApplyMovieMaterialization(ctx context.Context, lib sqlc.Library, result Result, db *pgxpool.Pool, emit Emitter) ([]MovieApplyResult, error) {
	if db == nil {
		return nil, fmt.Errorf("movie apply db is required")
	}
	if lib.MediaType != sqlc.MediaTypeMovie {
		return nil, fmt.Errorf("movie apply only supports movie libraries (got %q)", lib.MediaType)
	}
	if len(result.MovieMaterialize) == 0 {
		return nil, nil
	}

	matches := map[string]MovieMatch{}
	for _, match := range result.MovieMatches {
		matches[match.Key] = match
	}
	metadataByKey := map[string]MovieFetchPreview{}
	for _, preview := range result.MovieMetadata {
		metadataByKey[preview.Key] = preview
	}
	filesByRel := inventoryFilesByRel(result.Inventory)

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin movie apply: %w", err)
	}
	defer tx.Rollback(ctx)

	q := sqlc.New(tx)
	txMatcher := matcher.New(db, matcher.MatchOptions{}, nil, nil).WithTx(tx)
	lookupStore := NewSQLMovieMaterializeStore(tx)

	results := make([]MovieApplyResult, 0, len(result.MovieMaterialize))
	for _, preview := range result.MovieMaterialize {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		applied := MovieApplyResult{
			Key:             preview.Key,
			Action:          preview.Action,
			Reason:          preview.Reason,
			Title:           preview.Title,
			Year:            preview.Year,
			ProviderID:      preview.ProviderID,
			MediaItemID:     preview.MediaItemID,
			MediaItemAction: preview.MediaItemAction,
			MovieRowAction:  preview.MovieRowAction,
		}

		if preview.Action == "blocked" {
			applied.Action = "skipped"
			applied.Skipped = true
			if applied.Reason == "" {
				applied.Reason = "materialization_blocked"
			}
			results = append(results, applied)
			emitMovieApplyResult(applied, emit)
			continue
		}

		meta, ok := metadataByKey[preview.Key]
		if !ok || meta.Detail == nil {
			applied.Action = "failed"
			applied.Error = "metadata detail is required for apply"
			results = append(results, applied)
			emitMovieApplyResult(applied, emit)
			return results, fmt.Errorf("apply %s: metadata detail is required", preview.Key)
		}
		if meta.Error != "" {
			applied.Action = "failed"
			applied.Error = meta.Error
			results = append(results, applied)
			emitMovieApplyResult(applied, emit)
			return results, fmt.Errorf("apply %s: metadata fetch failed: %s", preview.Key, meta.Error)
		}

		detail := movieApplyDetail(meta.Detail, preview)
		item, mediaAction, err := applyMovieMediaItem(ctx, q, lookupStore, lib.ID, preview, detail)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitMovieApplyResult(applied, emit)
			return results, fmt.Errorf("apply media item %s: %w", preview.Key, err)
		}
		applied.MediaItemID = item.ID
		applied.MediaItemAction = mediaAction
		if applied.Action != "repair" && mediaAction == "create_media_item" {
			applied.Action = "create"
		}

		if err := txMatcher.StoreEntityMetadata(ctx, item.ID, metadata.KindMovie, detail); err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitMovieApplyResult(applied, emit)
			return results, fmt.Errorf("apply movie row %s: %w", preview.Key, err)
		}
		if preview.MovieRowAction != "" {
			applied.MovieRowAction = preview.MovieRowAction
		}
		movieRow, err := q.GetMovieByMediaItemID(ctx, item.ID)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitMovieApplyResult(applied, emit)
			return results, fmt.Errorf("load movie row %s: %w", preview.Key, err)
		}

		if err := txMatcher.StoreRichMetadata(ctx, item.ID, detail); err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitMovieApplyResult(applied, emit)
			return results, fmt.Errorf("apply rich metadata %s: %w", preview.Key, err)
		}
		applied.RichMetadata = true

		fileCounts, err := applyMovieFiles(ctx, q, lib.ID, item.ID, movieRow.ID, preview, filesByRel)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitMovieApplyResult(applied, emit)
			return results, fmt.Errorf("apply files %s: %w", preview.Key, err)
		}
		applied.FilesCreated = fileCounts.created
		applied.FilesAttached = fileCounts.attached
		applied.FilesAlreadyAttached = fileCounts.alreadyAttached
		applied.FilesReassigned = fileCounts.reassigned

		match := matches[preview.Key]
		localAssets, err := applyMovieLocalAssets(ctx, q, item.ID, match, filesByRel)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitMovieApplyResult(applied, emit)
			return results, fmt.Errorf("apply local assets %s: %w", preview.Key, err)
		}
		applied.LocalAssets = localAssets

		localExtras, err := applyMovieLocalExtras(ctx, q, lib.ID, item.ID, match, filesByRel, result.Inventory)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitMovieApplyResult(applied, emit)
			return results, fmt.Errorf("apply local extras %s: %w", preview.Key, err)
		}
		applied.LocalExtras = localExtras

		remoteAssets, err := applyMovieRemoteAssets(ctx, q, item.ID, detail)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitMovieApplyResult(applied, emit)
			return results, fmt.Errorf("apply remote assets %s: %w", preview.Key, err)
		}
		applied.RemoteAssets = remoteAssets

		if err := markMovieApplyEnriched(ctx, q, item.ID); err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitMovieApplyResult(applied, emit)
			return results, fmt.Errorf("mark enriched %s: %w", preview.Key, err)
		}

		results = append(results, applied)
		emitMovieApplyResult(applied, emit)
	}

	if err := tx.Commit(ctx); err != nil {
		return results, fmt.Errorf("commit movie apply: %w", err)
	}
	emitMovieApplySummary(results, emit)
	return results, nil
}

func movieApplyDetail(detail *metadata.MediaDetail, preview MovieMaterializePreview) *metadata.MediaDetail {
	d := *detail
	d.ExternalIDs = mergeStringMaps(preview.ExternalIDs, detail.ExternalIDs)
	if d.Title == "" {
		d.Title = preview.Title
	}
	if d.Year == "" {
		d.Year = preview.Year
	}
	if d.SortTitle == "" {
		d.SortTitle = strings.ToLower(d.Title)
	}
	if d.ProviderKind == "" {
		d.ProviderKind = providerKindFromID(preview.ProviderID)
	}
	return &d
}

func applyMovieMediaItem(ctx context.Context, q *sqlc.Queries, lookupStore MovieMaterializeStore, libraryID int64, preview MovieMaterializePreview, detail *metadata.MediaDetail) (sqlc.MediaItemCard, string, error) {
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
		item, ok, err := findMaterializeMediaItem(ctx, lookupStore, libraryID, detail.ExternalIDs, detail.Title, detail.Year)
		if err != nil {
			return sqlc.MediaItemCard{}, "", err
		}
		if ok {
			existing = item
			found = true
		}
	}

	if found {
		updated, err := q.UpdateMediaItem(ctx, movieUpdateMediaItemParams(existing, detail))
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

	item, err := q.CreateMediaItem(ctx, movieCreateMediaItemParams(libraryID, detail))
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

func movieCreateMediaItemParams(libraryID int64, detail *metadata.MediaDetail) sqlc.CreateMediaItemParams {
	title := firstNonEmpty(detail.Title, "Untitled")
	return sqlc.CreateMediaItemParams{
		LibraryID:        libraryID,
		MediaType:        sqlc.MediaTypeMovie,
		Title:            title,
		SortTitle:        movieSortTitle(detail, title),
		Year:             detail.Year,
		Description:      detail.Description,
		PosterPath:       detail.PosterURL,
		BackdropPath:     detail.BackdropURL,
		ExternalIds:      mustJSONBytes(detail.ExternalIDs),
		Tagline:          detail.Tagline,
		OriginalTitle:    detail.OriginalTitle,
		OriginalLanguage: detail.OriginalLanguage,
		Status:           firstNonEmpty(detail.Status, detail.MovieStatus),
		ProviderKind:     firstNonEmpty(detail.ProviderKind, "heya"),
		HeyaSlug:         detail.HeyaSlug,
	}
}

func movieUpdateMediaItemParams(existing sqlc.MediaItemCard, detail *metadata.MediaDetail) sqlc.UpdateMediaItemParams {
	title := firstNonEmpty(detail.Title, existing.Title, "Untitled")
	return sqlc.UpdateMediaItemParams{
		ID:               existing.ID,
		Title:            title,
		SortTitle:        movieSortTitle(detail, title),
		Year:             firstNonEmpty(detail.Year, existing.Year),
		Description:      firstNonEmpty(detail.Description, existing.Description),
		PosterPath:       firstNonEmpty(detail.PosterURL, existing.PosterPath),
		BackdropPath:     firstNonEmpty(detail.BackdropURL, existing.BackdropPath),
		ExternalIds:      mustJSONBytes(mergeStringMaps(externalIDsFromMediaItem(existing), detail.ExternalIDs)),
		Tagline:          firstNonEmpty(detail.Tagline, existing.Tagline),
		OriginalTitle:    firstNonEmpty(detail.OriginalTitle, existing.OriginalTitle),
		OriginalLanguage: firstNonEmpty(detail.OriginalLanguage, existing.OriginalLanguage),
		Status:           firstNonEmpty(detail.Status, detail.MovieStatus, existing.Status),
		ProviderKind:     firstNonEmpty(detail.ProviderKind, existing.ProviderKind, "heya"),
		HeyaSlug:         firstNonEmpty(detail.HeyaSlug, existing.HeyaSlug),
	}
}

func movieSortTitle(detail *metadata.MediaDetail, title string) string {
	if detail.SortTitle != "" {
		return detail.SortTitle
	}
	return strings.ToLower(title)
}

func updateMovieSlug(ctx context.Context, q *sqlc.Queries, mediaItemID int64, title, year string) error {
	itemSlug := slug.GenerateUnique(ctx, title, year, mediaItemID, func(ctx context.Context, s string, excludeID int64) (bool, error) {
		return q.MediaItemSlugExists(ctx, sqlc.MediaItemSlugExistsParams{Slug: s, ID: excludeID})
	})
	return q.UpdateMediaItemSlug(ctx, sqlc.UpdateMediaItemSlugParams{ID: mediaItemID, Slug: itemSlug})
}

type movieFileApplyCounts struct {
	created         int
	attached        int
	alreadyAttached int
	reassigned      int
}

func applyMovieFiles(ctx context.Context, q *sqlc.Queries, libraryID, mediaItemID, movieID int64, preview MovieMaterializePreview, filesByRel map[string][]InventoryFile) (movieFileApplyCounts, error) {
	var counts movieFileApplyCounts
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
				ParseResult: movieLibraryFileParseResult(preview, action.RelPath),
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
		if err := replaceMovieLibraryFileLink(ctx, q, fileID, mediaItemID, movieID, relationType, partIndex); err != nil {
			return counts, err
		}
	}
	return counts, nil
}

func replaceMovieLibraryFileLink(ctx context.Context, q *sqlc.Queries, libraryFileID, mediaItemID, movieID int64, relationType string, partIndex pgtype.Int4) error {
	if err := q.DeleteLibraryFileLinksByFile(ctx, libraryFileID); err != nil {
		return err
	}
	_, err := q.CreateLibraryFileLink(ctx, sqlc.CreateLibraryFileLinkParams{
		LibraryFileID: libraryFileID,
		MediaItemID:   mediaItemID,
		MovieID:       pgInt8(movieID),
		RelationType:  relationType,
		PartIndex:     partIndex,
		Source:        "scanner_v2",
		Confidence:    1,
	})
	return err
}

func movieLibraryFileParseResult(preview MovieMaterializePreview, relPath string) []byte {
	parsed := parser.ParseStoragePath(relPath)
	return mustJSONBytes(map[string]any{
		"scanner":     "ingestv2",
		"parsed":      parsed,
		"match_key":   preview.Key,
		"provider_id": preview.ProviderID,
	})
}

func applyMovieLocalAssets(ctx context.Context, q *sqlc.Queries, mediaItemID int64, match MovieMatch, filesByRel map[string][]InventoryFile) (int, error) {
	created := 0
	sortOrders := map[sqlc.AssetType]int32{}
	for _, asset := range match.Assets {
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

func applyMovieRemoteAssets(ctx context.Context, q *sqlc.Queries, mediaItemID int64, detail *metadata.MediaDetail) (int, error) {
	created := 0
	if detail.PosterURL != "" {
		ok, err := createMovieAsset(ctx, q, sqlc.CreateMediaAssetParams{
			MediaItemID: mediaItemID,
			AssetType:   sqlc.AssetTypePoster,
			Source:      "remote",
			RemoteUrl:   detail.PosterURL,
			SortOrder:   0,
		})
		if err != nil {
			return created, err
		}
		if ok {
			created++
		}
	}
	if detail.BackdropURL != "" {
		ok, err := createMovieAsset(ctx, q, sqlc.CreateMediaAssetParams{
			MediaItemID: mediaItemID,
			AssetType:   sqlc.AssetTypeBackdrop,
			Source:      "remote",
			RemoteUrl:   detail.BackdropURL,
			SortOrder:   0,
		})
		if err != nil {
			return created, err
		}
		if ok {
			created++
		}
	}

	count := map[string]int{}
	if detail.PosterURL != "" {
		count["poster"] = 1
	}
	if detail.BackdropURL != "" {
		count["backdrop"] = 1
	}
	maxPerType := map[string]int{"backdrop": 5, "poster": 1, "logo": 1, "banner": 1, "clearart": 1, "thumb": 1, "disc": 1}
	sortOrder := int32(10)
	for _, art := range detail.Artwork {
		if art.URL == "" || art.URL == detail.PosterURL || art.URL == detail.BackdropURL {
			continue
		}
		assetType, ok := movieRemoteAssetType(art.AssetType)
		if !ok {
			continue
		}
		limit := maxPerType[art.AssetType]
		if limit == 0 {
			limit = 1
		}
		if count[art.AssetType] >= limit {
			continue
		}
		count[art.AssetType]++
		label := art.Language
		if label == "" {
			label = "extra"
		}
		ok, err := createMovieAsset(ctx, q, sqlc.CreateMediaAssetParams{
			MediaItemID: mediaItemID,
			AssetType:   assetType,
			Source:      "remote",
			RemoteUrl:   art.URL,
			Language:    art.Language,
			Label:       label,
			SortOrder:   sortOrder,
			Width:       int32(art.Width),
			Height:      int32(art.Height),
		})
		if err != nil {
			return created, err
		}
		if ok {
			created++
		}
		sortOrder++
	}
	return created, nil
}

func createMovieAsset(ctx context.Context, q *sqlc.Queries, params sqlc.CreateMediaAssetParams) (bool, error) {
	_, err := q.CreateMediaAsset(ctx, params)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func markMovieApplyEnriched(ctx context.Context, q *sqlc.Queries, mediaItemID int64) error {
	if err := q.MarkEnrichBaseDone(ctx, mediaItemID); err != nil {
		return err
	}
	if err := q.MarkEnrichPeopleDone(ctx, mediaItemID); err != nil {
		return err
	}
	if err := q.MarkEnrichExtrasDone(ctx, mediaItemID); err != nil {
		return err
	}
	if err := q.MarkEnrichImagesDone(ctx, mediaItemID); err != nil {
		return err
	}
	return q.MarkEnrichComplete(ctx, mediaItemID)
}

func singleInventoryFile(filesByRel map[string][]InventoryFile, relPath string) (InventoryFile, bool) {
	files := filesByRel[relPath]
	if len(files) != 1 {
		return InventoryFile{}, false
	}
	return files[0], true
}

func movieLocalAssetType(raw string) (sqlc.AssetType, bool) {
	switch raw {
	case "poster", "primary":
		return sqlc.AssetTypePoster, true
	case "fanart", "backdrop":
		return sqlc.AssetTypeBackdrop, true
	case "banner":
		return sqlc.AssetTypeBanner, true
	case "clearart", "art":
		return sqlc.AssetTypeArt, true
	case "clearlogo", "logo":
		return sqlc.AssetTypeLogo, true
	case "landscape", "thumb":
		return sqlc.AssetTypeThumb, true
	case "disc", "discart", "cdart":
		return sqlc.AssetTypeDisc, true
	default:
		return "", false
	}
}

func movieRemoteAssetType(raw string) (sqlc.AssetType, bool) {
	switch raw {
	case "poster":
		return sqlc.AssetTypePoster, true
	case "backdrop":
		return sqlc.AssetTypeBackdrop, true
	case "logo":
		return sqlc.AssetTypeLogo, true
	case "banner":
		return sqlc.AssetTypeBanner, true
	case "clearart":
		return sqlc.AssetTypeClearart, true
	case "thumb":
		return sqlc.AssetTypeThumb, true
	case "disc":
		return sqlc.AssetTypeDisc, true
	default:
		return "", false
	}
}

func providerKindFromID(providerID string) string {
	parts := strings.Split(providerID, ":")
	if len(parts) >= 4 && parts[0] == "heya" {
		return parts[2]
	}
	return "heya"
}

func pgInt8(id int64) pgtype.Int8 {
	return pgtype.Int8{Int64: id, Valid: id != 0}
}

func pgInt4(id int32) pgtype.Int4 {
	return pgtype.Int4{Int32: id, Valid: id != 0}
}

func emitMovieApplyResult(result MovieApplyResult, emit Emitter) {
	if emit == nil {
		return
	}
	event := "materialize.apply"
	severity := SeverityInfo
	if result.Action == "skipped" {
		event = "materialize.apply_skipped"
	} else if result.Action == "failed" {
		event = "materialize.apply_failed"
		severity = SeverityWarn
	}
	emit.Emit(Event{
		Event:    event,
		Severity: severity,
		Kind:     "movie",
		Reason:   result.Reason,
		Message:  result.Error,
		Data: map[string]any{
			"key":              result.Key,
			"title":            result.Title,
			"year":             result.Year,
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

func emitMovieApplySummary(results []MovieApplyResult, emit Emitter) {
	if emit == nil {
		return
	}
	emit.Emit(Event{Event: "materialize.apply_summary", Kind: "movie", Data: movieApplySummary(results)})
}

func movieApplySummary(results []MovieApplyResult) map[string]any {
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

func sortMovieApplyResults(items []MovieApplyResult) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Action == items[j].Action {
			if items[i].Title == items[j].Title {
				return items[i].Year < items[j].Year
			}
			return items[i].Title < items[j].Title
		}
		return movieApplyActionRank(items[i].Action) < movieApplyActionRank(items[j].Action)
	})
}

func movieApplyActionRank(action string) int {
	switch action {
	case "failed":
		return 0
	case "skipped":
		return 1
	case "repair":
		return 2
	case "create":
		return 3
	case "update":
		return 4
	default:
		return 5
	}
}
