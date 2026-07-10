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
		ID:             artistID,
		Listeners:      detail.ArtistListeners,
		Playcount:      detail.ArtistPlaycount,
		Popularity:     int32(detail.ArtistPopularity),
		Annotation:     detail.ArtistAnnotation,
		Urls:           mustJSONBytes(detail.ArtistURLs),
		WikipediaLinks: mustJSONBytes(detail.ArtistWikipedia),
		Profiles:       mustJSONBytes(detail.ArtistProfiles),
		Aliases:        nonNilStrings(detail.ArtistAliases),
		Groups:         mustJSONBytes(detail.ArtistGroups),
		Members:        mustJSONBytes(detail.ArtistMembers),
		ArtistType:     detail.ArtistType,
		BeginDate:      detail.ArtistBeginDate,
		BeginYear:      int32(detail.ArtistBeginYear),
		EndDate:        detail.ArtistEndDate,
		Ended:          detail.ArtistEnded,
		Deathday:       detail.ArtistDeathday,
		Birthplace:     detail.ArtistBirthplace,
		Tags:           nonNilStrings(sortedUnique(append(append([]string{}, detail.Genres...), detail.ArtistTags...))),
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
	fileActionByRel := map[string]MovieMaterializeFileAction{}
	for _, action := range preview.FileActions {
		fileActionByRel[action.RelPath] = action
	}
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

func applyMusicAlbum(ctx context.Context, q *sqlc.Queries, artistID int64, mapping MusicAlbumFetchMatch, remote metadata.AlbumEntry) (sqlc.Album, string, error) {
	mbid := firstNonEmpty(remote.ExternalIDs["musicbrainz_release_group"], remote.ExternalIDs["musicbrainz_album"], mapping.RemoteExternalIDs["musicbrainz_release_group"], mapping.RemoteExternalIDs["musicbrainz_album"])
	year := musicMaterializeAlbumYear(mapping)
	existing, found := sqlc.Album{}, false
	if mbid != "" {
		if album, err := q.GetAlbumByMusicBrainzID(ctx, mbid); err == nil && album.ArtistID == artistID {
			existing, found = album, true
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Album{}, "", err
		}
	}
	if !found {
		if album, err := q.GetAlbumByArtistTitleYear(ctx, sqlc.GetAlbumByArtistTitleYearParams{
			ArtistID: artistID,
			Lower:    firstNonEmpty(remote.Title, mapping.RemoteAlbum),
			Year:     year,
		}); err == nil {
			existing, found = album, true
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Album{}, "", err
		}
	}

	params := musicAlbumParams(artistID, mapping, remote, mbid, year)
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
	})
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
	_ = q.UpdateTrackExtendedMetadata(ctx, sqlc.UpdateTrackExtendedMetadataParams{
		ID:            track.ID,
		ExternalIds:   mustJSONBytes(remote.ExternalIDs),
		Column3:       remote.ISRC,
		Column4:       remote.RecordingMBID,
		Column5:       remote.PreviewURL,
		Explicit:      remote.Explicit,
		ArtistCredits: mustJSONBytes(remote.ArtistCredits),
	})
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
	trackFile, err := q.UpsertTrackFile(ctx, sqlc.UpsertTrackFileParams{
		TrackID:       trackID,
		LibraryFileID: fileID,
		Format:        format,
		QualityScore:  int32(mediaprobe.ExtensionQualityBase(format)),
		SizeBytes:     invFile.Size,
	})
	if err != nil {
		return counts, err
	}
	if existingTF && existingTrackFile.TrackID == trackID {
		counts.trackFilesUpdated++
	} else if existingTF {
		counts.trackFilesUpdated++
	} else {
		counts.trackFilesCreated++
	}
	if err := refreshMusicTrackPrimary(ctx, q, trackID, trackFile); err != nil {
		return counts, err
	}
	return counts, nil
}

func refreshMusicTrackPrimary(ctx context.Context, q *sqlc.Queries, trackID int64, fallback sqlc.TrackFile) error {
	primary, err := q.GetPrimaryTrackFile(ctx, trackID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		primary = fallback
	}
	file, err := q.GetLibraryFileByID(ctx, primary.LibraryFileID)
	if err != nil {
		return err
	}
	return q.UpdateTrackPrimary(ctx, sqlc.UpdateTrackPrimaryParams{
		ID:            trackID,
		FilePath:      file.Path,
		LibraryFileID: pgInt8(file.ID),
		LyricsPath:    primary.LyricsPath,
	})
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
