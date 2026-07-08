package scanner

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
	"github.com/karbowiak/heya/internal/mediatype"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/parser"
)

type TVApplyResult struct {
	Key                  string `json:"key"`
	Action               string `json:"action"`
	Reason               string `json:"reason,omitempty"`
	Title                string `json:"title"`
	Year                 string `json:"year,omitempty"`
	ProviderID           string `json:"provider_id,omitempty"`
	MediaItemID          int64  `json:"media_item_id,omitempty"`
	TVSeriesID           int64  `json:"tv_series_id,omitempty"`
	MediaItemAction      string `json:"media_item_action,omitempty"`
	TVSeriesAction       string `json:"tv_series_action,omitempty"`
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

func ApplyTVMaterialization(ctx context.Context, lib sqlc.Library, result Result, db *pgxpool.Pool, emit Emitter) ([]TVApplyResult, error) {
	domain := tvLikeDomainForMediaType(lib.MediaType)
	if db == nil {
		return nil, fmt.Errorf("%s apply db is required", domain)
	}
	if !mediatype.IsTVLike(lib.MediaType) {
		return nil, fmt.Errorf("TV apply only supports TV-like libraries (got %q)", lib.MediaType)
	}
	if len(result.TVMaterialize) == 0 {
		return nil, nil
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
		metadataByKey[preview.Key] = preview
		for _, key := range keys {
			metadataByKey[key] = preview
		}
	}
	filesByRel := inventoryFilesByRel(result.Inventory)
	plansByRel := tvPlansByRelPath(result.TVPlans)
	useLocalData := libraryUsesLocalData(lib)

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin TV apply: %w", err)
	}
	defer tx.Rollback(ctx)

	q := sqlc.New(tx)
	txMatcher := matcher.New(db, matcher.MatchOptions{}, nil, nil).WithTx(tx)
	lookupStore := NewSQLTVMaterializeStore(tx)

	results := make([]TVApplyResult, 0, len(result.TVMaterialize))
	for _, preview := range result.TVMaterialize {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		applied := TVApplyResult{
			Key:             preview.Key,
			Action:          preview.Action,
			Reason:          preview.Reason,
			Title:           preview.Title,
			Year:            preview.Year,
			ProviderID:      preview.ProviderID,
			MediaItemID:     preview.MediaItemID,
			TVSeriesID:      preview.TVSeriesID,
			MediaItemAction: preview.MediaItemAction,
			TVSeriesAction:  preview.TVSeriesAction,
		}

		if preview.Action == "blocked" {
			applied.Action = "skipped"
			applied.Skipped = true
			if applied.Reason == "" {
				applied.Reason = "materialization_blocked"
			}
			results = append(results, applied)
			emitTVApplyResult(applied, domain, emit)
			continue
		}

		meta, ok := metadataByKey[preview.Key]
		if !ok && len(preview.Keys) > 0 {
			meta = metadataByKey[preview.Keys[0]]
			ok = meta.ProviderID != ""
		}
		if !ok || meta.Detail == nil {
			applied.Action = "skipped"
			applied.Skipped = true
			applied.Reason = "metadata_detail_missing"
			applied.Error = "metadata detail is required for apply"
			results = append(results, applied)
			emitTVApplyResult(applied, domain, emit)
			continue
		}
		if meta.Error != "" {
			applied.Action = "failed"
			applied.Error = meta.Error
			results = append(results, applied)
			emitTVApplyResult(applied, domain, emit)
			return results, fmt.Errorf("apply %s: metadata fetch failed: %s", preview.Key, meta.Error)
		}

		detail := tvApplyDetail(meta.Detail, preview)
		item, mediaAction, err := applyTVMediaItem(ctx, q, lookupStore, lib.ID, lib.MediaType, preview, detail)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitTVApplyResult(applied, domain, emit)
			return results, fmt.Errorf("apply TV media item %s: %w", preview.Key, err)
		}
		applied.MediaItemID = item.ID
		applied.MediaItemAction = mediaAction
		if applied.Action != "repair" && mediaAction == "create_media_item" {
			applied.Action = "create"
		}

		if err := txMatcher.StoreEntityMetadata(ctx, item.ID, metadata.KindTV, detail); err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitTVApplyResult(applied, domain, emit)
			return results, fmt.Errorf("apply TV rows %s: %w", preview.Key, err)
		}
		if preview.TVSeriesAction != "" {
			applied.TVSeriesAction = preview.TVSeriesAction
		}
		series, err := q.GetTVSeriesByMediaItemID(ctx, item.ID)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitTVApplyResult(applied, domain, emit)
			return results, fmt.Errorf("load TV series %s: %w", preview.Key, err)
		}
		applied.TVSeriesID = series.ID

		episodeLinks, err := loadTVEpisodeLinkIndex(ctx, q, item.ID)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitTVApplyResult(applied, domain, emit)
			return results, fmt.Errorf("load TV episode links %s: %w", preview.Key, err)
		}
		fileCounts, err := applyTVFiles(ctx, q, lib.ID, item.ID, preview, filesByRel, plansByRel, episodeLinks)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitTVApplyResult(applied, domain, emit)
			return results, fmt.Errorf("apply TV files %s: %w", preview.Key, err)
		}
		applied.FilesCreated = fileCounts.created
		applied.FilesAttached = fileCounts.attached
		applied.FilesAlreadyAttached = fileCounts.alreadyAttached
		applied.FilesReassigned = fileCounts.reassigned

		localMatch := combinedTVMatchForPreview(preview, matches)
		if useLocalData {
			localAssets, err := applyTVLocalAssets(ctx, q, item.ID, localMatch, filesByRel)
			if err != nil {
				applied.Action = "failed"
				applied.Error = err.Error()
				results = append(results, applied)
				emitTVApplyResult(applied, domain, emit)
				return results, fmt.Errorf("apply TV local assets %s: %w", preview.Key, err)
			}
			applied.LocalAssets = localAssets
		}

		localExtras, err := applyTVLocalExtras(ctx, q, lib.ID, item.ID, localMatch, filesByRel, result.Inventory)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitTVApplyResult(applied, domain, emit)
			return results, fmt.Errorf("apply TV local extras %s: %w", preview.Key, err)
		}
		applied.LocalExtras = localExtras

		remoteAssets, err := applyMovieRemoteAssets(ctx, q, item.ID, detail)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitTVApplyResult(applied, domain, emit)
			return results, fmt.Errorf("apply TV remote assets %s: %w", preview.Key, err)
		}
		applied.RemoteAssets = remoteAssets

		if _, err := matcher.ReconcileAbsoluteEpisodes(ctx, q, item.ID); err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitTVApplyResult(applied, domain, emit)
			return results, fmt.Errorf("reconcile absolute episodes %s: %w", preview.Key, err)
		}
		if err := markTVApplyCoreEnriched(ctx, q, item.ID); err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitTVApplyResult(applied, domain, emit)
			return results, fmt.Errorf("mark TV enriched %s: %w", preview.Key, err)
		}

		results = append(results, applied)
		emitTVApplyResult(applied, domain, emit)
	}

	if err := tx.Commit(ctx); err != nil {
		return results, fmt.Errorf("commit TV apply: %w", err)
	}
	sortTVApplyResults(results)
	emitTVApplySummary(results, domain, emit)
	return results, nil
}

func tvApplyDetail(detail *metadata.MediaDetail, preview TVMaterializePreview) *metadata.MediaDetail {
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

func applyTVMediaItem(ctx context.Context, q *sqlc.Queries, lookupStore TVMaterializeStore, libraryID int64, mediaType sqlc.MediaType, preview TVMaterializePreview, detail *metadata.MediaDetail) (sqlc.MediaItemCard, string, error) {
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
		item, ok, err := findTVMaterializeMediaItem(ctx, lookupStore, libraryID, mediaType, detail.ExternalIDs, detail.Title, detail.Year)
		if err != nil {
			return sqlc.MediaItemCard{}, "", err
		}
		if ok {
			existing = item
			found = true
		}
	}

	if found {
		if !mediatype.IsTVLike(existing.MediaType) {
			return sqlc.MediaItemCard{}, "", fmt.Errorf("existing media item %d has non-TV media type %s", existing.ID, existing.MediaType)
		}
		updated, err := q.UpdateMediaItem(ctx, tvUpdateMediaItemParams(existing, detail))
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

	item, err := q.CreateMediaItem(ctx, tvCreateMediaItemParams(libraryID, mediaType, detail))
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

func tvCreateMediaItemParams(libraryID int64, mediaType sqlc.MediaType, detail *metadata.MediaDetail) sqlc.CreateMediaItemParams {
	title := firstNonEmpty(detail.Title, "Untitled")
	return sqlc.CreateMediaItemParams{
		LibraryID:        libraryID,
		MediaType:        mediaType,
		Title:            title,
		SortTitle:        tvSortTitle(detail, title),
		Year:             detail.Year,
		Description:      detail.Description,
		PosterPath:       detail.PosterURL,
		BackdropPath:     detail.BackdropURL,
		ExternalIds:      mustJSONBytes(detail.ExternalIDs),
		OriginalTitle:    firstNonEmpty(detail.OriginalTitle, detail.OriginalName),
		OriginalLanguage: detail.OriginalLanguage,
		Status:           detail.Status,
		ProviderKind:     firstNonEmpty(detail.ProviderKind, "heya"),
		HeyaSlug:         detail.HeyaSlug,
	}
}

func tvUpdateMediaItemParams(existing sqlc.MediaItemCard, detail *metadata.MediaDetail) sqlc.UpdateMediaItemParams {
	title := firstNonEmpty(detail.Title, existing.Title, "Untitled")
	return sqlc.UpdateMediaItemParams{
		ID:               existing.ID,
		Title:            title,
		SortTitle:        tvSortTitle(detail, title),
		Year:             firstNonEmpty(detail.Year, existing.Year),
		Description:      firstNonEmpty(detail.Description, existing.Description),
		PosterPath:       firstNonEmpty(detail.PosterURL, existing.PosterPath),
		BackdropPath:     firstNonEmpty(detail.BackdropURL, existing.BackdropPath),
		ExternalIds:      mustJSONBytes(mergeStringMaps(externalIDsFromMediaItem(existing), detail.ExternalIDs)),
		Tagline:          existing.Tagline,
		OriginalTitle:    firstNonEmpty(detail.OriginalTitle, detail.OriginalName, existing.OriginalTitle),
		OriginalLanguage: firstNonEmpty(detail.OriginalLanguage, existing.OriginalLanguage),
		Status:           firstNonEmpty(detail.Status, existing.Status),
		ProviderKind:     firstNonEmpty(detail.ProviderKind, existing.ProviderKind, "heya"),
		HeyaSlug:         firstNonEmpty(detail.HeyaSlug, existing.HeyaSlug),
	}
}

func tvSortTitle(detail *metadata.MediaDetail, title string) string {
	if detail.SortTitle != "" {
		return detail.SortTitle
	}
	return strings.ToLower(title)
}

type tvFileApplyCounts = movieFileApplyCounts

func applyTVFiles(ctx context.Context, q *sqlc.Queries, libraryID, mediaItemID int64, preview TVMaterializePreview, filesByRel map[string][]InventoryFile, plansByRel map[string]TVPlan, episodeLinks tvEpisodeLinkIndex) (tvFileApplyCounts, error) {
	var counts tvFileApplyCounts
	for _, action := range preview.FileActions {
		if action.Action == "blocked" {
			return counts, fmt.Errorf("%s blocked: %s", action.RelPath, action.Reason)
		}

		fileID := action.FileID
		plan := plansByRel[action.RelPath]
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
				ParseResult: tvLibraryFileParseResult(preview, plan, action.RelPath),
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
		if err := replaceTVLibraryFileLinks(ctx, q, fileID, mediaItemID, plan, episodeLinks); err != nil {
			return counts, err
		}
	}
	return counts, nil
}

type tvEpisodeNumberKey struct {
	season  int32
	episode int32
}

type tvEpisodeLinkIndex struct {
	byNumber   map[tvEpisodeNumberKey]int64
	byAbsolute map[int32]int64
}

type tvEpisodeLinkTarget struct {
	episodeID      int64
	seasonNumber   int32
	episodeNumber  int32
	absoluteNumber int32
}

func loadTVEpisodeLinkIndex(ctx context.Context, q *sqlc.Queries, mediaItemID int64) (tvEpisodeLinkIndex, error) {
	rows, err := q.ListTVEpisodeLinkTargetsByMediaItem(ctx, mediaItemID)
	if err != nil {
		return tvEpisodeLinkIndex{}, err
	}
	index := tvEpisodeLinkIndex{
		byNumber:   map[tvEpisodeNumberKey]int64{},
		byAbsolute: map[int32]int64{},
	}
	for _, row := range rows {
		index.byNumber[tvEpisodeNumberKey{season: row.SeasonNumber, episode: row.EpisodeNumber}] = row.EpisodeID
		if row.AbsoluteNumber > 0 {
			index.byAbsolute[row.AbsoluteNumber] = row.EpisodeID
		}
	}
	return index, nil
}

func replaceTVLibraryFileLinks(ctx context.Context, q *sqlc.Queries, libraryFileID, mediaItemID int64, plan TVPlan, episodeLinks tvEpisodeLinkIndex) error {
	if err := q.DeleteLibraryFileLinksByFile(ctx, libraryFileID); err != nil {
		return err
	}

	targets := tvEpisodeLinkTargetsForPlan(plan, episodeLinks)
	for _, target := range targets {
		if err := createTVLibraryFileLink(ctx, q, libraryFileID, mediaItemID, target.episodeID, target.seasonNumber, target.episodeNumber, target.absoluteNumber, plan.Title); err != nil {
			return err
		}
	}

	if len(targets) > 0 {
		return nil
	}
	_, err := q.CreateLibraryFileLink(ctx, sqlc.CreateLibraryFileLinkParams{
		LibraryFileID: libraryFileID,
		MediaItemID:   mediaItemID,
		RelationType:  "primary",
		Title:         plan.Title,
		Source:        "scanner",
		Confidence:    1,
	})
	return err
}

func tvEpisodeLinkTargetsForPlan(plan TVPlan, episodeLinks tvEpisodeLinkIndex) []tvEpisodeLinkTarget {
	targets := make([]tvEpisodeLinkTarget, 0, len(plan.Episodes)+len(plan.AbsoluteEpisodes))
	pairedAbsolute := len(plan.Episodes) > 0 && len(plan.AbsoluteEpisodes) == len(plan.Episodes)
	for idx, episode := range plan.Episodes {
		seasonNumber := int32(plan.Season)
		episodeNumber := int32(episode)
		var absoluteNumber int32
		if pairedAbsolute {
			absoluteNumber = int32(plan.AbsoluteEpisodes[idx])
		}
		episodeID := episodeLinks.byNumber[tvEpisodeNumberKey{season: seasonNumber, episode: episodeNumber}]
		if episodeID == 0 && absoluteNumber > 0 {
			episodeID = episodeLinks.byAbsolute[absoluteNumber]
		}
		targets = append(targets, tvEpisodeLinkTarget{
			episodeID:      episodeID,
			seasonNumber:   seasonNumber,
			episodeNumber:  episodeNumber,
			absoluteNumber: absoluteNumber,
		})
	}
	if len(targets) > 0 {
		return targets
	}
	for _, absolute := range plan.AbsoluteEpisodes {
		absoluteNumber := int32(absolute)
		targets = append(targets, tvEpisodeLinkTarget{
			episodeID:      episodeLinks.byAbsolute[absoluteNumber],
			absoluteNumber: absoluteNumber,
		})
	}
	return targets
}

func createTVLibraryFileLink(ctx context.Context, q *sqlc.Queries, libraryFileID, mediaItemID, episodeID int64, seasonNumber, episodeNumber, absoluteNumber int32, title string) error {
	_, err := q.CreateLibraryFileLink(ctx, sqlc.CreateLibraryFileLinkParams{
		LibraryFileID:  libraryFileID,
		MediaItemID:    mediaItemID,
		TvEpisodeID:    pgInt8(episodeID),
		RelationType:   "episode",
		SeasonNumber:   pgtype.Int4{Int32: seasonNumber, Valid: episodeNumber > 0},
		EpisodeNumber:  pgtype.Int4{Int32: episodeNumber, Valid: episodeNumber > 0},
		AbsoluteNumber: pgtype.Int4{Int32: absoluteNumber, Valid: absoluteNumber > 0},
		Title:          title,
		Source:         "scanner",
		Confidence:     1,
	})
	return err
}

func tvLibraryFileParseResult(preview TVMaterializePreview, plan TVPlan, relPath string) []byte {
	parsed := parser.ParseStoragePath(relPath)
	if parsed.Release == nil {
		parsed.Release = &parser.SceneReleaseParse{Media: parser.MediaVideo}
	}
	parsed.Release.Title = firstNonEmpty(plan.Title, preview.Title, parsed.Release.Title)
	parsed.Release.Year = firstNonEmpty(plan.Year, preview.Year, parsed.Release.Year)
	parsed.Release.IsTv = true
	parsed.Release.Seasons = uniqueInts(append([]int{}, plan.Season))
	if plan.Season == 0 && len(plan.Episodes) == 0 {
		parsed.Release.Seasons = nil
	}
	parsed.Release.Episodes = uniqueInts(append([]int{}, plan.Episodes...))
	parsed.Release.AbsoluteEpisodes = uniqueInts(append([]int{}, plan.AbsoluteEpisodes...))
	return mustJSONBytes(map[string]any{
		"scanner":     "scanner",
		"parsed":      parsed,
		"match_key":   preview.Key,
		"provider_id": preview.ProviderID,
	})
}

func tvPlansByRelPath(plans []TVPlan) map[string]TVPlan {
	out := map[string]TVPlan{}
	for _, plan := range plans {
		for _, relPath := range plan.Files {
			out[relPath] = plan
		}
	}
	return out
}

func combinedTVMatchForPreview(preview TVMaterializePreview, matches map[string]TVMatch) TVMatch {
	keys := preview.Keys
	if len(keys) == 0 && preview.Key != "" {
		keys = strings.Split(preview.Key, ",")
	}
	localMatches := make([]TVMatch, 0, len(keys))
	for _, key := range keys {
		localMatches = append(localMatches, matches[key])
	}
	return combineTVFetchMatches(localMatches)
}

func applyTVLocalAssets(ctx context.Context, q *sqlc.Queries, mediaItemID int64, match TVMatch, filesByRel map[string][]InventoryFile) (int, error) {
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

func markTVApplyEnriched(ctx context.Context, q *sqlc.Queries, mediaItemID int64) error {
	if err := q.MarkEnrichBaseDone(ctx, mediaItemID); err != nil {
		return err
	}
	if err := q.MarkEnrichStructureDone(ctx, mediaItemID); err != nil {
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

func markTVApplyCoreEnriched(ctx context.Context, q *sqlc.Queries, mediaItemID int64) error {
	if err := q.MarkEnrichBaseDone(ctx, mediaItemID); err != nil {
		return err
	}
	if err := q.MarkEnrichStructureDone(ctx, mediaItemID); err != nil {
		return err
	}
	if err := q.MarkEnrichImagesDone(ctx, mediaItemID); err != nil {
		return err
	}
	return q.MarkEnrichPartial(ctx, mediaItemID)
}

func emitTVApplyResult(result TVApplyResult, domain string, emit Emitter) {
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
		Kind:     domain,
		Reason:   result.Reason,
		Message:  result.Error,
		Data: map[string]any{
			"key":              result.Key,
			"title":            result.Title,
			"year":             result.Year,
			"action":           result.Action,
			"media_item_id":    result.MediaItemID,
			"tv_series_id":     result.TVSeriesID,
			"files_created":    result.FilesCreated,
			"files_attached":   result.FilesAttached,
			"files_reassigned": result.FilesReassigned,
			"local_assets":     result.LocalAssets,
			"remote_assets":    result.RemoteAssets,
		},
	})
}

func emitTVApplySummary(results []TVApplyResult, domain string, emit Emitter) {
	if emit == nil {
		return
	}
	emit.Emit(Event{Event: "materialize.apply_summary", Kind: domain, Data: tvApplySummary(results)})
}

func tvApplySummary(results []TVApplyResult) map[string]any {
	summary := map[string]any{"plans": len(results)}
	for _, result := range results {
		summary[result.Action] = intFromAny(summary[result.Action]) + 1
		if result.MediaItemAction != "" {
			summary[result.MediaItemAction] = intFromAny(summary[result.MediaItemAction]) + 1
		}
		if result.TVSeriesAction != "" {
			summary[result.TVSeriesAction] = intFromAny(summary[result.TVSeriesAction]) + 1
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

func sortTVApplyResults(items []TVApplyResult) {
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
