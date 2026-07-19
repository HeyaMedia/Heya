package scanner

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

type MusicMaterializeStore interface {
	FindMediaItemByExternalIDs(context.Context, int64, map[string]string) (sqlc.MediaItemCard, bool, error)
	FindMediaItemByIdentity(context.Context, int64, string) (sqlc.MediaItemCard, bool, error)
	GetMediaItemByID(context.Context, int64) (sqlc.MediaItemCard, bool, error)
	GetArtistByMediaItemID(context.Context, int64) (sqlc.Artist, bool, error)
	GetAlbumByMusicBrainzID(context.Context, string) (sqlc.Album, bool, error)
	GetAlbumByArtistTitleYear(context.Context, int64, string, string) (sqlc.Album, bool, error)
	GetTrackByAlbumDiscTrack(context.Context, int64, int32, int32) (sqlc.Track, bool, error)
	GetLibraryFileByPath(context.Context, int64, string) (sqlc.LibraryFile, bool, error)
	GetTrackFileByLibraryFileID(context.Context, int64) (sqlc.TrackFile, bool, error)
}

type SQLMusicMaterializeStore struct {
	q *sqlc.Queries
}

func NewSQLMusicMaterializeStore(db sqlc.DBTX) *SQLMusicMaterializeStore {
	return &SQLMusicMaterializeStore{q: sqlc.New(db)}
}

func (s *SQLMusicMaterializeStore) FindMediaItemByExternalIDs(ctx context.Context, libraryID int64, ids map[string]string) (sqlc.MediaItemCard, bool, error) {
	for _, key := range orderedMusicExternalIDKeys(ids) {
		value := ids[key]
		if value == "" {
			continue
		}
		item, err := s.q.GetMediaItemByNormalizedExternalID(ctx, sqlc.GetMediaItemByNormalizedExternalIDParams{
			LibraryID:  libraryID,
			Provider:   key,
			ExternalID: value,
		})
		if err == nil {
			// Provider IDs occasionally become polluted on an old same-name
			// artist row. MusicBrainz is the identity spine: a weaker shared ID
			// may never select a row whose MBID contradicts the target.
			if musicMediaItemIdentityCompatible(item, ids) {
				return item, true, nil
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return sqlc.MediaItemCard{}, false, err
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
		if musicMediaItemIdentityCompatible(item, ids) {
			return item, true, nil
		}
		return sqlc.MediaItemCard{}, false, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.MediaItemCard{}, false, nil
	}
	return sqlc.MediaItemCard{}, false, err
}

func musicMediaItemIdentityCompatible(item sqlc.MediaItemCard, targetIDs map[string]string) bool {
	_, contradictory := compareStrongMusicArtistExternalIDs(externalIDsFromMediaItem(item), targetIDs)
	return !contradictory
}

func (s *SQLMusicMaterializeStore) FindMediaItemByIdentity(ctx context.Context, libraryID int64, title string) (sqlc.MediaItemCard, bool, error) {
	item, err := s.q.FindMediaItemByIdentity(ctx, sqlc.FindMediaItemByIdentityParams{
		LibraryID:      libraryID,
		MediaType:      sqlc.MediaTypeMusic,
		Title:          title,
		Year:           "",
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

func (s *SQLMusicMaterializeStore) GetMediaItemByID(ctx context.Context, mediaItemID int64) (sqlc.MediaItemCard, bool, error) {
	item, err := s.q.GetMediaItemByID(ctx, mediaItemID)
	if err == nil {
		return item, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.MediaItemCard{}, false, nil
	}
	return sqlc.MediaItemCard{}, false, err
}

func (s *SQLMusicMaterializeStore) GetArtistByMediaItemID(ctx context.Context, mediaItemID int64) (sqlc.Artist, bool, error) {
	artist, err := s.q.GetArtistByMediaItemID(ctx, mediaItemID)
	if err == nil {
		return artist, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Artist{}, false, nil
	}
	return sqlc.Artist{}, false, err
}

func (s *SQLMusicMaterializeStore) GetAlbumByMusicBrainzID(ctx context.Context, mbid string) (sqlc.Album, bool, error) {
	if strings.TrimSpace(mbid) == "" {
		return sqlc.Album{}, false, nil
	}
	album, err := s.q.GetAlbumByMusicBrainzID(ctx, mbid)
	if err == nil {
		return album, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Album{}, false, nil
	}
	return sqlc.Album{}, false, err
}

func (s *SQLMusicMaterializeStore) GetAlbumByArtistTitleYear(ctx context.Context, artistID int64, title, year string) (sqlc.Album, bool, error) {
	if artistID == 0 || strings.TrimSpace(title) == "" {
		return sqlc.Album{}, false, nil
	}
	album, err := s.q.GetAlbumByArtistTitleYear(ctx, sqlc.GetAlbumByArtistTitleYearParams{
		ArtistID: artistID,
		Lower:    title,
		Year:     year,
	})
	if err == nil {
		return album, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Album{}, false, nil
	}
	return sqlc.Album{}, false, err
}

func (s *SQLMusicMaterializeStore) GetTrackByAlbumDiscTrack(ctx context.Context, albumID int64, disc, track int32) (sqlc.Track, bool, error) {
	if albumID == 0 || disc <= 0 || track <= 0 {
		return sqlc.Track{}, false, nil
	}
	row, err := s.q.GetTrackByAlbumDiscTrack(ctx, sqlc.GetTrackByAlbumDiscTrackParams{
		AlbumID:     albumID,
		DiscNumber:  disc,
		TrackNumber: track,
	})
	if err == nil {
		return row, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Track{}, false, nil
	}
	return sqlc.Track{}, false, err
}

func (s *SQLMusicMaterializeStore) GetLibraryFileByPath(ctx context.Context, libraryID int64, path string) (sqlc.LibraryFile, bool, error) {
	file, err := s.q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{LibraryID: libraryID, Path: path})
	if err == nil {
		return file, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.LibraryFile{}, false, nil
	}
	return sqlc.LibraryFile{}, false, err
}

func (s *SQLMusicMaterializeStore) GetTrackFileByLibraryFileID(ctx context.Context, libraryFileID int64) (sqlc.TrackFile, bool, error) {
	if libraryFileID == 0 {
		return sqlc.TrackFile{}, false, nil
	}
	file, err := s.q.GetTrackFileByLibraryFileID(ctx, libraryFileID)
	if err == nil {
		return file, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.TrackFile{}, false, nil
	}
	return sqlc.TrackFile{}, false, err
}

type MusicMaterializePreview struct {
	Key               string                           `json:"key"`
	Action            string                           `json:"action"`
	Reason            string                           `json:"reason,omitempty"`
	Artist            string                           `json:"artist"`
	SortName          string                           `json:"sort_name,omitempty"`
	ProviderID        string                           `json:"provider_id,omitempty"`
	MediaItemID       int64                            `json:"media_item_id,omitempty"`
	ArtistID          int64                            `json:"artist_id,omitempty"`
	MediaItemAction   string                           `json:"media_item_action,omitempty"`
	ArtistRowAction   string                           `json:"artist_row_action,omitempty"`
	AlbumActions      []MusicMaterializeAlbumAction    `json:"album_actions,omitempty"`
	AlbumMappings     []MusicAlbumFetchMatch           `json:"-"`
	FileActions       []MovieMaterializeFileAction     `json:"file_actions,omitempty"`
	RecordingEvidence []MusicAcceptedRecordingEvidence `json:"recording_evidence,omitempty"`
	ExternalIDs       map[string]string                `json:"external_ids,omitempty"`
	MetadataFields    []string                         `json:"metadata_fields,omitempty"`
	LocalAlbums       int                              `json:"local_albums,omitempty"`
	MappedAlbums      int                              `json:"mapped_albums,omitempty"`
	RemoteAlbums      int                              `json:"remote_albums,omitempty"`
	LocalTracks       int                              `json:"local_tracks,omitempty"`
	MappedTracks      int                              `json:"mapped_tracks,omitempty"`
	RemoteTracks      int                              `json:"remote_tracks,omitempty"`
	RemoteArtwork     int                              `json:"remote_artwork,omitempty"`
	Tags              int                              `json:"tags,omitempty"`
	AlbumsCreate      int                              `json:"albums_create,omitempty"`
	AlbumsUpdate      int                              `json:"albums_update,omitempty"`
	TracksCreate      int                              `json:"tracks_create,omitempty"`
	TracksUpdate      int                              `json:"tracks_update,omitempty"`
	TrackFilesCreate  int                              `json:"track_files_create,omitempty"`
	TrackFilesUpdate  int                              `json:"track_files_update,omitempty"`
	Issues            []string                         `json:"issues,omitempty"`
}

type MusicMaterializeAlbumAction struct {
	Key          string   `json:"key"`
	Action       string   `json:"action"`
	Reason       string   `json:"reason,omitempty"`
	LocalAlbum   string   `json:"local_album"`
	RemoteAlbum  string   `json:"remote_album,omitempty"`
	Year         string   `json:"year,omitempty"`
	AlbumID      int64    `json:"album_id,omitempty"`
	TracksCreate int      `json:"tracks_create,omitempty"`
	TracksUpdate int      `json:"tracks_update,omitempty"`
	Issues       []string `json:"issues,omitempty"`
}

func PlanMusicMaterialization(ctx context.Context, lib sqlc.Library, result Result, store MusicMaterializeStore, emit Emitter) ([]MusicMaterializePreview, error) {
	if store == nil {
		return nil, fmt.Errorf("music materialize store is required")
	}

	metadata := map[string]MusicFetchPreview{}
	for _, preview := range result.MusicMetadata {
		metadata[preview.Key] = preview
	}
	searchByKey := map[string]MusicSearchMatch{}
	for _, search := range result.MusicSearch {
		searchByKey[search.Key] = search
	}
	filesByRel := inventoryFilesByRel(result.Inventory)

	previews := make([]MusicMaterializePreview, 0, len(result.MusicArtists))
	for _, local := range result.MusicArtists {
		if err := ctx.Err(); err != nil {
			return previews, err
		}
		search, hasSearch := searchByKey[local.Key]
		preview := MusicMaterializePreview{
			Key:        local.Key,
			Action:     "blocked",
			ProviderID: search.ProviderID,
			Artist:     firstNonEmpty(search.Artist, local.Artist, search.Query.Artist),
			ExternalIDs: mergeStringMaps(
				local.ExternalIDs,
				search.ExternalIDs,
				musicProviderIDExternalID(search.ProviderID),
			),
			LocalAlbums: len(local.Albums),
			LocalTracks: countMusicArtistTracks(local),
		}
		if hasSearch && search.Accepted {
			preview.RecordingEvidence = musicMaterializeRecordingEvidence(local, search.RecordingEvidence)
		}
		if !hasSearch {
			preview.Reason = "local_only"
		} else if !search.Accepted {
			preview.Reason = "search_rejected"
			preview.Issues = append(preview.Issues, search.Reason)
		}

		meta, ok := metadata[local.Key]
		if !ok && preview.Reason == "" {
			preview.Reason = "metadata_not_fetched"
		}
		if ok {
			preview.ProviderID = firstNonEmpty(meta.ProviderID, preview.ProviderID)
			preview.Artist = firstNonEmpty(meta.Artist, preview.Artist)
			preview.SortName = meta.SortName
			preview.ExternalIDs = mergeStringMaps(preview.ExternalIDs, meta.ExternalIDs, musicProviderIDExternalID(meta.ProviderID))
			preview.MetadataFields = append([]string{}, meta.WouldApply...)
			preview.LocalAlbums = meta.LocalAlbums
			preview.MappedAlbums = meta.MappedAlbums
			preview.RemoteAlbums = meta.RemoteAlbums
			preview.LocalTracks = meta.LocalTracks
			preview.MappedTracks = meta.MappedTracks
			preview.RemoteTracks = meta.RemoteTracks
			preview.RemoteArtwork = meta.Artwork
			preview.Tags = meta.Tags
			if meta.Error != "" {
				preview.Reason = "metadata_fetch_failed"
				preview.Issues = append(preview.Issues, meta.Error)
			} else if meta.Detail == nil {
				preview.Reason = "metadata_detail_missing"
			}
		}

		preview.AlbumMappings = musicMaterializeAlbumMappings(local, meta)
		if err := planMusicTarget(ctx, lib, preview.AlbumMappings, filesByRel, store, &preview); err != nil {
			return previews, err
		}
		previews = append(previews, preview)
		emitMusicMaterializePreview(preview, emit)
	}

	sort.Slice(previews, func(i, j int) bool {
		if previews[i].Action == previews[j].Action {
			return previews[i].Artist < previews[j].Artist
		}
		return materializeActionRank(previews[i].Action) < materializeActionRank(previews[j].Action)
	})
	emit.Emit(Event{Event: "materialize.preview_summary", Kind: "music", Data: musicMaterializeSummary(previews)})
	return previews, nil
}

// musicMaterializeRecordingEvidence narrows accepted acoustic evidence to the
// exact paths owned by this artist. It deliberately does not clean, case-fold,
// or slash-normalize paths: a fingerprint from one file must never migrate to
// a merely similar path. Conflicting duplicate claims are discarded.
func musicMaterializeRecordingEvidence(local MusicArtistPlan, values []MusicAcceptedRecordingEvidence) []MusicAcceptedRecordingEvidence {
	localPaths := map[string]bool{}
	for _, album := range local.Albums {
		for _, track := range album.Tracks {
			if track.RelPath != "" {
				localPaths[track.RelPath] = true
			}
		}
	}
	byPath := map[string]MusicAcceptedRecordingEvidence{}
	conflicted := map[string]bool{}
	for _, value := range values {
		if !localPaths[value.RelPath] || conflicted[value.RelPath] || value.Confidence <= 0 || value.SourceDuration <= 0 || value.RecordingDuration <= 0 {
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
		if existing, ok := byPath[value.RelPath]; ok {
			if !strings.EqualFold(existing.RecordingMBID, value.RecordingMBID) || !strings.EqualFold(existing.CanonicalRecordingID, value.CanonicalRecordingID) {
				delete(byPath, value.RelPath)
				conflicted[value.RelPath] = true
				continue
			}
			if existing.Confidence >= value.Confidence {
				continue
			}
		}
		byPath[value.RelPath] = value
	}
	out := make([]MusicAcceptedRecordingEvidence, 0, len(byPath))
	for _, value := range byPath {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out
}

func planMusicTarget(ctx context.Context, lib sqlc.Library, mappings []MusicAlbumFetchMatch, filesByRel map[string][]InventoryFile, store MusicMaterializeStore, preview *MusicMaterializePreview) error {
	item, found, err := findMusicMaterializeMediaItem(ctx, store, lib.ID, preview.ExternalIDs, preview.Artist)
	if err != nil {
		return err
	}
	if found {
		if item.MediaType != sqlc.MediaTypeMusic {
			preview.Action = "blocked"
			preview.Reason = "media_type_conflict"
			preview.Issues = append(preview.Issues, fmt.Sprintf("existing_media_item=%d type=%s", item.ID, item.MediaType))
			return nil
		}
		preview.MediaItemID = item.ID
		preview.MediaItemAction = "update_media_item"
		artist, hasArtist, err := store.GetArtistByMediaItemID(ctx, item.ID)
		if err != nil {
			return err
		}
		if hasArtist {
			preview.ArtistID = artist.ID
			preview.ArtistRowAction = "update_artist_row"
		} else {
			preview.ArtistRowAction = "create_artist_row"
		}
		preview.Action = "update"
	} else {
		preview.MediaItemAction = "create_media_item"
		preview.ArtistRowAction = "create_artist_row"
		preview.Action = "create"
	}

	if err := planMusicAlbumAndTrackCounts(ctx, preview.ArtistID, mappings, store, preview); err != nil {
		return err
	}
	preview.FileActions = planMusicFileActions(ctx, lib.ID, musicMappedRelPaths(mappings), filesByRel, preview.MediaItemID, preview.Artist, preview.ExternalIDs, store)
	preview.TrackFilesCreate, preview.TrackFilesUpdate = planMusicTrackFileCounts(ctx, preview.FileActions, store)
	if fileIssues := materializeFileIssues(preview.FileActions); len(fileIssues) > 0 {
		preview.Action = "blocked"
		preview.Reason = "file_conflict"
		preview.Issues = append(preview.Issues, fileIssues...)
	} else if preview.Action != "blocked" && hasMovieFileAction(preview.FileActions, "reassign_library_file") {
		preview.Action = "repair"
		preview.Reason = "stale_file_attachment"
	}
	return nil
}

func findMusicMaterializeMediaItem(ctx context.Context, store MusicMaterializeStore, libraryID int64, ids map[string]string, artist string) (sqlc.MediaItemCard, bool, error) {
	if len(ids) > 0 {
		if item, ok, err := store.FindMediaItemByExternalIDs(ctx, libraryID, ids); err != nil || ok {
			return item, ok, err
		}
		// A stable provider identity which is new to this library must create a
		// distinct artist. Falling back to a case-insensitive title lookup here
		// merged unrelated same-name acts (most visibly LISA and LiSA) before the
		// second artist's external-ID binding existed locally.
		if len(strongMusicArtistExternalIDs(ids)) > 0 {
			return sqlc.MediaItemCard{}, false, nil
		}
	}
	if artist == "" {
		return sqlc.MediaItemCard{}, false, nil
	}
	return store.FindMediaItemByIdentity(ctx, libraryID, artist)
}

func musicMaterializeAlbumMappings(local MusicArtistPlan, meta MusicFetchPreview) []MusicAlbumFetchMatch {
	remoteByKey := map[string]MusicAlbumFetchMatch{}
	if meta.Error == "" && meta.Detail != nil {
		for _, mapping := range meta.AlbumMappings {
			remoteByKey[mapping.Key] = mapping
		}
	}
	mappings := make([]MusicAlbumFetchMatch, 0, len(local.Albums))
	for _, album := range local.Albums {
		remote, ok := remoteByKey[album.Key]
		if ok && musicRemoteAlbumMappingClean(remote) {
			mappings = append(mappings, remote)
			continue
		}
		localMapping := musicLocalAlbumMapping(album)
		if ok && remote.RemoteAlbum != "" {
			localMapping = musicAlbumMappingWithLocalTrackFallback(localMapping, remote)
		}
		mappings = append(mappings, localMapping)
	}
	sort.Slice(mappings, func(i, j int) bool {
		if mappings[i].LocalYear == mappings[j].LocalYear {
			return mappings[i].LocalAlbum < mappings[j].LocalAlbum
		}
		return mappings[i].LocalYear < mappings[j].LocalYear
	})
	return mappings
}

func musicAlbumMappingWithLocalTrackFallback(local, remote MusicAlbumFetchMatch) MusicAlbumFetchMatch {
	// The remote release-group match remains useful even when none of its many
	// issued editions has been materialized into a tracklist yet. Preserve that
	// canonical album evidence, but use the local files as the track authority.
	local.RemoteAlbum = remote.RemoteAlbum
	local.RemoteYear = remote.RemoteYear
	local.RemoteKind = remote.RemoteKind
	local.RemoteExternalIDs = copyMusicExternalIDs(remote.RemoteExternalIDs)
	local.Confidence = remote.Confidence
	local.Reason = firstNonEmpty(remote.Reason, "remote_album") + "_local_tracks"
	local.RemoteTracks = remote.RemoteTracks

	remoteByPath := make(map[string]MusicTrackFetchMatch, len(remote.TrackMappings))
	for _, track := range remote.TrackMappings {
		if track.RelPath != "" && track.Matched && track.Issue == "" {
			remoteByPath[track.RelPath] = track
		}
	}
	for i, track := range local.TrackMappings {
		if remoteTrack, ok := remoteByPath[track.RelPath]; ok {
			local.TrackMappings[i] = remoteTrack
		}
	}
	return local
}

func musicRemoteAlbumMappingClean(mapping MusicAlbumFetchMatch) bool {
	return mapping.RemoteAlbum != "" &&
		len(mapping.Issues) == 0 &&
		mapping.LocalTracks > 0 &&
		mapping.MappedTracks == mapping.LocalTracks
}

func musicLocalAlbumMapping(album MusicAlbumPlan) MusicAlbumFetchMatch {
	mapping := MusicAlbumFetchMatch{
		Key:              album.Key,
		LocalAlbum:       album.Album,
		LocalYear:        album.Year,
		LocalKind:        album.ReleaseKind,
		LocalExternalIDs: copyMusicExternalIDs(album.ExternalIDs),
		LocalTracks:      len(album.Tracks),
		MappedTracks:     len(album.Tracks),
		Reason:           "local_only",
	}
	used := map[int]map[int]bool{}
	for i, track := range album.Tracks {
		disc := track.DiscNumber
		if disc <= 0 {
			disc = 1
		}
		if used[disc] == nil {
			used[disc] = map[int]bool{}
		}
		trackNumber := track.TrackNumber
		if trackNumber <= 0 || used[disc][trackNumber] {
			trackNumber = nextLocalMusicTrackNumber(used[disc], i+1)
		}
		used[disc][trackNumber] = true
		mapping.TrackMappings = append(mapping.TrackMappings, MusicTrackFetchMatch{
			RelPath:    track.RelPath,
			LocalTitle: track.TrackTitle,
			LocalDisc:  disc,
			LocalTrack: trackNumber,
			Confidence: track.Confidence,
			Reason:     "local_only",
			Matched:    true,
		})
	}
	return mapping
}

func nextLocalMusicTrackNumber(used map[int]bool, start int) int {
	if start <= 0 {
		start = 1
	}
	for used[start] {
		start++
	}
	return start
}

func planMusicAlbumAndTrackCounts(ctx context.Context, artistID int64, mappings []MusicAlbumFetchMatch, store MusicMaterializeStore, preview *MusicMaterializePreview) error {
	for _, mapping := range mappings {
		action := MusicMaterializeAlbumAction{
			Key:         mapping.Key,
			Action:      "create",
			LocalAlbum:  mapping.LocalAlbum,
			RemoteAlbum: mapping.RemoteAlbum,
			Year:        musicMaterializeAlbumYear(mapping),
		}
		if artistID == 0 {
			action.TracksCreate = mapping.MappedTracks
			preview.AlbumsCreate++
			preview.TracksCreate += mapping.MappedTracks
			preview.AlbumActions = append(preview.AlbumActions, action)
			continue
		}

		album, found, err := findMusicMaterializeAlbum(ctx, artistID, mapping, store)
		if err != nil {
			return err
		}
		if found {
			action.Action = "update"
			action.AlbumID = album.ID
			preview.AlbumsUpdate++
		} else {
			preview.AlbumsCreate++
		}

		for _, trackMapping := range mapping.TrackMappings {
			if !trackMapping.Matched {
				continue
			}
			disc, track := musicMaterializeTrackNumbers(trackMapping)
			_, trackFound, err := store.GetTrackByAlbumDiscTrack(ctx, action.AlbumID, int32(disc), int32(track))
			if err != nil {
				return err
			}
			if action.AlbumID != 0 && trackFound {
				action.TracksUpdate++
				preview.TracksUpdate++
			} else {
				action.TracksCreate++
				preview.TracksCreate++
			}
		}
		preview.AlbumActions = append(preview.AlbumActions, action)
	}
	sort.Slice(preview.AlbumActions, func(i, j int) bool {
		if preview.AlbumActions[i].Year == preview.AlbumActions[j].Year {
			return firstNonEmpty(preview.AlbumActions[i].RemoteAlbum, preview.AlbumActions[i].LocalAlbum) < firstNonEmpty(preview.AlbumActions[j].RemoteAlbum, preview.AlbumActions[j].LocalAlbum)
		}
		return preview.AlbumActions[i].Year < preview.AlbumActions[j].Year
	})
	return nil
}

func findMusicMaterializeAlbum(ctx context.Context, artistID int64, mapping MusicAlbumFetchMatch, store MusicMaterializeStore) (sqlc.Album, bool, error) {
	for _, mbid := range musicMaterializeAlbumMBIDs(mapping) {
		album, found, err := store.GetAlbumByMusicBrainzID(ctx, mbid)
		if err != nil {
			return sqlc.Album{}, false, err
		}
		if found {
			if album.ArtistID == artistID {
				return album, true, nil
			}
			return sqlc.Album{}, false, nil
		}
	}
	return store.GetAlbumByArtistTitleYear(ctx, artistID, musicMaterializeAlbumTitle(mapping), musicMaterializeAlbumYear(mapping))
}

func planMusicFileActions(ctx context.Context, libraryID int64, relPaths []string, filesByRel map[string][]InventoryFile, targetMediaItemID int64, targetArtist string, targetExternalIDs map[string]string, store MusicMaterializeStore) []MovieMaterializeFileAction {
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
				if canRepairMusicFileAttachment(existingItem, targetArtist, targetExternalIDs) {
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

func planMusicTrackFileCounts(ctx context.Context, actions []MovieMaterializeFileAction, store MusicMaterializeStore) (create, update int) {
	for _, action := range actions {
		if action.FileID == 0 || action.Action == "blocked" {
			if action.Action != "blocked" {
				create++
			}
			continue
		}
		_, found, err := store.GetTrackFileByLibraryFileID(ctx, action.FileID)
		if err != nil {
			create++
			continue
		}
		if found {
			update++
		} else {
			create++
		}
	}
	return create, update
}

func canRepairMusicFileAttachment(existing sqlc.MediaItemCard, targetArtist string, targetExternalIDs map[string]string) bool {
	if existing.MediaType != sqlc.MediaTypeMusic {
		return false
	}
	existingIDs := externalIDsFromMediaItem(existing)
	shared, contradictory := compareStrongMusicArtistExternalIDs(existingIDs, targetExternalIDs)
	// MusicBrainz is the identity spine. A contradictory MBID proves these are
	// different artists even when an older bad merge polluted the row with the
	// target's Apple, Spotify, or other weaker provider ID. That is precisely
	// the attachment this repair path exists to undo.
	if contradictory {
		return true
	}
	if shared || sharedExternalID(existingIDs, targetExternalIDs) {
		return false
	}
	return normalizeSearchTitle(existing.Title) != normalizeSearchTitle(targetArtist)
}

// strongMusicArtistExternalIDs normalizes the stable artist namespaces Heya
// receives from local tags and HeyaMetadata. Aliases are folded together so a
// legacy `musicbrainz_artist` binding compares correctly with a current `mbid`
// result. Values remain sets because collaboration tags may carry several IDs.
func strongMusicArtistExternalIDs(ids map[string]string) map[string]map[string]bool {
	providers := map[string]string{
		"mbid": "musicbrainz", "musicbrainz_artist": "musicbrainz", "musicbrainz:artist": "musicbrainz",
		"apple": "apple", "apple_artist": "apple", "apple:artist": "apple", "itunes_artist": "apple",
		"deezer": "deezer", "deezer_artist": "deezer", "deezer:artist": "deezer",
		"discogs": "discogs", "discogs_artist": "discogs", "discogs:artist": "discogs",
		"spotify": "spotify", "spotify_artist": "spotify", "spotify:artist": "spotify",
	}
	out := map[string]map[string]bool{}
	for key, raw := range ids {
		provider := providers[strings.ToLower(strings.TrimSpace(key))]
		if provider == "" {
			continue
		}
		for _, value := range strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == ',' }) {
			value = strings.ToLower(strings.TrimSpace(value))
			if value == "" {
				continue
			}
			if out[provider] == nil {
				out[provider] = map[string]bool{}
			}
			out[provider][value] = true
		}
	}
	return out
}

func compareStrongMusicArtistExternalIDs(left, right map[string]string) (shared, contradictory bool) {
	leftIDs := strongMusicArtistExternalIDs(left)
	rightIDs := strongMusicArtistExternalIDs(right)
	// MusicBrainz is the artist spine. When both sides carry it, it decides the
	// relation even if a weaker provider ID is stale or was later replaced.
	if len(leftIDs["musicbrainz"]) > 0 && len(rightIDs["musicbrainz"]) > 0 {
		for value := range leftIDs["musicbrainz"] {
			if rightIDs["musicbrainz"][value] {
				return true, false
			}
		}
		return false, true
	}
	for provider, leftValues := range leftIDs {
		if provider == "musicbrainz" {
			continue
		}
		rightValues := rightIDs[provider]
		if len(rightValues) == 0 {
			continue
		}
		providerShared := false
		for value := range leftValues {
			if rightValues[value] {
				providerShared = true
				shared = true
				break
			}
		}
		if !providerShared {
			contradictory = true
		}
	}
	if shared {
		return true, false
	}
	return shared, contradictory
}

func musicMappedRelPaths(mappings []MusicAlbumFetchMatch) []string {
	seen := map[string]bool{}
	var out []string
	for _, album := range mappings {
		for _, track := range album.TrackMappings {
			if !track.Matched || track.RelPath == "" || seen[track.RelPath] {
				continue
			}
			seen[track.RelPath] = true
			out = append(out, track.RelPath)
		}
	}
	sort.Strings(out)
	return out
}

func musicMaterializeTrackNumbers(mapping MusicTrackFetchMatch) (disc, track int) {
	disc = mapping.RemoteDisc
	track = mapping.RemoteTrack
	if disc <= 0 {
		disc = mapping.LocalDisc
	}
	if track <= 0 {
		track = mapping.LocalTrack
	}
	return disc, track
}

func musicMaterializeAlbumTitle(mapping MusicAlbumFetchMatch) string {
	return firstNonEmpty(mapping.RemoteAlbum, mapping.LocalAlbum)
}

func musicMaterializeAlbumYear(mapping MusicAlbumFetchMatch) string {
	if mapping.RemoteYear > 0 {
		return strconv.Itoa(mapping.RemoteYear)
	}
	return mapping.LocalYear
}

func musicMaterializeAlbumMBIDs(mapping MusicAlbumFetchMatch) []string {
	ids := mergeStringMaps(mapping.RemoteExternalIDs, mapping.LocalExternalIDs)
	var out []string
	for _, key := range []string{"musicbrainz_release_group", "musicbrainz_album", "mbid"} {
		if ids[key] != "" {
			out = append(out, ids[key])
		}
	}
	return sortedUnique(out)
}

func musicProviderIDExternalID(providerID string) map[string]string {
	parts := strings.Split(providerID, ":")
	if len(parts) >= 4 && parts[0] == "heya" && parts[2] != "" && parts[3] != "" {
		return map[string]string{parts[2]: strings.Join(parts[3:], ":")}
	}
	return nil
}

func orderedMusicExternalIDKeys(ids map[string]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, key := range []string{"mbid", "musicbrainz_artist", "apple", "discogs", "deezer", "spotify", "itunes_artist"} {
		if ids[key] != "" {
			out = append(out, key)
			seen[key] = true
		}
	}
	var rest []string
	for key := range ids {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func emitMusicMaterializePreview(preview MusicMaterializePreview, emit Emitter) {
	event := "materialize.preview"
	severity := SeverityInfo
	if preview.Action == "blocked" {
		event = "materialize.blocked"
		severity = SeverityWarn
	}
	emit.Emit(Event{
		Event:    event,
		Severity: severity,
		Kind:     "music",
		Reason:   preview.Reason,
		Data: map[string]any{
			"key":               preview.Key,
			"artist":            preview.Artist,
			"action":            preview.Action,
			"media_item_action": preview.MediaItemAction,
			"artist_row_action": preview.ArtistRowAction,
			"media_item_id":     preview.MediaItemID,
			"artist_id":         preview.ArtistID,
			"albums":            len(preview.AlbumActions),
			"files":             len(preview.FileActions),
			"issues":            preview.Issues,
		},
	})
}

func musicMaterializeSummary(previews []MusicMaterializePreview) map[string]any {
	summary := map[string]any{"plans": len(previews)}
	for _, preview := range previews {
		summary[preview.Action] = intFromAny(summary[preview.Action]) + 1
		if preview.MediaItemAction != "" {
			summary[preview.MediaItemAction] = intFromAny(summary[preview.MediaItemAction]) + 1
		}
		if preview.ArtistRowAction != "" {
			summary[preview.ArtistRowAction] = intFromAny(summary[preview.ArtistRowAction]) + 1
		}
		summary["albums_create"] = intFromAny(summary["albums_create"]) + preview.AlbumsCreate
		summary["albums_update"] = intFromAny(summary["albums_update"]) + preview.AlbumsUpdate
		summary["tracks_create"] = intFromAny(summary["tracks_create"]) + preview.TracksCreate
		summary["tracks_update"] = intFromAny(summary["tracks_update"]) + preview.TracksUpdate
		summary["track_files_create"] = intFromAny(summary["track_files_create"]) + preview.TrackFilesCreate
		summary["track_files_update"] = intFromAny(summary["track_files_update"]) + preview.TrackFilesUpdate
		for _, file := range preview.FileActions {
			key := "file_" + file.Action
			summary[key] = intFromAny(summary[key]) + 1
		}
	}
	return summary
}
