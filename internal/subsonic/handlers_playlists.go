package subsonic

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Playlists — full CRUD onto Heya's user playlists (the same rows the web
// player's sidebar shows). Subsonic playlists are per-owner; Heya has no
// shared playlists, so public is always false and only the owner sees them.

func playlistDTO(p sqlc.UserPlaylist, songCount int32, duration int32) Playlist {
	return Playlist{
		ID:        EncodeID(KindPlaylist, p.ID),
		Name:      p.Name,
		Comment:   p.Description,
		Public:    false,
		SongCount: songCount,
		Duration:  duration,
		Created:   subTimePtr(p.CreatedAt.Time),
		Changed:   subTimePtr(p.UpdatedAt.Time),
		CoverArt:  EncodeID(KindPlaylist, p.ID),
	}
}

// getPlaylists.
func (s *Server) handleGetPlaylists(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	// The username param lets admins list another user's playlists; Heya
	// keeps playlists private, so only the caller's own are served.
	if requested := param(r, "username"); requested != "" && requested != u.Username {
		respondError(w, r, errNotAuthorized, "playlists are private per user")
		return
	}
	rows, err := s.app.ListUserPlaylists(r.Context(), u.ID)
	if err != nil {
		respondError(w, r, errGeneric, "listing playlists failed")
		return
	}
	out := Playlists{Playlists: []Playlist{}}
	for _, row := range rows {
		dto := playlistDTO(sqlc.UserPlaylist{
			ID: row.ID, UserID: row.UserID, Name: row.Name, Description: row.Description,
			CoverPath: row.CoverPath, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}, int32(row.TrackCount), 0) //nolint:gosec // playlist sizes are tiny
		dto.Owner = u.Username
		out.Playlists = append(out.Playlists, dto)
	}
	respond(w, r, "playlists", &out)
}

// playlistWithSongs hydrates one playlist detail.
func (s *Server) playlistWithSongs(r *http.Request, userID, playlistID int64, username string) (*PlaylistWithSongs, error) {
	detail, err := s.app.GetUserPlaylistDetail(r.Context(), userID, playlistID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(detail.Tracks))
	for _, t := range detail.Tracks {
		ids = append(ids, t.TrackID)
	}
	entries := s.tracksByIDs(r.Context(), userID, ids)
	var duration int32
	for _, e := range entries {
		duration += e.Duration
	}
	out := &PlaylistWithSongs{
		Playlist: playlistDTO(detail.Playlist, int32(len(entries)), duration), //nolint:gosec // bounded
		Entries:  entries,
	}
	out.Owner = username
	return out, nil
}

// getPlaylist.
func (s *Server) handleGetPlaylist(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	playlistID, err := DecodeIDKind(param(r, "id"), KindPlaylist)
	if err != nil {
		respondError(w, r, errNotFound, "playlist not found")
		return
	}
	out, err := s.playlistWithSongs(r, u.ID, playlistID, u.Username)
	if err != nil {
		respondError(w, r, errNotFound, "playlist not found")
		return
	}
	respond(w, r, "playlist", out)
}

// createPlaylist — create (name+songId...) or, per spec, overwrite an
// existing one's songs when playlistId is given.
func (s *Server) handleCreatePlaylist(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	ctx := r.Context()

	var songIDs []int64
	for _, raw := range paramAll(r, "songId") {
		id, err := DecodeIDKind(raw, KindTrack)
		if err != nil {
			respondError(w, r, errNotFound, "unknown songId: "+raw)
			return
		}
		songIDs = append(songIDs, id)
	}

	var playlistID int64
	if existing := param(r, "playlistId"); existing != "" {
		id, err := DecodeIDKind(existing, KindPlaylist)
		if err != nil {
			respondError(w, r, errNotFound, "playlist not found")
			return
		}
		detail, err := s.app.GetUserPlaylistDetail(ctx, u.ID, id)
		if err != nil {
			respondError(w, r, errNotFound, "playlist not found")
			return
		}
		// Spec semantics: createPlaylist with playlistId REPLACES the songs.
		for _, t := range detail.Tracks {
			if err := s.app.RemoveTrackFromPlaylist(ctx, u.ID, id, t.TrackID); err != nil {
				respondError(w, r, errGeneric, "updating playlist failed")
				return
			}
		}
		playlistID = id
	} else {
		name := param(r, "name")
		if name == "" {
			respondError(w, r, errMissingParameter, `either "playlistId" or "name" is required`)
			return
		}
		created, err := s.app.CreateUserPlaylist(ctx, u.ID, name, "", "")
		if err != nil {
			respondError(w, r, errGeneric, "creating playlist failed")
			return
		}
		playlistID = created.ID
	}

	for _, id := range songIDs {
		if err := s.app.AddTrackToPlaylist(ctx, u.ID, playlistID, id); err != nil {
			respondError(w, r, errGeneric, "adding song failed")
			return
		}
	}
	out, err := s.playlistWithSongs(r, u.ID, playlistID, u.Username)
	if err != nil {
		respondError(w, r, errGeneric, "reading playlist back failed")
		return
	}
	respond(w, r, "playlist", out)
}

// updatePlaylist — rename/comment plus incremental add/remove.
func (s *Server) handleUpdatePlaylist(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	ctx := r.Context()
	playlistID, err := DecodeIDKind(param(r, "playlistId"), KindPlaylist)
	if err != nil {
		respondError(w, r, errNotFound, "playlist not found")
		return
	}
	detail, err := s.app.GetUserPlaylistDetail(ctx, u.ID, playlistID)
	if err != nil {
		respondError(w, r, errNotFound, "playlist not found")
		return
	}

	name := detail.Playlist.Name
	if v := param(r, "name"); v != "" {
		name = v
	}
	comment := detail.Playlist.Description
	if v := param(r, "comment"); v != "" {
		comment = v
	}
	if err := s.app.UpdateUserPlaylist(ctx, u.ID, playlistID, name, comment, detail.Playlist.CoverPath, nil); err != nil {
		respondError(w, r, errGeneric, "updating playlist failed")
		return
	}

	for _, raw := range paramAll(r, "songIdToAdd") {
		id, err := DecodeIDKind(raw, KindTrack)
		if err != nil {
			respondError(w, r, errNotFound, "unknown songIdToAdd: "+raw)
			return
		}
		if err := s.app.AddTrackToPlaylist(ctx, u.ID, playlistID, id); err != nil {
			respondError(w, r, errGeneric, "adding song failed")
			return
		}
	}
	// songIndexToRemove is positional; resolve against the pre-update order.
	for _, raw := range paramAll(r, "songIndexToRemove") {
		idx, err := strconv.Atoi(raw)
		if err != nil || idx < 0 || idx >= len(detail.Tracks) {
			respondError(w, r, errNotFound, "songIndexToRemove out of range: "+raw)
			return
		}
		if err := s.app.RemoveTrackFromPlaylist(ctx, u.ID, playlistID, detail.Tracks[idx].TrackID); err != nil {
			respondError(w, r, errGeneric, "removing song failed")
			return
		}
	}
	respond(w, r, "", nil)
}

// deletePlaylist.
func (s *Server) handleDeletePlaylist(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	playlistID, err := DecodeIDKind(param(r, "id"), KindPlaylist)
	if err != nil {
		respondError(w, r, errNotFound, "playlist not found")
		return
	}
	if err := s.app.DeleteUserPlaylist(r.Context(), u.ID, playlistID); err != nil {
		respondError(w, r, errGeneric, "deleting playlist failed")
		return
	}
	respond(w, r, "", nil)
}
