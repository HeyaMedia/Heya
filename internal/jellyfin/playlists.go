package jellyfin

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Playlists: Heya's native per-user playlists exposed on the Jellyfin
// surface. Heya playlists hold tracks, which matches upstream's typed-
// playlist behavior — real Jellyfin silently filters media that doesn't fit
// a playlist's MediaType, so adding a movie is a no-op there too.
//
// PlaylistItemId (the handle DELETE …/Items?EntryIds= expects) is simply the
// track's own Jellyfin id: Heya's playlist rows are (playlist, track) pairs,
// so the track id is a stable, reversible entry handle.

// dtoFromPlaylist renders the playlist container item.
func (s *Server) dtoFromPlaylist(row sqlc.ListUserPlaylistsRow, serverID string) baseItemDto {
	count := int32(row.TrackCount)
	dto := baseItemDto{
		Name:              row.Name,
		ServerID:          serverID,
		ID:                EncodeID(KindPlaylist, row.ID),
		Etag:              tag32("etag-playlist", row.ID),
		Overview:          row.Description,
		CanDownload:       false,
		Taglines:          []string{},
		Genres:            []string{},
		DateCreated:       tsTime(row.CreatedAt),
		IsFolder:          true,
		Type:              "Playlist",
		MediaType:         "Audio",
		LocationType:      "FileSystem",
		BackdropImageTags: []string{},
	}
	if row.HasCover {
		dto.ImageTags = map[string]string{"Primary": tag32("img-playlist", row.ID)}
		dto.PrimaryImageAspectRatio = &aspectSquare
	}
	if count > 0 {
		dto.ChildCount = &count
	}
	return dto.done()
}

// playlistDto fetches one playlist (owner-scoped) as a dto.
func (s *Server) playlistDto(ctx context.Context, userID, playlistID int64, serverID string) (baseItemDto, bool) {
	rows, err := s.app.ListUserPlaylists(ctx, userID)
	if err != nil {
		return baseItemDto{}, false
	}
	for _, row := range rows {
		if row.ID == playlistID {
			return s.dtoFromPlaylist(row, serverID), true
		}
	}
	return baseItemDto{}, false
}

// playlistsResult lists the user's playlists for /Items queries
// (IncludeItemTypes=Playlist — how clients build their playlist pickers).
func (s *Server) playlistsResult(ctx context.Context, userID int64, serverID string, req itemsRequest) (queryResult[baseItemDto], error) {
	empty := queryResult[baseItemDto]{Items: []baseItemDto{}, StartIndex: req.startIndex}
	rows, err := s.app.ListUserPlaylists(ctx, userID)
	if err != nil {
		return empty, err
	}
	items := make([]baseItemDto, 0, len(rows))
	for _, row := range rows {
		if req.searchTerm != "" && !strings.Contains(strings.ToLower(row.Name), strings.ToLower(req.searchTerm)) {
			continue
		}
		items = append(items, s.dtoFromPlaylist(row, serverID))
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	total := len(items)
	items = window(items, req.startIndex, req.limit)
	return queryResult[baseItemDto]{Items: items, TotalRecordCount: total, StartIndex: req.startIndex}, nil
}

// playlistTracksResult renders a playlist's tracks in playlist order —
// backs GET /Playlists/{id}/Items and /Items?parentId={playlist}.
func (s *Server) playlistTracksResult(ctx context.Context, userID, playlistID int64, serverID string, req itemsRequest) (queryResult[baseItemDto], error) {
	empty := queryResult[baseItemDto]{Items: []baseItemDto{}, StartIndex: req.startIndex}
	detail, err := s.app.GetUserPlaylistDetail(ctx, userID, playlistID)
	if err != nil {
		return empty, err
	}
	ordered := make([]int64, 0, len(detail.Tracks))
	for _, t := range detail.Tracks {
		ordered = append(ordered, t.TrackID)
	}
	total := len(ordered)
	ordered = window(ordered, req.startIndex, req.limit)
	if len(ordered) == 0 {
		return queryResult[baseItemDto]{Items: []baseItemDto{}, TotalRecordCount: total, StartIndex: req.startIndex}, nil
	}

	rows, _, err := s.app.JFListTracks(ctx, sqlc.JFListTracksParams{OnlyIds: ordered})
	if err != nil {
		return empty, err
	}
	dec := s.favoriteDecor(ctx, userID, "track")
	dtoByID := make(map[int64]baseItemDto, len(rows))
	rowByID := make(map[int64]sqlc.JFListTracksRow, len(rows))
	for _, row := range rows {
		dtoByID[row.ID] = s.dtoFromTrackRow(row, serverID, dec)
		rowByID[row.ID] = row
	}

	items := make([]baseItemDto, 0, len(ordered))
	orderedRows := make([]sqlc.JFListTracksRow, 0, len(ordered))
	parent := EncodeID(KindPlaylist, playlistID)
	for _, id := range ordered {
		dto, ok := dtoByID[id]
		if !ok {
			continue // track deleted underneath the playlist
		}
		dto.ParentID = parent
		dto.PlaylistItemID = EncodeID(KindTrack, id)
		items = append(items, dto)
		orderedRows = append(orderedRows, rowByID[id])
	}
	if req.wantsSources() {
		s.attachTrackSources(ctx, orderedRows, items, req)
	}
	return queryResult[baseItemDto]{Items: items, TotalRecordCount: total, StartIndex: req.startIndex}, nil
}

// window slices a list by startIndex/limit with Jellyfin semantics
// (limit<=0 → unbounded).
func window[T any](items []T, start, limit int) []T {
	if start >= len(items) {
		return nil
	}
	items = items[start:]
	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}
	return items
}

// --- handlers ---

// POST /Playlists — create, optionally seeded with items. Non-track seeds
// are filtered like upstream filters type-incompatible media.
func (s *Server) handleCreatePlaylist(w http.ResponseWriter, r *http.Request, _ Params) {
	u, _ := UserFrom(r.Context())
	var body struct {
		Name      string   `json:"Name"`
		Ids       []string `json:"Ids"`
		MediaType string   `json:"MediaType"`
	}
	_ = decodeJSON(r, &body)
	if body.Name == "" {
		body.Name = queryCI(r, "name")
	}
	if len(body.Ids) == 0 && queryCI(r, "ids") != "" {
		body.Ids = strings.Split(queryCI(r, "ids"), ",")
	}
	if body.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	pl, err := s.app.CreateUserPlaylist(r.Context(), u.ID, body.Name, "")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	for _, raw := range body.Ids {
		if trackID, err := DecodeIDKind(strings.TrimSpace(raw), KindTrack); err == nil {
			_ = s.app.AddTrackToPlaylist(r.Context(), u.ID, pl.ID, trackID)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"Id": EncodeID(KindPlaylist, pl.ID)})
}

// GET /Playlists/{playlistId}/Items
func (s *Server) handleGetPlaylistItems(w http.ResponseWriter, r *http.Request, p Params) {
	u, _ := UserFrom(r.Context())
	playlistID, err := DecodeIDKind(p["playlistId"], KindPlaylist)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	req := parseItemsRequest(r)
	res, err := s.playlistTracksResult(r.Context(), u.ID, playlistID, s.serverID(r), req)
	if err != nil {
		http.NotFound(w, r) // user-scoped lookup: not yours == not found
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// POST /Playlists/{playlistId}/Items?ids=
func (s *Server) handleAddPlaylistItems(w http.ResponseWriter, r *http.Request, p Params) {
	u, _ := UserFrom(r.Context())
	playlistID, err := DecodeIDKind(p["playlistId"], KindPlaylist)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	for _, raw := range strings.Split(queryCI(r, "ids"), ",") {
		if trackID, err := DecodeIDKind(strings.TrimSpace(raw), KindTrack); err == nil {
			_ = s.app.AddTrackToPlaylist(r.Context(), u.ID, playlistID, trackID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /Playlists/{playlistId}/Items?entryIds=
func (s *Server) handleRemovePlaylistItems(w http.ResponseWriter, r *http.Request, p Params) {
	u, _ := UserFrom(r.Context())
	playlistID, err := DecodeIDKind(p["playlistId"], KindPlaylist)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	for _, raw := range strings.Split(queryCI(r, "entryIds"), ",") {
		if trackID, err := DecodeIDKind(strings.TrimSpace(raw), KindTrack); err == nil {
			_ = s.app.RemoveTrackFromPlaylist(r.Context(), u.ID, playlistID, trackID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /Playlists/{playlistId}/Users/{userId} — edit-permission probe.
// Heya playlists are strictly owner-private; the owner can edit, everyone
// else gets upstream's "no access record" 404.
func (s *Server) handlePlaylistUser(w http.ResponseWriter, r *http.Request, p Params) {
	u, _ := UserFrom(r.Context())
	playlistID, err := DecodeIDKind(p["playlistId"], KindPlaylist)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	askedUser, err := DecodeIDKind(p["userId"], KindUser)
	if err != nil || askedUser != u.ID {
		http.NotFound(w, r)
		return
	}
	if _, ok := s.playlistDto(r.Context(), u.ID, playlistID, s.serverID(r)); !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"UserId":  EncodeID(KindUser, u.ID),
		"CanEdit": true,
	})
}
