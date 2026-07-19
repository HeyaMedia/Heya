package scanner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/slug"
)

type MusicApplyResult struct {
	Key                  string `json:"key"`
	Action               string `json:"action"`
	Reason               string `json:"reason,omitempty"`
	Artist               string `json:"artist"`
	ProviderID           string `json:"provider_id,omitempty"`
	MediaItemID          int64  `json:"media_item_id,omitempty"`
	ArtistID             int64  `json:"artist_id,omitempty"`
	MediaItemAction      string `json:"media_item_action,omitempty"`
	ArtistRowAction      string `json:"artist_row_action,omitempty"`
	AlbumsCreated        int    `json:"albums_created,omitempty"`
	AlbumsUpdated        int    `json:"albums_updated,omitempty"`
	TracksCreated        int    `json:"tracks_created,omitempty"`
	TracksUpdated        int    `json:"tracks_updated,omitempty"`
	TrackFilesCreated    int    `json:"track_files_created,omitempty"`
	TrackFilesUpdated    int    `json:"track_files_updated,omitempty"`
	FilesCreated         int    `json:"files_created,omitempty"`
	FilesAttached        int    `json:"files_attached,omitempty"`
	FilesAlreadyAttached int    `json:"files_already_attached,omitempty"`
	FilesReassigned      int    `json:"files_reassigned,omitempty"`
	Skipped              bool   `json:"skipped,omitempty"`
	Error                string `json:"error,omitempty"`
}

func ApplyMusicMaterialization(ctx context.Context, lib sqlc.Library, result Result, db *pgxpool.Pool, emit Emitter) ([]MusicApplyResult, error) {
	if db == nil {
		return nil, fmt.Errorf("music apply db is required")
	}
	if lib.MediaType != sqlc.MediaTypeMusic {
		return nil, fmt.Errorf("music apply only supports music libraries (got %q)", lib.MediaType)
	}
	if len(result.MusicMaterialize) == 0 {
		return nil, nil
	}

	metadataByKey := map[string]MusicFetchPreview{}
	for _, preview := range result.MusicMetadata {
		metadataByKey[preview.Key] = preview
	}
	tracksByRel := musicTracksByRel(result.MusicTracks)
	filesByRel := inventoryFilesByRel(result.Inventory)

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin music apply: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := runScannerApplyPreflightGuard(ctx); err != nil {
		return nil, fmt.Errorf("validate music sources before apply: %w", err)
	}

	q := sqlc.New(tx)
	lookupStore := NewSQLMusicMaterializeStore(tx)

	results := make([]MusicApplyResult, 0, len(result.MusicMaterialize))
	for _, preview := range result.MusicMaterialize {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		applied := MusicApplyResult{
			Key:             preview.Key,
			Action:          preview.Action,
			Reason:          preview.Reason,
			Artist:          preview.Artist,
			ProviderID:      preview.ProviderID,
			MediaItemID:     preview.MediaItemID,
			ArtistID:        preview.ArtistID,
			MediaItemAction: preview.MediaItemAction,
			ArtistRowAction: preview.ArtistRowAction,
		}
		if preview.Action == "blocked" {
			applied.Action = "skipped"
			applied.Skipped = true
			if applied.Reason == "" {
				applied.Reason = "materialization_blocked"
			}
			results = append(results, applied)
			emitMusicApplyResult(applied, emit)
			continue
		}

		meta := metadataByKey[preview.Key]

		detail := musicApplyDetail(meta.Detail, preview)
		item, mediaAction, err := applyMusicMediaItem(ctx, q, lookupStore, lib.ID, preview, detail)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitMusicApplyResult(applied, emit)
			return results, fmt.Errorf("apply music media item %s: %w", preview.Key, err)
		}
		applied.MediaItemID = item.ID
		applied.MediaItemAction = mediaAction
		if applied.Action != "repair" && mediaAction == "create_media_item" {
			applied.Action = "create"
		}

		artist, artistAction, err := applyMusicArtist(ctx, q, item.ID, preview, detail)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitMusicApplyResult(applied, emit)
			return results, fmt.Errorf("apply music artist %s: %w", preview.Key, err)
		}
		applied.ArtistID = artist.ID
		applied.ArtistRowAction = artistAction
		if artist.MediaItemID != item.ID {
			canonical, canonicalAction, err := applyMusicCanonicalArtistMediaItem(ctx, q, item, mediaAction, artist, detail)
			if err != nil {
				applied.Action = "failed"
				applied.Error = err.Error()
				results = append(results, applied)
				emitMusicApplyResult(applied, emit)
				return results, fmt.Errorf("adopt music artist media item %s: %w", preview.Key, err)
			}
			item = canonical
			mediaAction = canonicalAction
			applied.MediaItemID = item.ID
			applied.MediaItemAction = mediaAction
		}
		if err := bindCanonicalMetadata(ctx, q, "media_item", item.ID, detail); err != nil {
			return results, fmt.Errorf("bind music item %s to canonical metadata: %w", preview.Key, err)
		}
		if err := bindCanonicalMetadata(ctx, q, "artist", artist.ID, detail); err != nil {
			return results, fmt.Errorf("bind artist %s to canonical metadata: %w", preview.Key, err)
		}

		counts, err := applyMusicAlbumsTracksAndFiles(ctx, q, lib.ID, item.ID, artist.ID, preview, meta, tracksByRel, filesByRel)
		if err != nil {
			applied.Action = "failed"
			applied.Error = err.Error()
			results = append(results, applied)
			emitMusicApplyResult(applied, emit)
			return results, fmt.Errorf("apply music tracks %s: %w", preview.Key, err)
		}
		applied.AlbumsCreated = counts.albumsCreated
		applied.AlbumsUpdated = counts.albumsUpdated
		applied.TracksCreated = counts.tracksCreated
		applied.TracksUpdated = counts.tracksUpdated
		applied.TrackFilesCreated = counts.trackFilesCreated
		applied.TrackFilesUpdated = counts.trackFilesUpdated
		applied.FilesCreated = counts.filesCreated
		applied.FilesAttached = counts.filesAttached
		applied.FilesAlreadyAttached = counts.filesAlreadyAttached
		applied.FilesReassigned = counts.filesReassigned

		if musicApplyHasRemoteDetail(meta) {
			if err := q.MarkMetadataRefreshed(ctx, item.ID); err != nil {
				applied.Action = "failed"
				applied.Error = err.Error()
				results = append(results, applied)
				emitMusicApplyResult(applied, emit)
				return results, fmt.Errorf("mark music refreshed %s: %w", preview.Key, err)
			}
		}

		results = append(results, applied)
		emitMusicApplyResult(applied, emit)
	}

	if err := runScannerApplyCommitGuard(ctx, tx); err != nil {
		return results, fmt.Errorf("validate music sources before commit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return results, fmt.Errorf("commit music apply: %w", err)
	}
	emitMusicApplySummary(results, emit)
	return results, nil
}

func musicApplyDetail(detail *metadata.MediaDetail, preview MusicMaterializePreview) *metadata.MediaDetail {
	d := metadata.MediaDetail{ProviderKind: "local"}
	if detail != nil {
		d = *detail
	}
	d.ExternalIDs = mergeStringMaps(preview.ExternalIDs, d.ExternalIDs, musicProviderIDExternalID(preview.ProviderID))
	if d.ArtistName == "" {
		d.ArtistName = firstNonEmpty(preview.Artist, d.Title)
	}
	if d.Title == "" {
		d.Title = d.ArtistName
	}
	if d.ArtistSortName == "" {
		d.ArtistSortName = preview.SortName
	}
	if d.ProviderKind == "" {
		d.ProviderKind = firstNonEmpty(providerKindFromID(preview.ProviderID), "local")
	}
	return &d
}

func musicApplyHasRemoteDetail(meta MusicFetchPreview) bool {
	return meta.Error == "" && meta.Detail != nil
}

func musicDetailHasRemoteMetadata(detail *metadata.MediaDetail) bool {
	return detail != nil && detail.ProviderKind != "local"
}

func applyMusicMediaItem(ctx context.Context, q *sqlc.Queries, lookupStore MusicMaterializeStore, libraryID int64, preview MusicMaterializePreview, detail *metadata.MediaDetail) (sqlc.MediaItemCard, string, error) {
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
	// Materialization previews are durable artifacts and may predate stronger
	// identity rules. Never let a stale same-name target force a contradictory
	// MBID overwrite; resolve again and create a distinct artist when needed.
	if found && !musicMediaItemIdentityCompatible(existing, detail.ExternalIDs) {
		existing = sqlc.MediaItemCard{}
		found = false
	}
	if !found {
		item, ok, err := findMusicMaterializeMediaItem(ctx, lookupStore, libraryID, detail.ExternalIDs, firstNonEmpty(detail.ArtistName, detail.Title))
		if err != nil {
			return sqlc.MediaItemCard{}, "", err
		}
		if ok {
			existing = item
			found = true
		}
	}
	if found && !musicMediaItemIdentityCompatible(existing, detail.ExternalIDs) {
		existing = sqlc.MediaItemCard{}
		found = false
	}
	if found {
		updated, err := q.UpdateMediaItem(ctx, musicUpdateMediaItemParams(existing, detail))
		if err != nil {
			return sqlc.MediaItemCard{}, "", err
		}
		if updated.Slug == "" {
			if err := updateMusicArtistSlug(ctx, q, updated.ID, updated.Title); err != nil {
				return sqlc.MediaItemCard{}, "", err
			}
		}
		if err := q.MarkMatched(ctx, updated.ID); err != nil {
			return sqlc.MediaItemCard{}, "", err
		}
		return updated, "update_media_item", nil
	}

	item, err := q.CreateMediaItem(ctx, musicCreateMediaItemParams(libraryID, detail))
	if err != nil {
		return sqlc.MediaItemCard{}, "", err
	}
	if err := updateMusicArtistSlug(ctx, q, item.ID, item.Title); err != nil {
		return sqlc.MediaItemCard{}, "", err
	}
	if err := q.MarkMatched(ctx, item.ID); err != nil {
		return sqlc.MediaItemCard{}, "", err
	}
	return item, "create_media_item", nil
}

func musicCreateMediaItemParams(libraryID int64, detail *metadata.MediaDetail) sqlc.CreateMediaItemParams {
	title := firstNonEmpty(detail.ArtistName, detail.Title, "Unknown Artist")
	return sqlc.CreateMediaItemParams{
		LibraryID:    libraryID,
		MediaType:    sqlc.MediaTypeMusic,
		Title:        title,
		SortTitle:    musicArtistSortTitle(detail, title),
		Description:  firstNonEmpty(detail.ArtistBio, detail.Description),
		PosterPath:   firstNonEmpty(detail.PosterURL, firstMusicArtworkURL(detail.ArtistImages)),
		ExternalIds:  mustJSONBytes(detail.ExternalIDs),
		ProviderKind: firstNonEmpty(detail.ProviderKind, "heya"),
		HeyaSlug:     detail.HeyaSlug,
	}
}

func musicUpdateMediaItemParams(existing sqlc.MediaItemCard, detail *metadata.MediaDetail) sqlc.UpdateMediaItemParams {
	title := firstNonEmpty(detail.ArtistName, detail.Title, existing.Title, "Unknown Artist")
	return sqlc.UpdateMediaItemParams{
		ID:               existing.ID,
		Title:            title,
		SortTitle:        musicArtistSortTitle(detail, title),
		Year:             existing.Year,
		Description:      firstNonEmpty(detail.ArtistBio, detail.Description, existing.Description),
		PosterPath:       firstNonEmpty(detail.PosterURL, firstMusicArtworkURL(detail.ArtistImages), existing.PosterPath),
		BackdropPath:     existing.BackdropPath,
		ExternalIds:      mustJSONBytes(mergeStringMaps(externalIDsFromMediaItem(existing), detail.ExternalIDs)),
		Tagline:          existing.Tagline,
		OriginalTitle:    existing.OriginalTitle,
		OriginalLanguage: existing.OriginalLanguage,
		Status:           existing.Status,
		ProviderKind:     firstNonEmpty(detail.ProviderKind, existing.ProviderKind, "heya"),
		HeyaSlug:         firstNonEmpty(detail.HeyaSlug, existing.HeyaSlug),
	}
}

func musicArtistSortTitle(detail *metadata.MediaDetail, title string) string {
	return firstNonEmpty(detail.ArtistSortName, detail.SortTitle, strings.ToLower(title))
}

func updateMusicArtistSlug(ctx context.Context, q *sqlc.Queries, mediaItemID int64, title string) error {
	itemSlug := slug.GenerateUnique(ctx, title, "", mediaItemID, func(ctx context.Context, s string, excludeID int64) (bool, error) {
		return q.MediaItemSlugExists(ctx, sqlc.MediaItemSlugExistsParams{Slug: s, ID: excludeID})
	})
	return q.UpdateMediaItemSlug(ctx, sqlc.UpdateMediaItemSlugParams{ID: mediaItemID, Slug: itemSlug})
}

func applyMusicArtist(ctx context.Context, q *sqlc.Queries, mediaItemID int64, preview MusicMaterializePreview, detail *metadata.MediaDetail) (sqlc.Artist, string, error) {
	name := firstNonEmpty(detail.ArtistName, detail.Title, preview.Artist, "Unknown Artist")
	mbid := firstNonEmpty(detail.ExternalIDs["mbid"], detail.ExternalIDs["musicbrainz_artist"])
	sortName := firstNonEmpty(detail.ArtistSortName, detail.SortTitle, name)
	bio := firstNonEmpty(detail.ArtistBio, detail.Description)
	disambig := detail.ArtistDisambiguation

	existing, err := q.GetArtistByMediaItemID(ctx, mediaItemID)
	if err == nil {
		if musicArtistMBIDContradicts(existing, mbid) {
			return sqlc.Artist{}, "", fmt.Errorf("refusing to overwrite artist %d MusicBrainz ID %q with %q", existing.ID, existing.MusicbrainzID, mbid)
		}
		updated, err := q.UpdateArtist(ctx, sqlc.UpdateArtistParams{
			ID:             existing.ID,
			MusicbrainzID:  firstNonEmpty(mbid, existing.MusicbrainzID),
			Name:           name,
			SortName:       sortName,
			Disambiguation: firstNonEmpty(disambig, existing.Disambiguation),
			Biography:      firstNonEmpty(bio, existing.Biography),
		})
		if err != nil {
			return sqlc.Artist{}, "", err
		}
		if musicDetailHasRemoteMetadata(detail) {
			_ = applyMusicArtistExtended(ctx, q, updated.ID, detail)
		}
		return updated, "update_artist_row", nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Artist{}, "", err
	}

	createdRow, err := q.CreateArtistIfNotExists(ctx, sqlc.CreateArtistIfNotExistsParams{
		MediaItemID:    mediaItemID,
		MusicbrainzID:  mbid,
		Name:           name,
		SortName:       sortName,
		Disambiguation: disambig,
		Biography:      bio,
	})
	if err != nil {
		return sqlc.Artist{}, "", err
	}
	created := musicArtistFromCreateIfNotExistsRow(createdRow)
	if created.MediaItemID != mediaItemID && musicArtistMBIDContradicts(created, mbid) {
		disambig = musicDisambiguationWithMBIDMarker(disambig, mbid)
		createdRow, err = q.CreateArtistIfNotExists(ctx, sqlc.CreateArtistIfNotExistsParams{
			MediaItemID:    mediaItemID,
			MusicbrainzID:  mbid,
			Name:           name,
			SortName:       sortName,
			Disambiguation: disambig,
			Biography:      bio,
		})
		if err != nil {
			return sqlc.Artist{}, "", err
		}
		created = musicArtistFromCreateIfNotExistsRow(createdRow)
	}
	if musicDetailHasRemoteMetadata(detail) {
		_ = applyMusicArtistExtended(ctx, q, created.ID, detail)
	}
	if created.MediaItemID != mediaItemID {
		return created, "adopt_artist_row", nil
	}
	return created, "create_artist_row", nil
}

func musicArtistFromCreateIfNotExistsRow(row sqlc.CreateArtistIfNotExistsRow) sqlc.Artist {
	return sqlc.Artist(row)
}

func applyMusicCanonicalArtistMediaItem(ctx context.Context, q *sqlc.Queries, current sqlc.MediaItemCard, currentAction string, artist sqlc.Artist, detail *metadata.MediaDetail) (sqlc.MediaItemCard, string, error) {
	if currentAction == "create_media_item" {
		if err := q.DeleteMediaItem(ctx, current.ID); err != nil {
			return sqlc.MediaItemCard{}, "", fmt.Errorf("delete duplicate artist media item %d: %w", current.ID, err)
		}
	}
	canonical, err := q.GetMediaItemByID(ctx, artist.MediaItemID)
	if err != nil {
		return sqlc.MediaItemCard{}, "", fmt.Errorf("get canonical artist media item %d: %w", artist.MediaItemID, err)
	}
	updated, err := q.UpdateMediaItem(ctx, musicUpdateMediaItemParams(canonical, detail))
	if err != nil {
		return sqlc.MediaItemCard{}, "", fmt.Errorf("update canonical artist media item %d: %w", canonical.ID, err)
	}
	if updated.Slug == "" {
		if err := updateMusicArtistSlug(ctx, q, updated.ID, updated.Title); err != nil {
			return sqlc.MediaItemCard{}, "", err
		}
	}
	if err := q.MarkMatched(ctx, updated.ID); err != nil {
		return sqlc.MediaItemCard{}, "", err
	}
	return updated, "adopt_media_item", nil
}

func musicArtistMBIDContradicts(existing sqlc.Artist, mbid string) bool {
	mbid = strings.TrimSpace(mbid)
	existingMBID := strings.TrimSpace(existing.MusicbrainzID)
	return mbid != "" &&
		existingMBID != "" &&
		existingMBID != mbid &&
		!musicIsSyntheticMBID(mbid) &&
		!musicIsSyntheticMBID(existingMBID)
}

func musicIsSyntheticMBID(mbid string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(mbid)), "dddddddd-")
}

func musicDisambiguationWithMBIDMarker(disambig, mbid string) string {
	marker := strings.TrimSpace(mbid)
	if len(marker) > 8 {
		marker = marker[:8]
	}
	if marker == "" {
		return disambig
	}
	return strings.TrimSpace(disambig + " (mbid " + marker + ")")
}

func applyMusicArtistExtended(ctx context.Context, q *sqlc.Queries, artistID int64, detail *metadata.MediaDetail) error {
	return q.UpdateArtistExtendedMetadata(ctx, sqlc.UpdateArtistExtendedMetadataParams{
		ID:              artistID,
		Listeners:       detail.ArtistListeners,
		Playcount:       detail.ArtistPlaycount,
		Popularity:      int32(detail.ArtistPopularity),
		Annotation:      detail.ArtistAnnotation,
		Urls:            mustJSONBytes(detail.ArtistURLs),
		WikipediaLinks:  mustJSONBytes(detail.ArtistWikipedia),
		Profiles:        mustJSONBytes(detail.ArtistProfiles),
		Aliases:         nonNilStrings(detail.ArtistAliases),
		Groups:          mustJSONBytes(detail.ArtistGroups),
		Members:         mustJSONBytes(detail.ArtistMembers),
		ArtistType:      detail.ArtistType,
		BeginDate:       detail.ArtistBeginDate,
		BeginYear:       int32(detail.ArtistBeginYear),
		EndDate:         detail.ArtistEndDate,
		Ended:           detail.ArtistEnded,
		Deathday:        detail.ArtistDeathday,
		Birthplace:      detail.ArtistBirthplace,
		Tags:            nonNilStrings(sortedUnique(detail.ArtistTags)),
		Genres:          nonNilStrings(sortedUnique(detail.Genres)),
		MetadataSources: nonNilStrings(detail.ArtistMetadataSources),
		Followers:       detail.ArtistFollowers,
	})
}

type musicApplyCounts struct {
	albumsCreated        int
	albumsUpdated        int
	tracksCreated        int
	tracksUpdated        int
	trackFilesCreated    int
	trackFilesUpdated    int
	filesCreated         int
	filesAttached        int
	filesAlreadyAttached int
	filesReassigned      int
}

func applyMusicAlbumsTracksAndFiles(ctx context.Context, q *sqlc.Queries, libraryID, mediaItemID, artistID int64, preview MusicMaterializePreview, meta MusicFetchPreview, tracksByRel map[string]MusicTrackPlan, filesByRel map[string][]InventoryFile) (musicApplyCounts, error) {
	var counts musicApplyCounts
	if _, err := q.LockArtistAlbumsForApply(ctx, artistID); err != nil {
		return counts, fmt.Errorf("lock artist albums for apply: %w", err)
	}
	fileActionByRel := map[string]MovieMaterializeFileAction{}
	for _, action := range preview.FileActions {
		fileActionByRel[action.RelPath] = action
	}
	recordingEvidenceByRel := musicRecordingEvidenceByRelPath(preview.RecordingEvidence)
	for _, mapping := range preview.AlbumMappings {
		remoteAlbum := musicAlbumEntryForApply(meta.Detail, mapping)
		album, action, err := applyMusicAlbum(ctx, q, artistID, mapping, remoteAlbum)
		if err != nil {
			return counts, err
		}
		if action == "create" {
			counts.albumsCreated++
		} else {
			counts.albumsUpdated++
		}
		if err := bindCanonicalChild(ctx, q, "album", album.ID, remoteAlbum.CanonicalID, "release_group"); err != nil {
			return counts, err
		}

		for _, trackMapping := range mapping.TrackMappings {
			if !trackMapping.Matched {
				continue
			}
			localTrack, ok := tracksByRel[trackMapping.RelPath]
			if !ok {
				return counts, fmt.Errorf("%s local track missing during apply", trackMapping.RelPath)
			}
			remoteTrack := metadata.TrackDetail{}
			if mapping.RemoteAlbum != "" {
				remoteTrack, _ = musicRemoteTrackForMapping(remoteAlbum, trackMapping)
			}
			track, created, err := applyMusicTrack(ctx, q, album.ID, localTrack, trackMapping, remoteTrack)
			if err != nil {
				return counts, err
			}
			if created {
				counts.tracksCreated++
			} else {
				counts.tracksUpdated++
			}
			if err := bindCanonicalChild(ctx, q, "track", track.ID, remoteTrack.CanonicalID, "recording"); err != nil {
				return counts, err
			}
			if evidence, ok := recordingEvidenceByRel[trackMapping.RelPath]; ok {
				if err := applyMusicFingerprintRecordingEvidence(ctx, q, track.ID, remoteTrack, evidence); err != nil {
					return counts, err
				}
			}
			fileCounts, err := applyMusicTrackFile(ctx, q, libraryID, mediaItemID, track.ID, localTrack, fileActionByRel[trackMapping.RelPath], filesByRel)
			if err != nil {
				return counts, err
			}
			counts.trackFilesCreated += fileCounts.trackFilesCreated
			counts.trackFilesUpdated += fileCounts.trackFilesUpdated
			counts.filesCreated += fileCounts.filesCreated
			counts.filesAttached += fileCounts.filesAttached
			counts.filesAlreadyAttached += fileCounts.filesAlreadyAttached
			counts.filesReassigned += fileCounts.filesReassigned
		}
	}
	return counts, nil
}

// musicRecordingEvidenceByRelPath keeps acoustic identity tied to the exact
// path that produced it. Similar-looking or differently-cased paths never
// share evidence, and conflicting duplicate claims are ignored altogether.
func musicRecordingEvidenceByRelPath(values []MusicAcceptedRecordingEvidence) map[string]MusicAcceptedRecordingEvidence {
	out := map[string]MusicAcceptedRecordingEvidence{}
	conflicted := map[string]bool{}
	for _, value := range values {
		if value.RelPath == "" || conflicted[value.RelPath] || value.Confidence <= 0 || value.SourceDuration <= 0 || value.RecordingDuration <= 0 {
			continue
		}
		value.RecordingMBID = strings.TrimSpace(value.RecordingMBID)
		value.CanonicalRecordingID = strings.TrimSpace(value.CanonicalRecordingID)
		if _, err := uuid.Parse(value.RecordingMBID); err != nil {
			continue
		}
		if _, err := uuid.Parse(value.CanonicalRecordingID); err != nil {
			continue
		}
		if existing, ok := out[value.RelPath]; ok {
			if !strings.EqualFold(existing.RecordingMBID, value.RecordingMBID) || !strings.EqualFold(existing.CanonicalRecordingID, value.CanonicalRecordingID) {
				delete(out, value.RelPath)
				conflicted[value.RelPath] = true
				continue
			}
			if existing.Confidence >= value.Confidence {
				continue
			}
		}
		out[value.RelPath] = value
	}
	return out
}

// applyMusicFingerprintRecordingEvidence fills recording identity that the
// fetched discography did not contain. Canonical fetch data and identities
// already stored on the local track are hard constraints: acoustic evidence
// may agree with them or fill a blank, but may never replace a conflict.
func applyMusicFingerprintRecordingEvidence(ctx context.Context, q *sqlc.Queries, trackID int64, remote metadata.TrackDetail, evidence MusicAcceptedRecordingEvidence) error {
	if remote.RecordingMBID != "" && !strings.EqualFold(strings.TrimSpace(remote.RecordingMBID), evidence.RecordingMBID) {
		return nil
	}
	if remote.CanonicalID != "" && !strings.EqualFold(strings.TrimSpace(remote.CanonicalID), evidence.CanonicalRecordingID) {
		return nil
	}
	track, err := q.GetTrackByID(ctx, trackID)
	if err != nil {
		return err
	}
	if track.RecordingMbid != "" && !strings.EqualFold(strings.TrimSpace(track.RecordingMbid), evidence.RecordingMBID) {
		return nil
	}
	binding, bindingErr := q.GetMetadataEntityBinding(ctx, sqlc.GetMetadataEntityBindingParams{LocalKind: "track", LocalID: trackID})
	if bindingErr == nil {
		if binding.EntityKind != "recording" || !strings.EqualFold(binding.EntityID.String(), evidence.CanonicalRecordingID) {
			return nil
		}
	} else if !errors.Is(bindingErr, pgx.ErrNoRows) {
		return bindingErr
	}

	if track.RecordingMbid == "" {
		externalIDs := track.ExternalIds
		if len(externalIDs) == 0 {
			externalIDs = []byte("{}")
		}
		artistCredits := track.ArtistCredits
		if len(artistCredits) == 0 {
			artistCredits = []byte("[]")
		}
		if err := q.UpdateTrackExtendedMetadata(ctx, sqlc.UpdateTrackExtendedMetadataParams{
			ID: track.ID, ExternalIds: externalIDs, Column3: track.Isrc,
			Column4: evidence.RecordingMBID, Column5: track.PreviewUrl, Explicit: track.Explicit,
			ArtistCredits: artistCredits, LyricsAvailable: track.LyricsAvailable,
		}); err != nil {
			return err
		}
	}
	if bindingErr == nil {
		return nil
	}
	return bindCanonicalChild(ctx, q, "track", trackID, evidence.CanonicalRecordingID, "recording")
}

func applyMusicAlbum(ctx context.Context, q *sqlc.Queries, artistID int64, mapping MusicAlbumFetchMatch, remote metadata.AlbumEntry) (sqlc.Album, string, error) {
	mbid := firstNonEmpty(remote.ExternalIDs["musicbrainz_release_group"], remote.ExternalIDs["musicbrainz_album"], mapping.RemoteExternalIDs["musicbrainz_release_group"], mapping.RemoteExternalIDs["musicbrainz_album"])
	year := musicMaterializeAlbumYear(mapping)
	params := musicAlbumParams(artistID, mapping, remote, mbid, year)
	existing, tupleFound := sqlc.Album{}, false
	if album, err := q.GetAlbumByArtistTitleYear(ctx, sqlc.GetAlbumByArtistTitleYearParams{
		ArtistID: artistID,
		Lower:    params.Title,
		Year:     params.Year,
	}); err == nil {
		existing, tupleFound = album, true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Album{}, "", err
	}

	mbidAlbum, mbidFound := sqlc.Album{}, false
	if mbid != "" {
		album, err := q.GetAlbumByArtistMusicBrainzID(ctx, sqlc.GetAlbumByArtistMusicBrainzIDParams{
			ArtistID:      artistID,
			MusicbrainzID: mbid,
		})
		if err == nil {
			mbidAlbum, mbidFound = album, true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Album{}, "", err
		}
	}

	// Exact local identity wins when embedded MBID evidence points at a
	// different sibling. This occurs in real libraries when an edition/remix
	// NFO carries the parent release-group MBID: choosing MBID first would load
	// the parent row and rename it onto the already-existing edition tuple,
	// tripping uq_albums_artist_title_year. Preserve the tuple owner's existing
	// MBID instead of moving the sibling's identity across albums.
	found := tupleFound || mbidFound
	if tupleFound {
		if mbidFound && mbidAlbum.ID != existing.ID {
			params.MusicbrainzID = existing.MusicbrainzID
		}
	} else if mbidFound {
		existing = mbidAlbum
	}
	if found {
		updated, err := q.UpdateAlbum(ctx, sqlc.UpdateAlbumParams{
			ID:            existing.ID,
			Title:         params.Title,
			Slug:          existing.Slug,
			Year:          params.Year,
			MusicbrainzID: params.MusicbrainzID,
			AlbumType:     params.AlbumType,
			Genres:        nonNilStrings(params.Genres),
			CoverPath:     firstNonEmpty(params.CoverPath, existing.CoverPath),
			ReleaseDate:   params.ReleaseDate,
			Label:         params.Label,
			Country:       params.Country,
			Barcode:       params.Barcode,
			TotalTracks:   params.TotalTracks,
			TotalDiscs:    params.TotalDiscs,
			Tags:          nonNilStrings(params.Tags),
		})
		if err != nil {
			return sqlc.Album{}, "", err
		}
		if mapping.RemoteAlbum != "" {
			_ = applyMusicAlbumExtended(ctx, q, updated.ID, remote)
		}
		return updated, "update", nil
	}

	created, err := q.CreateAlbum(ctx, params)
	if err != nil {
		return sqlc.Album{}, "", err
	}
	albumSlug := slug.GenerateUnique(ctx, created.Title, created.Year, created.ID, func(ctx context.Context, candidate string, excludeID int64) (bool, error) {
		return q.AlbumSlugExists(ctx, sqlc.AlbumSlugExistsParams{ArtistID: artistID, Slug: candidate, ID: excludeID})
	})
	if err := q.SetAlbumSlug(ctx, sqlc.SetAlbumSlugParams{ID: created.ID, Slug: albumSlug}); err != nil {
		return sqlc.Album{}, "", err
	}
	created.Slug = albumSlug
	if mapping.RemoteAlbum != "" {
		_ = applyMusicAlbumExtended(ctx, q, created.ID, remote)
	}
	return created, "create", nil
}

func musicAlbumEntryForApply(detail *metadata.MediaDetail, mapping MusicAlbumFetchMatch) metadata.AlbumEntry {
	if mapping.RemoteAlbum != "" {
		if remoteAlbum, ok := musicRemoteAlbumForMapping(detail, mapping); ok {
			return remoteAlbum
		}
	}
	year, _ := strconv.Atoi(mapping.LocalYear)
	return metadata.AlbumEntry{
		Title:       firstNonEmpty(mapping.LocalAlbum, mapping.RemoteAlbum, "Unknown Album"),
		Year:        year,
		Type:        firstNonEmpty(mapping.LocalKind, mapping.RemoteKind, "album"),
		ExternalIDs: copyMusicExternalIDs(mapping.LocalExternalIDs),
		TrackCount:  mapping.LocalTracks,
	}
}

func musicAlbumParams(artistID int64, mapping MusicAlbumFetchMatch, remote metadata.AlbumEntry, mbid, year string) sqlc.CreateAlbumParams {
	return sqlc.CreateAlbumParams{
		ArtistID:      artistID,
		Title:         firstNonEmpty(remote.Title, mapping.RemoteAlbum, mapping.LocalAlbum),
		Year:          year,
		MusicbrainzID: mbid,
		AlbumType:     firstNonEmpty(mapping.RemoteKind, normalizeMusicReleaseKind(remote.Type), "album"),
		Genres:        nonNilStrings(sortedUnique(append(append([]string{}, remote.Genres...), remote.Tags...))),
		CoverPath:     remote.CoverURL,
		ReleaseDate:   musicPGDateFromString(remote.ReleaseDate),
		Label:         remote.Label,
		Country:       remote.Country,
		Barcode:       remote.Barcode,
		TotalTracks:   int32(firstPositive(remote.TrackCount, len(remote.Tracks), mapping.RemoteTracks, mapping.MappedTracks)),
		TotalDiscs:    int32(maxMusicDisc(remote.Tracks)),
		Tags:          nonNilStrings(sortedUnique(remote.Tags)),
	}
}

func applyMusicAlbumExtended(ctx context.Context, q *sqlc.Queries, albumID int64, remote metadata.AlbumEntry) error {
	ratings := remote.Ratings
	if ratings == nil {
		ratings = []metadata.AlbumRating{}
	}
	editions := remote.Editions
	if editions == nil {
		editions = []metadata.AlbumEdition{}
	}
	releaseEvents := remote.ReleaseEvents
	if releaseEvents == nil {
		releaseEvents = []metadata.AlbumReleaseEvent{}
	}
	return q.UpdateAlbumExtendedMetadata(ctx, sqlc.UpdateAlbumExtendedMetadataParams{
		ID:             albumID,
		Column2:        remote.CatalogNo,
		Column3:        remote.OriginalTitle,
		Column4:        remote.Language,
		Explicit:       remote.Explicit,
		Column6:        int32(remote.Duration),
		Column7:        pgNumericFromFloat64(remote.Rating),
		Popularity:     int32(remote.Popularity),
		Listeners:      remote.Listeners,
		Playcount:      remote.Playcount,
		SecondaryTypes: nonNilStrings(remote.SecondaryTypes),
		Styles:         nonNilStrings(remote.Styles),
		Isrcs:          nonNilStrings(remote.ISRCs),
		ExternalIds:    mustJSONBytes(remote.ExternalIDs),
		ArtistCredits:  mustJSONBytes(remote.ArtistCredits),
		Column16:       remote.Description,
		Column17:       remote.Review,
		Ratings:        mustJSONBytes(ratings),
		Editions:       mustJSONBytes(editions),
		Column20:       remote.Sales,
		Artwork:        mustJSONBytes(albumArtworkRefs(remote.Artwork)),
		Column22:       remote.Script,
		ReleaseEvents:  mustJSONBytes(releaseEvents),
	})
}

// albumArtworkRefs projects the full ArtworkResult pool down to the slim
// {type, url} shape albums.artwork persists (never nil — jsonb NOT NULL).
// Mirrors the matcher's twin of the same name (separate package, same
// write contract).
func albumArtworkRefs(values []metadata.ArtworkResult) []metadata.AlbumArtworkRef {
	out := make([]metadata.AlbumArtworkRef, 0, len(values))
	for _, v := range values {
		if v.URL == "" {
			continue
		}
		out = append(out, metadata.AlbumArtworkRef{Type: v.AssetType, URL: v.URL})
	}
	return out
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func applyMusicTrack(ctx context.Context, q *sqlc.Queries, albumID int64, local MusicTrackPlan, mapping MusicTrackFetchMatch, remote metadata.TrackDetail) (sqlc.Track, bool, error) {
	disc, trackNumber := musicMaterializeTrackNumbers(mapping)
	if disc <= 0 {
		disc = 1
	}
	if trackNumber <= 0 {
		trackNumber = 1
	}
	title := firstNonEmpty(remote.Title, mapping.RemoteTitle, local.TrackTitle, mapping.LocalTitle, "Untitled")
	duration := int32(remote.Duration)
	existing, err := q.GetTrackByAlbumDiscTrack(ctx, sqlc.GetTrackByAlbumDiscTrackParams{
		AlbumID:     albumID,
		DiscNumber:  int32(disc),
		TrackNumber: int32(trackNumber),
	})
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Track{}, false, err
	}
	track, err := q.GetOrCreateTrack(ctx, sqlc.GetOrCreateTrackParams{
		AlbumID:     albumID,
		DiscNumber:  int32(disc),
		TrackNumber: int32(trackNumber),
		Title:       title,
		Duration:    duration,
	})
	if err != nil {
		return sqlc.Track{}, false, err
	}
	if found && (title != "" || duration > 0) {
		updateTitle := firstNonEmpty(title, existing.Title)
		updateDuration := existing.Duration
		if duration > 0 {
			updateDuration = duration
		}
		if updated, err := q.UpdateTrackTitleAndDuration(ctx, sqlc.UpdateTrackTitleAndDurationParams{
			ID:       track.ID,
			Title:    updateTitle,
			Duration: updateDuration,
		}); err == nil {
			track = updated
		}
	}
	// An album-level fetch can legitimately lack this recording. Do not let an
	// empty TrackDetail erase rich data already stored on a local fallback row.
	if remote.Title != "" || remote.RecordingMBID != "" || remote.CanonicalID != "" || len(remote.ExternalIDs) > 0 {
		if err := q.UpdateTrackExtendedMetadata(ctx, sqlc.UpdateTrackExtendedMetadataParams{
			ID:              track.ID,
			ExternalIds:     mustJSONBytes(remote.ExternalIDs),
			Column3:         remote.ISRC,
			Column4:         remote.RecordingMBID,
			Column5:         remote.PreviewURL,
			Explicit:        remote.Explicit,
			ArtistCredits:   mustJSONBytes(remote.ArtistCredits),
			LyricsAvailable: remote.LyricsAvailable,
		}); err != nil {
			return sqlc.Track{}, false, err
		}
	}
	return track, !found, nil
}

type musicTrackFileApplyCounts struct {
	trackFilesCreated    int
	trackFilesUpdated    int
	filesCreated         int
	filesAttached        int
	filesAlreadyAttached int
	filesReassigned      int
}

func applyMusicTrackFile(ctx context.Context, q *sqlc.Queries, libraryID, mediaItemID, trackID int64, local MusicTrackPlan, action MovieMaterializeFileAction, filesByRel map[string][]InventoryFile) (musicTrackFileApplyCounts, error) {
	var counts musicTrackFileApplyCounts
	if action.Action == "blocked" {
		return counts, fmt.Errorf("%s blocked: %s", action.RelPath, action.Reason)
	}
	fileID := action.FileID
	invFile, ok := singleInventoryFile(filesByRel, local.RelPath)
	if !ok {
		return counts, fmt.Errorf("%s inventory file missing", local.RelPath)
	}
	switch action.Action {
	case "create_library_file_and_attach":
		file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
			LibraryID:   libraryID,
			Path:        invFile.Path,
			Size:        invFile.Size,
			Mtime:       pgtype.Timestamptz{Time: invFile.MTime, Valid: !invFile.MTime.IsZero()},
			ParseResult: musicLibraryFileParseResult(local),
			Status:      sqlc.FileStatusPending,
		})
		if err != nil {
			return counts, err
		}
		fileID = file.ID
		counts.filesCreated++
		counts.filesAttached++
	case "attach_existing_library_file":
		counts.filesAttached++
	case "already_attached":
		counts.filesAlreadyAttached++
	case "reassign_library_file":
		counts.filesReassigned++
	default:
		return counts, fmt.Errorf("%s unsupported file action %q", local.RelPath, action.Action)
	}
	if fileID == 0 {
		return counts, fmt.Errorf("%s has no library file id", local.RelPath)
	}
	if action.Action != "create_library_file_and_attach" {
		if err := q.TouchLibraryFileSeen(ctx, sqlc.TouchLibraryFileSeenParams{
			ID:    fileID,
			Size:  invFile.Size,
			Mtime: pgtype.Timestamptz{Time: invFile.MTime, Valid: !invFile.MTime.IsZero()},
		}); err != nil {
			return counts, err
		}
	}

	existingTrackFile, err := q.GetTrackFileByLibraryFileID(ctx, fileID)
	existingTF := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return counts, err
	}
	if err := q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID:          fileID,
		Status:      sqlc.FileStatusMatched,
		MediaItemID: pgInt8(mediaItemID),
	}); err != nil {
		return counts, err
	}
	format := strings.ToLower(strings.TrimPrefix(filepath.Ext(invFile.Path), "."))
	if local.Format != "" {
		format = strings.ToLower(local.Format)
	}
	_, err = q.UpsertTrackFile(ctx, sqlc.UpsertTrackFileParams{
		TrackID:       trackID,
		LibraryFileID: fileID,
		Format:        format,
		QualityScore:  int32(mediaprobe.ExtensionQualityBase(format)),
		SizeBytes:     invFile.Size,
	})
	if err != nil {
		return counts, err
	}
	if existingTF && existingTrackFile.TrackID != trackID {
		if err := pruneReassignedMusicTrack(ctx, q, existingTrackFile.TrackID, trackID); err != nil {
			return counts, err
		}
	}
	// The upsert resets loudness when the bytes changed; the
	// sonic facets are keyed by track and need the same invalidation so the
	// analysis pump re-measures instead of settling on data computed from
	// the old audio.
	if existingTF && existingTrackFile.SizeBytes != invFile.Size {
		if err := q.ResetTrackFacetsVersionForTrack(ctx, trackID); err != nil {
			return counts, err
		}
	}
	if existingTF && existingTrackFile.TrackID == trackID {
		counts.trackFilesUpdated++
	} else if existingTF {
		counts.trackFilesUpdated++
	} else {
		counts.trackFilesCreated++
	}
	return counts, nil
}

// pruneReassignedMusicTrack closes the repair loop after a library file moves
// between canonical artists. The old track/album must disappear once empty or
// the UI keeps showing the poisoned release under both artists. User-owned
// state is folded onto the replacement before deletion; a source track which
// still has another file is left intact because the two copies may be distinct.
func pruneReassignedMusicTrack(ctx context.Context, q *sqlc.Queries, sourceTrackID, targetTrackID int64) error {
	source, err := q.GetTrackByIDForUpdate(ctx, sourceTrackID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock reassigned source track %d: %w", sourceTrackID, err)
	}
	hasFiles, err := q.TrackHasFiles(ctx, sourceTrackID)
	if err != nil {
		return fmt.Errorf("inspect reassigned source track %d: %w", sourceTrackID, err)
	}
	if hasFiles {
		return nil
	}
	target, err := q.GetTrackByID(ctx, targetTrackID)
	if err != nil {
		return fmt.Errorf("load reassigned target track %d: %w", targetTrackID, err)
	}
	if err := q.MergeTrackRatingsInto(ctx, sqlc.MergeTrackRatingsIntoParams{DstTrackID: targetTrackID, SrcTrackID: sourceTrackID}); err != nil {
		return fmt.Errorf("move reassigned track ratings: %w", err)
	}
	if err := q.MergeTrackPlaylistsInto(ctx, sqlc.MergeTrackPlaylistsIntoParams{DstTrackID: targetTrackID, SrcTrackID: sourceTrackID}); err != nil {
		return fmt.Errorf("move reassigned track playlists: %w", err)
	}
	if err := q.MergeTrackFavoritesInto(ctx, sqlc.MergeTrackFavoritesIntoParams{DstTrackID: targetTrackID, SrcTrackID: sourceTrackID}); err != nil {
		return fmt.Errorf("move reassigned track favorites: %w", err)
	}
	if err := q.ReparentPlayQueueItemsInto(ctx, sqlc.ReparentPlayQueueItemsIntoParams{DstTrackID: targetTrackID, SrcTrackID: sourceTrackID}); err != nil {
		return fmt.Errorf("move reassigned play queue items: %w", err)
	}
	if err := q.ReparentExternalListensInto(ctx, sqlc.ReparentExternalListensIntoParams{DstTrackID: pgInt8(targetTrackID), SrcTrackID: pgInt8(sourceTrackID)}); err != nil {
		return fmt.Errorf("move reassigned external listens: %w", err)
	}
	if err := q.ReparentTrackPlayEventsInto(ctx, sqlc.ReparentTrackPlayEventsIntoParams{DstTrackID: targetTrackID, SrcTrackID: sourceTrackID}); err != nil {
		return fmt.Errorf("move reassigned track play events: %w", err)
	}
	if err := q.DeleteMetadataEntityBinding(ctx, sqlc.DeleteMetadataEntityBindingParams{LocalKind: "track", LocalID: sourceTrackID}); err != nil {
		return fmt.Errorf("delete reassigned source track metadata binding: %w", err)
	}
	if err := q.DeleteTrackByID(ctx, sourceTrackID); err != nil {
		return fmt.Errorf("delete empty reassigned source track: %w", err)
	}
	if source.AlbumID == target.AlbumID {
		return nil
	}
	hasTracks, err := q.AlbumHasTracks(ctx, source.AlbumID)
	if err != nil {
		return fmt.Errorf("inspect reassigned source album %d: %w", source.AlbumID, err)
	}
	if hasTracks {
		return nil
	}
	if err := q.MergeAlbumRatings(ctx, sqlc.MergeAlbumRatingsParams{DstAlbumID: target.AlbumID, SrcAlbumID: source.AlbumID}); err != nil {
		return fmt.Errorf("move reassigned album ratings: %w", err)
	}
	if err := q.MergeAlbumFavorites(ctx, sqlc.MergeAlbumFavoritesParams{DstAlbumID: target.AlbumID, SrcAlbumID: source.AlbumID}); err != nil {
		return fmt.Errorf("move reassigned album favorites: %w", err)
	}
	if err := q.DeleteAlbumByID(ctx, source.AlbumID); err != nil {
		return fmt.Errorf("delete empty reassigned source album: %w", err)
	}
	return nil
}

func musicLibraryFileParseResult(local MusicTrackPlan) []byte {
	return mustJSONBytes(map[string]any{
		"scanner":      "scanner",
		"media_type":   "music",
		"artist":       local.Artist,
		"album":        local.Album,
		"title":        local.TrackTitle,
		"disc_number":  local.DiscNumber,
		"track_number": local.TrackNumber,
		"external_ids": local.ExternalIDs,
	})
}

func musicRemoteAlbumForMapping(detail *metadata.MediaDetail, mapping MusicAlbumFetchMatch) (metadata.AlbumEntry, bool) {
	if detail == nil {
		return metadata.AlbumEntry{}, false
	}
	for _, remote := range detail.Albums {
		if sharedExternalID(remote.ExternalIDs, mergeStringMaps(mapping.RemoteExternalIDs, mapping.LocalExternalIDs)) {
			return remote, true
		}
	}
	for _, remote := range detail.Albums {
		if normalizeSearchTitle(remote.Title) == normalizeSearchTitle(mapping.RemoteAlbum) && musicAlbumYearString(remote) == musicMaterializeAlbumYear(mapping) {
			return remote, true
		}
	}
	for _, remote := range detail.Albums {
		if normalizeSearchTitle(remote.Title) == normalizeSearchTitle(mapping.RemoteAlbum) {
			return remote, true
		}
	}
	return metadata.AlbumEntry{}, false
}

func musicRemoteTrackForMapping(album metadata.AlbumEntry, mapping MusicTrackFetchMatch) (metadata.TrackDetail, bool) {
	disc, trackNumber := musicMaterializeTrackNumbers(mapping)
	for _, remote := range album.Tracks {
		if remote.DiscNumber == disc && remote.TrackNumber == trackNumber {
			return remote, true
		}
	}
	for _, remote := range album.Tracks {
		if normalizeSearchTitle(remote.Title) == normalizeSearchTitle(mapping.RemoteTitle) {
			return remote, true
		}
	}
	return metadata.TrackDetail{}, false
}

func musicAlbumYearString(album metadata.AlbumEntry) string {
	if album.Year > 0 {
		return fmt.Sprintf("%d", album.Year)
	}
	if len(album.ReleaseDate) >= 4 {
		return album.ReleaseDate[:4]
	}
	return ""
}

func musicTracksByRel(tracks []MusicTrackPlan) map[string]MusicTrackPlan {
	out := map[string]MusicTrackPlan{}
	for _, track := range tracks {
		out[track.RelPath] = track
	}
	return out
}

func firstMusicArtworkURL(artwork []metadata.ArtworkResult) string {
	for _, item := range artwork {
		if item.URL != "" {
			return item.URL
		}
	}
	return ""
}

func maxMusicDisc(tracks []metadata.TrackDetail) int {
	maxDisc := 1
	for _, track := range tracks {
		if track.DiscNumber > maxDisc {
			maxDisc = track.DiscNumber
		}
	}
	return maxDisc
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func musicPGDateFromString(s string) pgtype.Date {
	if strings.TrimSpace(s) == "" {
		return pgtype.Date{}
	}
	for _, format := range []string{"2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(format, s); err == nil {
			return pgtype.Date{Time: t, Valid: true}
		}
	}
	if len(s) >= 4 {
		if t, err := time.Parse("2006", strings.TrimSpace(s[:4])); err == nil {
			return pgtype.Date{Time: t, Valid: true}
		}
	}
	return pgtype.Date{}
}

func emitMusicApplyResult(result MusicApplyResult, emit Emitter) {
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
		Kind:     "music",
		Reason:   result.Reason,
		Data: map[string]any{
			"key":         result.Key,
			"artist":      result.Artist,
			"action":      result.Action,
			"media_item":  result.MediaItemID,
			"artist_id":   result.ArtistID,
			"albums":      result.AlbumsCreated + result.AlbumsUpdated,
			"tracks":      result.TracksCreated + result.TracksUpdated,
			"track_files": result.TrackFilesCreated + result.TrackFilesUpdated,
			"files":       result.FilesCreated + result.FilesAttached + result.FilesAlreadyAttached + result.FilesReassigned,
			"error":       result.Error,
		},
	})
}

func emitMusicApplySummary(results []MusicApplyResult, emit Emitter) {
	if emit == nil {
		return
	}
	summary := map[string]any{"plans": len(results)}
	for _, result := range results {
		summary[result.Action] = intFromAny(summary[result.Action]) + 1
		summary["albums_created"] = intFromAny(summary["albums_created"]) + result.AlbumsCreated
		summary["albums_updated"] = intFromAny(summary["albums_updated"]) + result.AlbumsUpdated
		summary["tracks_created"] = intFromAny(summary["tracks_created"]) + result.TracksCreated
		summary["tracks_updated"] = intFromAny(summary["tracks_updated"]) + result.TracksUpdated
		summary["track_files_created"] = intFromAny(summary["track_files_created"]) + result.TrackFilesCreated
		summary["track_files_updated"] = intFromAny(summary["track_files_updated"]) + result.TrackFilesUpdated
	}
	emit.Emit(Event{Event: "materialize.apply_summary", Kind: "music", Data: summary})
}

func sortMusicApplyResults(items []MusicApplyResult) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Artist < items[j].Artist
	})
}
